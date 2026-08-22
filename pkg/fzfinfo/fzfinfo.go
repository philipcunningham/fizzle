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
// full dump. PlaybackMode mirrors the spec's loop-mode field ("normal",
// "reverse", "cue", "synthesized", "no_sound") and decides whether a slot
// is audible. NoSound slots are spec-defined placeholders, kept here so
// the array length matches the declared voice_count and slot indices line
// up with bank vp[] references. Skip them to filter to audible voices.
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
	// AssembleMultiDisk stamps the total wave sector count across both
	// disks into the bank sector at BankTotalWaveOffset. A value above the
	// audio present here means this is disk 1 of a 2-disk split. The first
	// voice whose wavst (cumulative sample address) points past the local
	// audio area is the boundary: its audio lives on disk 2, which the
	// sampler appends into RAM after the last local voice. See
	// llm-wiki/topics/multi-disk-dumps.md.
	//
	// The FZ-1 doesn't always write BankTotalWaveOffset, so real-world
	// dumps often carry garbage there. Declaring is_split=true therefore
	// needs corroboration: the candidate boundary voice must itself be
	// plausible (printable name and valid sample-rate index). When only
	// implausible slots trigger it, the marker is noise and the file
	// reports as standalone.
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
		// Clamp to the audio present in the file. Real FZFs from older
		// tooling carry a garbage waveEnd in the last voice header
		// (Drums.fzf reports about 4 GB before clamping), and the audio
		// area is the upper bound on this dump's memory.
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

// parseVoiceEntry always returns a VoiceEntry for slot i, even for a
// PlaybackModeNoSound placeholder, which carries only the slot index and
// PlaybackMode "no_sound" with every other field zero.
//
// On multi-bank FZFs several banks can reference one voice slot through
// vp[] (TECHNO.img shares slot 10 across banks 1 to 4 and 6 to 7). The
// displayed key range, MIDI channel, output, velocity, and bvol come from
// the first BankSite in bank-then-split order, so the value is
// deterministic and matches the front panel's first reference. Orphan
// headers with no bank site render with zero metadata; fizzle-built dumps
// never produce those, hand-crafted hardware files can.
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
		// Surface the undocumented variant so its occurrences stay
		// visible. Treating it as Normal is best-effort: the structural
		// fields validate and the file is otherwise clean, but the
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
		// Orphan voice header, or ParseBankVoiceEntry declined. The
		// header still carries the audio metadata, so default the bank
		// fields and carry on with duration, rate, and hasLoop below.
		// MIDIChannel reports 1 (spec channel 1) so downstream invariants
		// don't read 0 as out of range, and Output renders "none" to
		// mirror the gchn=0 case.
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
	// loop_sus (0 to 7) picks the active loopst/looped pair; 8 means no
	// sustain loop. Mask the spec's reserved loop-fine and skip-flag bits
	// so the comparison sees sample positions only.
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

// countAudibleVoices counts entries with a non-NoSound playback mode:
// what the sampler actually plays, as against VoiceCount, which is the
// spec's slot count.
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
		header = append(header, "Level")
	}
	header = append(header, "Rate", "Duration")
	t.AppendHeader(header)

	for _, v := range info.Voices {
		// NoSound slots exist only to keep slot indices aligned with bank
		// vp[], so there's no row worth rendering.
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
				// Spec §1-5 gives htch/ltch the range 1 to 127, so (0,0)
				// is unreachable by a MIDI note-on and the voice never
				// triggers.
				vel = "off"
			default:
				vel = fmt.Sprintf("%d to %d", v.VelLow, v.VelHigh)
			}
			row = append(row, vel)
		}
		if info.ShowVolume {
			// The panel's AREA LEVEL row, where 127 is loudest. The
			// stored bvol byte counts the other way.
			row = append(row, disk.AreaLevelFromByte(v.BankVolume))
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
