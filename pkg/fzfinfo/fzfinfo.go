// Package fzfinfo implements the 'fizzle fzf info' command. It reads a full
// dump file and returns its voice map as structured data, with a separate
// renderer for terminal output.
package fzfinfo

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/logger"
	"github.com/philipcunningham/fizzle/pkg/render"
)

// VoiceEntry holds the parsed parameters for a single voice slot in the
// full dump. PlaybackMode reflects the spec's loop-mode field
// ("normal"/"reverse"/"cue"/"synthesized"/"no_sound") and is the
// authoritative signal for whether a slot is audible: a NoSound slot is a
// spec-defined placeholder, included here so the array length matches the
// declared voice_count and consumers can correlate slot index with bank
// vp[] references. To filter to audible voices, skip entries with
// PlaybackMode == "no_sound".
type VoiceEntry struct {
	fzutil.VoiceEntry
	RateIndex uint8   `json:"rate_index"`
	Duration  float64 `json:"duration"`
	HasLoop   bool    `json:"has_loop"`
}

// FullDump holds the parsed contents of a full dump file.
type FullDump struct {
	Filename     string       `json:"filename"`
	VoiceCount   int          `json:"voice_count"`
	MemoryBytes  int          `json:"memory_bytes"`
	IsSplit      bool         `json:"is_split"`
	DiskNumber   int          `json:"disk_number,omitempty"`
	TotalDisks   int          `json:"total_disks,omitempty"`
	LocalVoices  int          `json:"local_voices"`
	ShowVelocity bool         `json:"-"`
	ShowVolume   bool         `json:"-"`
	Voices       []VoiceEntry `json:"voices"`
}

