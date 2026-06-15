// Package fznote formats FZ MIDI note numbers for display in the studio
// TUI. It wraps render.NoteName with the studio-wide out-of-range guard so
// the layout and area editors render notes identically from one source.
package fznote

import "github.com/philipcunningham/fizzle/pkg/render"

// Name returns the note name for a MIDI note (e.g. 60 -> "C4"), or "?" for
// values outside the valid 0..127 range (which can appear when reading a
// corrupt bank byte).
func Name(midi int) string {
	if midi < 0 || midi > 127 {
		return "?"
	}
	return render.NoteName(uint8(midi))
}
