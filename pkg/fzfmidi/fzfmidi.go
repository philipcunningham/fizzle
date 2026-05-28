// Package fzfmidi implements the 'fizzle fzf midi' command. It sets the
// MIDI receive channel for one or more voices in an FZF full dump file.
//
// Hardware abstraction: Area Mode.
//
// The FZ-1 bank sector stores a MIDI receive channel per voice at offset
// 0x142 (mchn[64] in struct bankdata). When voices in a full dump have
// distinct channels, the sampler operates multitimbrally: each voice
// listens on its own MIDI channel, and pitch bend / expression / other CCs
// affect only the voices on the matching channel. The FZ-1 documentation
// calls this Area Mode.
//
// Set is the in-memory equivalent of one front-panel edit cycle for the
// mchn array: validate names, mutate the bank-sector bytes, write atomically.
// The hardware reads the same bytes at load time.
package fzfmidi

import (
	"fmt"
	"io"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fileutil"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/logger"
	"github.com/philipcunningham/fizzle/pkg/render"
)

// VoiceUpdate describes a single voice whose MIDI channel was changed.
type VoiceUpdate struct {
	Index      int    // 1-based voice index
	Name       string // trimmed stored name
	OldChannel uint8  // previous channel (1-16)
	NewChannel uint8  // new channel (1-16)
}

// Result describes the outcome of a Set operation.
type Result struct {
	Updated []VoiceUpdate
}

// Set modifies the MIDI receive channel for matching voices in the FZF at path.
// channel must be 1-16 (stored internally as 0-15).
// If voices is non-empty, only those exact names are targeted (case-insensitive).
// If all is true, all voices are updated.
// All names are validated before any write. The file is never partially updated.
func Set(path string, voices []string, all bool, channel uint8) (Result, error) {
	if err := validateInputs(voices, all, channel); err != nil {
		return Result{}, err
	}

	data, hdr, err := fzutil.ReadFZF(path)
	if err != nil {
		return Result{}, fmt.Errorf("fzfmidi: %w", err)
	}

	targets, storedNames, err := fzutil.ResolveVoiceTargets(data, hdr, voices, all)
	if err != nil {
		return Result{}, fmt.Errorf("fzfmidi: %w", err)
	}

	// Apply changes and collect results.
	//
	// Multi-bank dumps fan a single voice slot across several (bank, split)
	// sites; the per-voice mchn byte the FZ-1 reads at note-on time lives in
	// each bank's own sector, indexed by key-split position (spec §2-2,
	// `mchn[64]`). Writing only data[BankMIDIRecvChanOffset+voiceSlot] would
	// patch bank 0 alone and leave banks 1-7 on their old channels: silent
	// failure mode for any voice that lives only in banks 1-7 (e.g. TECHNO's
	// METAL-BELL referenced from bank 0 split 0 via vp[0]=11).
	chanByte := channel - 1 // stored 0-indexed
	var result Result
	for _, i := range targets {
		sites := fzutil.FindBankSitesForVoice(data, hdr, i)
		if len(sites) == 0 {
			// Orphan voice header: no bank references the slot. fizzle's
			// voicebuild never produces such a dump; hardware files might
			// (or the file may be corrupt). Skip the write so we don't
			// silently rewrite an unrelated byte.
			logger.Warn().
				Int("voice", i+1).
				Str("name", storedNames[i]).
				Msg("fzfmidi: voice has no bank reference; skipping MIDI-channel write")
			continue
		}
		// Old-channel report uses the first site (the deterministic owner).
		// Different sites can carry different mchn bytes when the user
		// hand-edited the dump; the rendered "from X" is the first one.
		firstOff := sites[0].BankIdx*disk.SectorSize + disk.BankMIDIRecvChanOffset + sites[0].SplitIdx
		oldChan := data[firstOff] + 1
		anyChange := false
		for _, site := range sites {
			off := site.BankIdx*disk.SectorSize + disk.BankMIDIRecvChanOffset + site.SplitIdx
			if data[off] != chanByte {
				data[off] = chanByte
				anyChange = true
			}
		}
		if !anyChange {
			continue
		}
		result.Updated = append(result.Updated, VoiceUpdate{
			Index:      i + 1,
			Name:       storedNames[i],
			OldChannel: oldChan,
			NewChannel: channel,
		})
	}

	if len(result.Updated) == 0 {
		return result, nil
	}

	if err := fileutil.WriteAtomic(path, data); err != nil {
		return Result{}, fmt.Errorf("fzfmidi: writing %q: %w", path, err)
	}

	return result, nil
}

// Render writes a human-readable summary of a Set result to w.
func Render(w io.Writer, res Result, channel uint8) {
	if len(res.Updated) == 0 {
		render.Println(w, "No voices updated (already on the specified channel).")
		return
	}
	render.Printf(w, "Set MIDI receive channel %d on:\n", channel)
	for _, u := range res.Updated {
		render.Printf(w, "  voice %-3d  %s\n", u.Index, u.Name)
	}
	render.Println(w)
}

func validateInputs(voices []string, all bool, channel uint8) error {
	if len(voices) > 0 && all {
		return fmt.Errorf("fzfmidi: --voice and --all cannot be used together")
	}
	if len(voices) == 0 && !all {
		return fmt.Errorf("fzfmidi: either --voice or --all is required")
	}
	if channel < 1 || channel > disk.MaxMIDIChannel {
		return fmt.Errorf("fzfmidi: --channel must be between 1 and %d, got %d", disk.MaxMIDIChannel, channel)
	}
	return nil
}