// Parse reads the FZF file at path and returns its contents as structured data.
func Parse(path string) (*FullDump, error) {
	data, err := fzutil.ReadBounded(path, fzutil.MaxReadSize)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("fzfinfo: %q: file not found", path)
		}
		return nil, fmt.Errorf("fzfinfo: %w", err)
	}
	if disk.IsPlausibleVoiceHeader(data) {
		return nil, fmt.Errorf("fzfinfo: %q looks like a voice file, not a full dump. Try 'fzv info' instead", path)
	}
	hdr, err := fzutil.ParseFZFHeader(data)
	if err != nil {
		return nil, fmt.Errorf("fzfinfo: %w", err)
	}

	bank := data[:disk.SectorSize]
	nvoice := hdr.NVoice
	voiceAreaStart := hdr.VoiceAreaStart

	voiceSectors := disk.VoiceAreaSectors(nvoice)
	voiceAreaEnd := voiceAreaStart + voiceSectors*disk.SectorSize
	if len(data) < voiceAreaEnd {
		return nil, fmt.Errorf("fzfinfo: %q: file truncated", path)
	}
	voiceArea := data[voiceAreaStart:voiceAreaEnd]

	waveEnds := make([]uint32, nvoice)
	for i := range nvoice {
		voff := disk.VoiceSlotOffset(0, i)
		if voff+8 <= len(voiceArea) {
			waveEnds[i] = binary.LittleEndian.Uint32(voiceArea[voff+disk.VoiceWaveEndOffset : voff+disk.VoiceWaveEndOffset+4])
		}
	}

	// Multi-disk detection.
	//
	// AssembleMultiDisk stamps the *total* wave sector count (across both
	// disks) into the bank sector at BankTotalWaveOffset. If that value
	// exceeds the audio actually present in this file, this is disk 1 of a
	// 2-disk split. The first voice whose wavst (cumulative sample address)
	// points past the local audio area is the boundary: its audio lives on
	// disk 2 and the sampler will append disk 2's bytes into RAM after the
	// last local voice. See docs/casio-fz1-format.md "Multi-Disk Full Dumps".
	//
	// The FZ-1 does not always write BankTotalWaveOffset, so this field is
	// frequently garbage in real-world dumps. We therefore require
	// corroborating evidence before declaring is_split=true: the candidate
	// boundary voice must itself be plausible (printable name + valid sample
	// rate index). If only non-plausible slots trigger the boundary
	// detection, the totalWaveMarker is treated as noise and the file is
	// reported as standalone.
	totalWaveMarker := int(binary.LittleEndian.Uint32(bank[disk.BankTotalWaveOffset : disk.BankTotalWaveOffset+4]))
	localAudioBytes := len(data) - voiceAreaEnd
	localWaveSectors := localAudioBytes / disk.SectorSize
	splitAt := -1
	if totalWaveMarker > 0 && totalWaveMarker > localWaveSectors {
		for i := range nvoice {
			voff := disk.VoiceSlotOffset(0, i)
			slot := voiceArea[voff : voff+disk.VoiceHeaderUsed]
			if !disk.IsPlausibleVoiceSlot(slot) {
				continue
			}
			wavst := binary.LittleEndian.Uint32(slot[disk.VoiceWaveStartOffset : disk.VoiceWaveStartOffset+4])
			if int(wavst)*disk.BytesPerSample >= localAudioBytes {
				splitAt = i
				break
			}
		}
	}

	info := &FullDump{
		Filename: filepath.Base(path),
		Voices:   []VoiceEntry{},
	}

	if splitAt >= 0 {
		info.IsSplit = true
		info.DiskNumber = 1
		info.TotalDisks = 2
		info.MemoryBytes = localAudioBytes
		info.VoiceCount = nvoice
	} else {
		info.VoiceCount = nvoice
		var totalBytes uint32
		if nvoice > 0 && waveEnds[nvoice-1] > 0 {
			totalBytes = waveEnds[nvoice-1] * disk.BytesPerSample
		} else if len(data) > voiceAreaEnd {
			totalBytes = uint32(localAudioBytes) //nolint:gosec // G115: localAudioBytes derived from file length, always non-negative
		}
		// Clamp to the audio actually present in the file. Real-world FZFs
		// from older tooling can carry a garbage waveEnd in the last voice
		// header (e.g. Drums.fzf reports ~4 GB before clamping). The audio
		// area is the upper bound for memory used by this dump.
		if int64(totalBytes) > int64(localAudioBytes) {
			totalBytes = uint32(localAudioBytes) //nolint:gosec // G115: localAudioBytes is non-negative
		}
		info.MemoryBytes = int(totalBytes)
	}

	info.ShowVelocity = fzutil.BankSectorShowsVelocity(data, hdr)
	info.ShowVolume = fzutil.BankSectorShowsVolume(data, hdr)

	for i := range nvoice {
		info.Voices = append(info.Voices, parseVoiceEntry(voiceArea, data, hdr, i))
	}

	if splitAt >= 0 {
		info.LocalVoices = splitAt
	} else {
		info.LocalVoices = countAudibleVoices(info.Voices)
	}

	return info, nil
}

