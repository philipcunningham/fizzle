// Package fzfoutput implements the 'fizzle fzf output' command. It sets the
// output (generator channel) assignment for one or more voices in an FZF full
// dump file.
//
// Hardware abstraction: the 8 voice generators and the OUTPUT panel.
//
// The FZ-1 has 8 voice generators behind 8 physical output jacks (1-8) on the
// back panel. The bank sector stores a generator channel bitmask per voice at
// offset 0x182 (gchn[64] in struct bankdata). Each bit corresponds to one of
// the 8 voice generators; the front panel shows them as 8 positions labelled
// OUTPUT (filled circle = assigned, dot = inactive).
//
// The same byte controls polyphony. A voice assigned to a single output is
// monophonic on that output; a new note cuts the previous. Voices sharing the
// same single output mute each other. This is the FZ-1's mute group mechanism
// (and what the SFZ converter targets when translating `mutegroup=N`). The
// value 0xff enables all 8 generators, distributing new notes round-robin and
// yielding full 8-voice polyphony.
package fzfoutput

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fileutil"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/logger"
	"github.com/philipcunningham/fizzle/pkg/render"
)

const allValue = "all"

// ParseOutputFlag parses a CLI output flag value into a gchn bitmask byte.
// Accepted formats: "all" (all 8 generators), "3" (single output), "1,3,5"
// (multiple outputs). Output numbers are 1-based (1 through 8).
func ParseOutputFlag(s string) (uint8, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("fzfoutput: --output value must not be empty")
	}
	if strings.EqualFold(s, allValue) {
		return disk.PolyphonicAudioOut, nil
	}

	var gchn uint8
	parts := strings.Split(s, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		n, err := strconv.Atoi(part)
		if err != nil {
			return 0, fmt.Errorf("fzfoutput: invalid output %q (use 1-8 or 'all')", part)
		}
		if n < 1 || n > disk.MaxGenerators {
			return 0, fmt.Errorf("fzfoutput: output %d out of range (use 1-%d or 'all')", n, disk.MaxGenerators)
		}
		gchn |= 1 << (n - 1)
	}
	if gchn == 0 {
		return 0, fmt.Errorf("fzfoutput: no valid outputs specified")
	}
	return gchn, nil
}

// VoiceUpdate describes a single voice whose output assignment was changed.
type VoiceUpdate struct {
	Index     int
	Name      string
	OldOutput string
	NewOutput string
}

// Result describes the outcome of a Set operation.
type Result struct {
	Updated []VoiceUpdate
}

// Set modifies the output assignment for matching voices in the FZF at path.
// gchn is the raw bitmask byte (use ParseOutputFlag to produce it from CLI input).
// If voices is non-empty, only those exact names are targeted (case-insensitive).
// If all is true, all voices are updated.
// All names are validated before any write. The file is never partially updated.
func Set(path string, voices []string, all bool, gchn uint8) (Result, error) {
	if err := validateInputs(voices, all); err != nil {
		return Result{}, err
	}

	data, hdr, err := fzutil.ReadFZF(path)
	if err != nil {
		return Result{}, fmt.Errorf("fzfoutput: %w", err)
	}

	targets, storedNames, err := fzutil.ResolveVoiceTargets(data, hdr, voices, all)
	if err != nil {
		return Result{}, fmt.Errorf("fzfoutput: %w", err)
	}

	// Multi-bank dumps fan one voice across several (bank, split) sites,
	// each with its own gchn byte indexed by key-split position (spec §2-2,
	// `gchn[64]`). Writing only data[BankAudioOutOffset+voiceSlot] would
	// patch bank 0 alone: same silent-failure mode as fzfmidi.Set. Walk
	// every site for the voice and update each one.
	newOutput := disk.FormatAudioOut(gchn)
	var result Result
	for _, i := range targets {
		sites := fzutil.FindBankSitesForVoice(data, hdr, i)
		if len(sites) == 0 {
			logger.Warn().
				Int("voice", i+1).
				Str("name", storedNames[i]).
				Msg("fzfoutput: voice has no bank reference; skipping output write")
			continue
		}
		firstOff := sites[0].BankIdx*disk.SectorSize + disk.BankAudioOutOffset + sites[0].SplitIdx
		oldGchn := data[firstOff]
		anyChange := false
		for _, site := range sites {
			off := site.BankIdx*disk.SectorSize + disk.BankAudioOutOffset + site.SplitIdx
			if data[off] != gchn {
				data[off] = gchn
				anyChange = true
			}
		}
		if !anyChange {
			continue
		}
		result.Updated = append(result.Updated, VoiceUpdate{
			Index:     i + 1,
			Name:      storedNames[i],
			OldOutput: disk.FormatAudioOut(oldGchn),
			NewOutput: newOutput,
		})
	}

	if len(result.Updated) == 0 {
		return result, nil
	}

	if err := fileutil.WriteAtomic(path, data); err != nil {
		return Result{}, fmt.Errorf("fzfoutput: writing %q: %w", path, err)
	}

	return result, nil
}

// Render writes a human-readable summary of a Set result to w.
func Render(w io.Writer, res Result, output string) {
	if len(res.Updated) == 0 {
		render.Println(w, "No voices updated (already on the specified output).")
		return
	}
	render.Printf(w, "Set output %s on:\n", output)
	for _, u := range res.Updated {
		render.Printf(w, "  voice %-3d  %s\n", u.Index, u.Name)
	}
	render.Println(w)
}

func validateInputs(voices []string, all bool) error {
	if len(voices) > 0 && all {
		return fmt.Errorf("fzfoutput: --voice and --all cannot be used together")
	}
	if len(voices) == 0 && !all {
		return fmt.Errorf("fzfoutput: either --voice or --all is required")
	}
	return nil
}
