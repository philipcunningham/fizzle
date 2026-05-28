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

// VoiceEntry is an alias for fzutil.VoiceEntry: fzbinfo and fzfinfo share
// the same 11-field shape for bank-mapped voice metadata. The alias keeps
// existing callers (`fzbinfo.VoiceEntry{...}` literals, `info.Voices[i].KeyLow`)
// working unchanged. fzfinfo's local VoiceEntry embeds the same type and
// adds three audio-only fields (rate index, duration, has-loop).
// PlaybackMode is the spec's loop-mode field ("normal" / "no_sound" / ...).
// NoSound entries are spec-defined placeholders, included so the slot index
// matches the bank's vp[] references; callers that want audible voices only
// should filter on PlaybackMode != disk.PlaybackModeNameNoSound.
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
	// bstep is the bank sector's stored voice count. The spec uses a separate
	// file-level vn field to size the voice area, but FZBs (single-bank by
	// spec §1-5) lose that during extraction. fzfutil.ParseFZFHeader recovers
	// vn for FZF by walking the voice area; do the same here so a buggy tool
	// that wrote a stale bstep cannot make fzbinfo silently report a wrong
	// voice count.
	bstep := int(binary.LittleEndian.Uint16(bank[disk.BankVoiceCountOffset : disk.BankVoiceCountOffset+2]))
	voiceAreaStart := disk.SectorSize

	// InferVoiceCount uses its bstep argument as an upper bound on the walk.
	// When bstep is implausible (0 or >MaxVoices) we still want to recover
	// the true count, so fall back to MaxVoices as the bound and let the
	// walk itself decide where the voice area ends.
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
		// bstep disagrees with the voice area, or is out of range. Trust
		// the inferred walk and log a debug message so the divergence
		// surfaces under --debug without breaking normal use.
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

	// FZB is single-bank by spec §1-5 (one bank sector, no multi-bank fan-out
	// over key splits). The (bank, split) and voice-slot indices coincide
	// for every entry, so we synthesise a one-bank FZFHeader and reuse the
	// shared show-* helpers without forking a single-bank variant.
	hdr := &fzutil.FZFHeader{
		NVoice:         nvoice,
		BStep0:         nvoice,
		NBankSectors:   1,
		VoiceAreaStart: voiceAreaStart,
	}
	info.ShowVelocity = fzutil.BankSectorShowsVelocity(data, hdr)
	info.ShowVolume = fzutil.BankSectorShowsVolume(data, hdr)

	for i := range nvoice {
		// ParseBankVoiceEntry returns false for NoSound placeholders and for
		// truncated input. Mirror fzfinfo: emit a placeholder VoiceEntry so
		// len(info.Voices) == VoiceCount and bank vp[] indices line up with
		// the rendered table. Without this, NoSound slots fall out silently
		// and every subsequent voice's slot index is shifted left.
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
		header = append(header, "Vol")
	}
	t.AppendHeader(header)

	for _, v := range info.Voices {
		// NoSound slots are spec-defined placeholders with no audible output;
		// the entry is kept in info.Voices so consumers (and bank vp[]
		// indices) can correlate slot order, but it has no meaningful row
		// to render. Same policy as fzfinfo.Render.
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
				// Spec §1-5 says htch/ltch range is 1-127; (0,0) is
				// unreachable by MIDI note-on (which uses vel 1-127),
				// so the voice will never trigger. Mirror the fzfinfo
				// rendering so both info tools agree on this state.
				vel = "off"
			default:
				vel = fmt.Sprintf("%d to %d", v.VelLow, v.VelHigh)
			}
			row = append(row, vel)
		}
		if info.ShowVolume {
			row = append(row, v.BankVolume)
		}
		t.AppendRow(row)
	}

	t.Render()
}