// parseVoiceEntry always returns a VoiceEntry for slot i, even when the
// slot is a PlaybackModeNoSound placeholder. NoSound entries carry only the
// slot index and PlaybackMode = "no_sound"; all other fields are zero.
// Callers that want audible voices only should filter on
// PlaybackMode != disk.PlaybackModeNameNoSound.
//
// On multi-bank FZFs a single voice slot can be referenced from multiple
// banks via vp[] (e.g. TECHNO.img shares slot 10 across banks 1-4/6-7).
// The displayed key range, MIDI channel, output, velocity, and bvol are
// read from the first BankSite (the bank that "owns" the slot in
// bank-then-split order), so the value is deterministic and matches the
// front panel's first reference to the voice. Voices with no bank site
// (orphan headers) render with zero metadata; this never happens in
// fizzle-built dumps but can occur in hand-crafted hardware files.
func parseVoiceEntry(voiceArea, data []byte, fhdr *fzutil.FZFHeader, i int) VoiceEntry {
	voff := disk.VoiceSlotOffset(0, i)
	hdr := voiceArea[voff : voff+disk.VoiceHeaderUsed]
	mode := binary.LittleEndian.Uint16(hdr[disk.VoiceLoopModeOffset : disk.VoiceLoopModeOffset+2])

	if mode == disk.PlaybackModeNoSound {
		return VoiceEntry{
			VoiceEntry: fzutil.VoiceEntry{
				Index:        i + 1,
				PlaybackMode: disk.PlaybackModeNameNoSound,
			},
		}
	}

	if mode == disk.PlaybackModeNormalVariant {
		// Surface the undocumented variant so its occurrences remain
		// visible. Treating as Normal is a best-effort: the structural
		// fields validate and the file is otherwise clean, but the precise
		// hardware semantics of the cleared bit aren't documented.
		logger.Warn().
			Int("slot", i+1).
			Uint16("loop_mode", mode).
			Msg("voice slot uses undocumented playback mode 0x0157 (treating as Normal variant)")
	}

	sites := fzutil.FindBankSitesForVoice(data, fhdr, i)
	var (
		base   fzutil.BankVoiceEntry
		baseOK bool
	)
	if len(sites) > 0 {
		site := sites[0]
		bank := fzutil.BankSliceAt(data, site.BankIdx)
		if bank != nil {
			base, baseOK = fzutil.ParseBankVoiceEntry(bank, voiceArea, site.SplitIdx, i)
		}
	}
	if !baseOK {
		// No referenced bank site (orphan voice header) or
		// ParseBankVoiceEntry returned false. We still know the voice's
		// audio metadata from the header itself, so render the slot with
		// defaults for the bank fields and continue computing duration /
		// rate / hasLoop below. MIDIChannel reports 1 (spec channel 1) so
		// downstream invariants don't treat 0 as out-of-range; the Output
		// column renders as "none" to mirror the gchn=0 case.
		name := disk.TrimPadded(hdr[disk.VoiceNameOffset : disk.VoiceNameOffset+disk.LabelSize])
		if name == "" || !disk.IsPrintableName([]byte(name)) {
			name = fmt.Sprintf("VOICE %d", i+1)
		}
		base = fzutil.BankVoiceEntry{
			Index:       i + 1,
			Name:        name,
			MIDIChannel: 1,
			Output:      disk.FormatAudioOut(0),
		}
	}

	sampIdx := hdr[disk.VoiceSampOffset]
	rate := disk.SampleRate(sampIdx)

	waveStart := binary.LittleEndian.Uint32(hdr[disk.VoiceWaveStartOffset : disk.VoiceWaveStartOffset+4])
	waveEnd := binary.LittleEndian.Uint32(hdr[disk.VoiceWaveEndOffset : disk.VoiceWaveEndOffset+4])
	var voiceSamples uint32
	if waveEnd > waveStart {
		voiceSamples = waveEnd - waveStart
	}
	var duration float64
	if rate > 0 && voiceSamples > 0 {
		duration = float64(voiceSamples) / float64(rate)
	}

	loopSus := hdr[disk.VoiceLoopSusOffset]
	// loop_sus (0..7) selects which of the eight loopst/looped pairs is
	// the active sustain loop; loop_sus == 8 means no sustain loop.
	// Mask the loop-fine and skip-flag bits the spec reserves so the
	// address comparison reflects sample positions only.
	hasLoop := false
	if loopSus < disk.NoSustainLoop {
		stOff := disk.VoiceLoopSt0Offset + int(loopSus)*4
		edOff := disk.VoiceLoopEd0Offset + int(loopSus)*4
		rawSt := binary.LittleEndian.Uint32(hdr[stOff : stOff+4])
		rawEd := binary.LittleEndian.Uint32(hdr[edOff : edOff+4])
		hasLoop = disk.LoopStartAddress(rawSt) < disk.LoopEndAddress(rawEd)
	}

	return VoiceEntry{
		VoiceEntry: fzutil.VoiceEntry{
			Index:        base.Index,
			Name:         base.Name,
			PlaybackMode: disk.PlaybackModeName(mode),
			RootNote:     base.RootNote,
			KeyLow:       base.KeyLow,
			KeyHigh:      base.KeyHigh,
			VelLow:       base.VelLow,
			VelHigh:      base.VelHigh,
			MIDIChannel:  base.MIDIChannel,
			Output:       base.Output,
			BankVolume:   base.BankVolume,
		},
		RateIndex: sampIdx,
		Duration:  duration,
		HasLoop:   hasLoop,
	}
}

