// Package helpers contains pure presentation and parsing helpers for the
// fizzle studio widgets. No tview imports live here; widget code under
// pkg/studio/widgets composes these primitives into InputField,
// DropDown, and Checkbox bindings.
package helpers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/render"
)

// noteIndices maps the canonical 12 chromatic note letters used by
// pkg/render.NoteName to their semitone offsets within an octave. Flats are
// accepted on the parser side and folded to the sharp equivalent so users can
// type either C#4 or Db4 and get the same MIDI value.
var noteIndices = map[string]int{
	"C": 0, "C#": 1, "Db": 1,
	"D": 2, "D#": 3, "Eb": 3,
	"E": 4,
	"F": 5, "F#": 6, "Gb": 6,
	"G": 7, "G#": 8, "Ab": 8,
	"A": 9, "A#": 10, "Bb": 10,
	"B": 11,
}

// ParseNote parses a MIDI note from either an integer (0-127) or a scientific
// note name (e.g. "C4", "f#3", "Bb2", "C-1"). fizzle uses C-1 = MIDI 0, so
// MIDI 60 = C4, matching pkg/render.NoteName. The input is case-insensitive;
// a single space between the letter and the octave is accepted.
func ParseNote(s string) (uint8, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, fmt.Errorf("helpers: empty note")
	}
	// Integer fast-path: any input that parses as a decimal integer is
	// treated as a raw MIDI number.
	if n, err := strconv.Atoi(t); err == nil {
		if n < 0 || n > int(disk.MaxMIDINote) {
			return 0, fmt.Errorf("helpers: MIDI note %d out of range (0-%d)", n, disk.MaxMIDINote)
		}
		return uint8(n), nil //nolint:gosec // G115: range checked above
	}

	// Note-name path. Letter, optional accidental, signed octave. Strip a
	// single internal space so "C 4" parses the same as "C4".
	t = strings.ReplaceAll(t, " ", "")
	if t == "" {
		return 0, fmt.Errorf("helpers: empty note")
	}

	letter := strings.ToUpper(t[:1])
	if letter < "A" || letter > "G" {
		return 0, fmt.Errorf("helpers: %q: not a note name", s)
	}
	idx := 1
	pitch := letter
	if idx < len(t) {
		switch t[idx] {
		case '#':
			pitch = letter + "#"
			idx++
		case 'b', 'B':
			// 'B' would collide with the note B at idx 0; we never reach
			// this branch with idx=0, so case-insensitive accidental
			// matching here is safe.
			pitch = letter + "b"
			idx++
		}
	}
	semitone, ok := noteIndices[pitch]
	if !ok {
		return 0, fmt.Errorf("helpers: %q: unknown pitch %q", s, pitch)
	}
	if idx >= len(t) {
		return 0, fmt.Errorf("helpers: %q: missing octave", s)
	}
	oct, err := strconv.Atoi(t[idx:])
	if err != nil {
		return 0, fmt.Errorf("helpers: %q: parsing octave %q: %w", s, t[idx:], err)
	}
	// Range check against MIDI 0..127. Octave can be negative (C-1).
	midi := (oct+1)*int(disk.SemitonesPerOctave) + semitone
	if midi < 0 || midi > int(disk.MaxMIDINote) {
		return 0, fmt.Errorf("helpers: %q: MIDI %d out of range (0-%d)", s, midi, disk.MaxMIDINote)
	}
	return uint8(midi), nil //nolint:gosec // G115: bound-checked
}

// FormatNote is the inverse of ParseNote for canonical (sharps-only) output.
// It wraps pkg/render.NoteName so display strings stay consistent across
// fizzle: voices, banks, area details, and the studio widget wave all
// render MIDI values through the same function.
func FormatNote(midi uint8) string {
	return render.NoteName(midi)
}
