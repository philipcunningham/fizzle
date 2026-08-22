// Package fzbinfo implements the 'fizzle fzb info' command. It reads a bank
// dump file and returns its voice map as structured data, with a separate
// renderer for terminal output. Bank dumps contain a bank sector and voice
// headers but no audio data.
package fzbinfo

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

// VoiceEntry is an alias for fzutil.VoiceEntry, the 11-field shape
// fzbinfo and fzfinfo share for bank-mapped voice metadata. fzfinfo's own
// VoiceEntry embeds the same type and adds three audio-only fields (rate
// index, duration, and has-loop). PlaybackMode is the spec's loop-mode
// field ("normal", "no_sound", and so on). NoSound entries are
// spec-defined placeholders, kept so slot indices match the bank's vp[]
// references; filter them out for audible voices only.
type VoiceEntry = fzutil.VoiceEntry

// BankDump holds the parsed contents of a bank dump file.
type BankDump struct {
	Filename     string       `json:"filename"`
	VoiceCount   int          `json:"voice_count"`
	BankName     string       `json:"bank_name"`
	ShowVelocity bool         `json:"-"`
	ShowVolume   bool         `json:"-"`
	Voices       []VoiceEntry `json:"voices"`
}

// Parse reads the FZB file at path and returns its contents as structured data.
func Parse(path string) (*BankDump, error) {
	data, err := fzutil.ReadBounded(path, fzutil.MaxReadSize)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("fzbinfo: %q: file not found", path)
		}
		return nil, fmt.Errorf("fzbinfo: %w", err)
	}
	if len(data) < disk.SectorSize {
		return nil, fmt.Errorf("fzbinfo: %q: file too small (%d bytes, need at least %d)", path, len(data), disk.SectorSize)
	}

	bank := data[:disk.SectorSize]
	// bstep is the bank sector's stored voice count. The spec sizes the
	// voice area from a separate file-level vn field, which FZBs
	// (single-bank by spec §1-5) lose during extraction. fzutil recovers
	// vn for FZF by walking the voice area, so do the same here and a
	// stale bstep from a buggy tool can't skew the reported count.
	bstep := int(binary.LittleEndian.Uint16(bank[disk.BankVoiceCountOffset : disk.BankVoiceCountOffset+2]))
	voiceAreaStart := disk.SectorSize

	// InferVoiceCount takes bstep as an upper bound on the walk. An
	// implausible bstep (0 or above MaxVoices) falls back to MaxVoices so
	// the walk itself decides where the voice area ends.
	upper := bstep
	if upper <= 0 || upper > disk.MaxVoices {
		upper = disk.MaxVoices
	}
	inferred := fzutil.InferVoiceCount(data, voiceAreaStart, upper)

	var nvoice int
	switch {
	case inferred == 0:
		return nil, fmt.Errorf("fzbinfo: %q: no valid voice headers found (bstep=%d)", path, bstep)
	case bstep >= 1 && bstep <= disk.MaxVoices && bstep == inferred:
		nvoice = bstep
	default:
		// bstep disagrees with the voice area or is out of range. Trust
		// the walk, and log so the divergence surfaces under --debug
		// without disturbing normal use.
		logger.Debug().
			Str("path", path).
			Int("bstep", bstep).
			Int("inferred", inferred).
			Msg("fzbinfo: bstep disagrees with voice-area walk; using inferred count")
		nvoice = inferred
	}

	bankName := disk.TrimPadded(bank[disk.BankNameOffset : disk.BankNameOffset+disk.LabelSize])

	voiceSectors := disk.VoiceAreaSectors(nvoice)
	voiceAreaEnd := voiceAreaStart + voiceSectors*disk.SectorSize
	if len(data) < voiceAreaEnd {
		return nil, fmt.Errorf("fzbinfo: %q: file truncated (need %d bytes for voice headers, have %d)", path, voiceAreaEnd, len(data))
	}
	voiceArea := data[voiceAreaStart:voiceAreaEnd]

	info := &BankDump{
		Filename:   filepath.Base(path),
		VoiceCount: nvoice,
		BankName:   bankName,
	}

	// FZB is single-bank by spec §1-5: one bank sector, no multi-bank
	// fan-out over key splits. The (bank, split) and voice-slot indices
	// coincide for every entry, so a synthesised one-bank FZFHeader lets
	// the shared show-* helpers work unforked.
	hdr := &fzutil.FZFHeader{
		NVoice:         nvoice,
		BStep0:         nvoice,
		NBankSectors:   1,
		VoiceAreaStart: voiceAreaStart,
	}
	info.ShowVelocity = fzutil.BankSectorShowsVelocity(data, hdr)
	info.ShowVolume = fzutil.BankSectorShowsVolume(data, hdr)

	for i := range nvoice {
		// ParseBankVoiceEntry returns false for NoSound placeholders and
		// truncated input. Mirror fzfinfo and emit a placeholder, keeping
		// len(info.Voices) == VoiceCount; drop them instead and every
		// later voice's slot index shifts left out of step with vp[].
		voff := disk.VoiceSlotOffset(0, i)
		var mode uint16
		if voff+disk.VoiceLoopModeOffset+2 <= len(voiceArea) {
			mode = binary.LittleEndian.Uint16(voiceArea[voff+disk.VoiceLoopModeOffset : voff+disk.VoiceLoopModeOffset+2])
		}
		// FZB is single-bank: bank slot index == voice slot index.
		base, ok := fzutil.ParseBankVoiceEntry(bank, voiceArea, i, i)
		if !ok {
			info.Voices = append(info.Voices, VoiceEntry{
				Index:        i + 1,
				PlaybackMode: disk.PlaybackModeName(mode),
			})
			continue
		}
		info.Voices = append(info.Voices, VoiceEntry{
			Index:        base.Index,
			Name:         base.Name,
			PlaybackMode: disk.PlaybackModeName(mode),
			KeyLow:       base.KeyLow,
			KeyHigh:      base.KeyHigh,
			RootNote:     base.RootNote,
			MIDIChannel:  base.MIDIChannel,
			Output:       base.Output,
			BankVolume:   base.BankVolume,
			VelLow:       base.VelLow,
			VelHigh:      base.VelHigh,
		})
	}

	return info, nil
}