// countAudibleVoices returns the count of entries with a non-NoSound
// playback mode. This is the "what the sampler will actually play"
// number, distinct from VoiceCount which is the spec's slot count.
func countAudibleVoices(voices []VoiceEntry) int {
	n := 0
	for _, v := range voices {
		if v.PlaybackMode != disk.PlaybackModeNameNoSound {
			n++
		}
	}
	return n
}

// RenderJSON writes the full dump info as indented JSON to w.
func RenderJSON(w io.Writer, info *FullDump) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(info)
}

// Render writes a formatted voice map table to w.
// highlighted is a set of 1-based voice indices to mark with * in the table.
func Render(w io.Writer, info *FullDump, highlighted map[int]bool) {
	render.Printf(w, "Full dump: %s\n", info.Filename)

	if info.IsSplit {
		render.Printf(w, "Disk:      %d of %d\n", info.DiskNumber, info.TotalDisks)
		render.Printf(w, "Memory:    %s\n", render.FormatBytes(info.MemoryBytes))
		render.Printf(w, "Voices:    %d\n", info.VoiceCount)
	} else {
		render.Printf(w, "Voices:    %d\n", info.VoiceCount)
		render.Printf(w, "Memory:    %s\n", render.FormatBytes(info.MemoryBytes))
	}

	render.Println(w)

	t := render.NewTable(w)

	header := table.Row{"#", "Name", "Root", "Keys", "Chan", "Out"}
	if info.ShowVelocity {
		header = append(header, "Velocity")
	}
	if info.ShowVolume {
		header = append(header, "Vol")
	}
	header = append(header, "Rate", "Duration")
	t.AppendHeader(header)

	for _, v := range info.Voices {
		// NoSound slots are spec-defined placeholders with no audible output;
		// they exist in the file for slot-index alignment with bank vp[] but
		// have no meaningful row to render.
		if v.PlaybackMode == disk.PlaybackModeNameNoSound {
			continue
		}
		var keys string
		if v.KeyLow == v.KeyHigh {
			keys = render.NoteName(v.KeyLow)
		} else {
			keys = fmt.Sprintf("%s to %s", render.NoteName(v.KeyLow), render.NoteName(v.KeyHigh))
		}

		durStr := fmt.Sprintf("%.3fs", v.Duration)
		if v.HasLoop {
			durStr += " [loop]"
		}

		rowNum := fmt.Sprintf("%d", v.Index)
		if highlighted[v.Index] {
			rowNum = "*" + rowNum
		}

		row := table.Row{rowNum, v.Name, render.NoteName(v.RootNote), keys, v.MIDIChannel, v.Output}
		if info.ShowVelocity {
			var vel string
			switch {
			case v.VelLow == 0 && v.VelHigh == 0:
				// Spec §1-5 says htch/ltch range is 1-127; (0,0) is
				// unreachable by MIDI note-on (which uses vel 1-127),
				// so the voice will never trigger.
				vel = "off"
			default:
				vel = fmt.Sprintf("%d to %d", v.VelLow, v.VelHigh)
			}
			row = append(row, vel)
		}
		if info.ShowVolume {
			row = append(row, v.BankVolume)
		}
		row = append(row, render.RateName(v.RateIndex), durStr)
		t.AppendRow(row)
	}

	t.Render()
}

// Info reads the FZF file at path and writes a voice map to w.
// highlighted is a set of 1-based voice indices to mark with * in the table.
// Pass nil for no highlighting.
func Info(path string, w io.Writer, highlighted map[int]bool) error {
	info, err := Parse(path)
	if err != nil {
		return err
	}
	Render(w, info, highlighted)
	return nil
}
