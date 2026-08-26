// Package fzbinfo implements the 'fizzle fzb info' command. It reads a bank
// dump file and returns its voice map as structured data, with a separate
// renderer for terminal output. Bank dumps contain a bank sector and voice
// headers but no audio data.
package fzbinfo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/philipcunningham/fizzle/pkg/disk"
	fzfmodel "github.com/philipcunningham/fizzle/pkg/fzf"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
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
	doc, err := fzfmodel.NewBankDump(data)
	if err != nil {
		return nil, fmt.Errorf("fzbinfo: %q: %w", path, err)
	}
	layout := doc.Layout()
	nvoice := layout.VoiceCount()
	bank := doc.Bank()

	info := &BankDump{
		Filename:   filepath.Base(path),
		VoiceCount: nvoice,
		BankName:   bank.Name(),
	}
	info.ShowVelocity = bank.ShowsVelocity()
	info.ShowVolume = bank.ShowsVolume()

	for i := range nvoice {
		// MappedVoice returns false for NoSound placeholders. Mirror fzfinfo
		// and emit a placeholder, keeping
		// len(info.Voices) == VoiceCount; drop them instead and every
		// later voice's slot index shifts left out of step with vp[].
		base, ok, err := doc.MappedVoice(i)
		if err != nil {
			return nil, fmt.Errorf("fzbinfo: %q: voice %d: %w", path, i+1, err)
		}
		if !ok {
			info.Voices = append(info.Voices, VoiceEntry{
				Index:        i + 1,
				PlaybackMode: disk.PlaybackModeName(base.PlaybackMode),
			})
			continue
		}
		info.Voices = append(info.Voices, VoiceEntry{
			Index:        base.Index,
			Name:         base.Name,
			PlaybackMode: disk.PlaybackModeName(base.PlaybackMode),
			KeyLow:       base.KeyLow,
			KeyHigh:      base.KeyHigh,
			RootNote:     base.RootKey,
			MIDIChannel:  base.MIDIChannel,
			Output:       base.Output,
			BankVolume:   base.Volume,
			VelLow:       base.VelocityLow,
			VelHigh:      base.VelocityHigh,
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