// RenderJSON writes the bank dump info as indented JSON to w.
func RenderJSON(w io.Writer, info *BankDump) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(info)
}

// Render writes a formatted voice map table to w.
func Render(w io.Writer, info *BankDump) {
	render.Printf(w, "Bank dump: %s\n", info.Filename)
	render.Printf(w, "Voices:    %d\n", info.VoiceCount)
	if info.BankName != "" {
		render.Printf(w, "Bank:      %s\n", info.BankName)
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
	t.AppendHeader(header)

	for _, v := range info.Voices {
		// NoSound slots stay in info.Voices to preserve slot order for
		// bank vp[], but there's no row worth rendering. Same policy as
		// fzfinfo.Render.
		if v.PlaybackMode == disk.PlaybackModeNameNoSound {
			continue
		}
		var keys string
		if v.KeyLow == v.KeyHigh {
			keys = render.NoteName(v.KeyLow)
		} else {
			keys = fmt.Sprintf("%s to %s", render.NoteName(v.KeyLow), render.NoteName(v.KeyHigh))
		}

		rowNum := fmt.Sprintf("%d", v.Index)

		row := table.Row{rowNum, v.Name, render.NoteName(v.RootNote), keys, v.MIDIChannel, v.Output}
		if info.ShowVelocity {
			var vel string
			switch {
			case v.VelLow == 0 && v.VelHigh == 0:
				// Spec §1-5 gives htch/ltch the range 1 to 127, so (0,0)
				// never triggers. Mirrors fzfinfo.Render.
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
		t.AppendRow(row)
	}

	t.Render()
}
