package envelopevisual

import (
	"strings"
	"testing"
)

// pianoEnvelope is the PIANO 1 C4 voice's DCA envelope from the FZ-1
// Factory Library disk fl-a-piano (Piano.fzf, voice index 6). Sus = 7
// / End = 4 means the envelope walks stages 0..4 on note-on then
// holds; the sample's natural decay carries the tail.
func pianoEnvelope() Envelope {
	return Envelope{
		Sus:        7,
		End:        4,
		Rates:      [8]uint8{0x7F, 0x88, 0x8D, 0x97, 0xBF, 0xBF, 0xBF, 0xBF},
		StopLevels: [8]uint8{0xFF, 0xEF, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00},
	}
}

// TestView_PianoEnvelope renders the realistic piano envelope and
// asserts both that the call returns a non-empty string and that the
// expected glyphs are present: a breakpoint dot and the Sus marker.
func TestView_PianoEnvelope(t *testing.T) {
	out := View(pianoEnvelope(), 0, 60, 16)
	if out == "" {
		t.Fatal("View returned empty string")
	}
	if !strings.ContainsRune(out, '●') {
		t.Errorf("expected breakpoint glyph ● in output, got:\n%s", out)
	}
	if !strings.ContainsRune(out, 'S') {
		t.Errorf("expected Sus glyph S in output, got:\n%s", out)
	}
}

// TestView_SelectedStageHighlight asserts that selecting a stage
// switches its glyph from the normal dot to the diamond highlight.
func TestView_SelectedStageHighlight(t *testing.T) {
	out := View(pianoEnvelope(), 2, 60, 16)
	if !strings.ContainsRune(out, '◆') {
		t.Errorf("expected selected-stage glyph ◆ in output, got:\n%s", out)
	}
}

// TestView_SingleStageEnvelope drives the edge case where End is 0:
// the envelope reduces to a single breakpoint. The renderer must
// still produce a non-empty view without panic.
func TestView_SingleStageEnvelope(t *testing.T) {
	env := Envelope{
		Sus:        0,
		End:        0,
		Rates:      [8]uint8{0x7F, 0, 0, 0, 0, 0, 0, 0},
		StopLevels: [8]uint8{0x80, 0, 0, 0, 0, 0, 0, 0},
	}
	out := View(env, -1, 60, 16)
	if out == "" {
		t.Fatal("View returned empty string for single-stage envelope")
	}
}

// TestView_ZeroEnvelope drives the fully zero case: rate 0 in
// envRateTable is 0 which would freeze the firmware ramp. The widget
// must still render without panic and without infinite loops.
func TestView_ZeroEnvelope(t *testing.T) {
	env := Envelope{
		Sus:        0,
		End:        0,
		Rates:      [8]uint8{},
		StopLevels: [8]uint8{},
	}
	out := View(env, -1, 60, 16)
	if out == "" {
		t.Fatal("View returned empty string for zero envelope")
	}
}

// TestView_SusEqualsEnd asserts that when Sus and End point at the
// same stage, both glyphs land in the output (stacked, per the
// implementation comment).
func TestView_SusEqualsEnd(t *testing.T) {
	env := Envelope{
		Sus: 2,
		End: 2,
		Rates: [8]uint8{
			0x7F, 0x7F, 0x88, 0, 0, 0, 0, 0,
		},
		StopLevels: [8]uint8{
			0x40, 0xFF, 0x80, 0, 0, 0, 0, 0,
		},
	}
	out := View(env, -1, 60, 16)
	if !strings.ContainsRune(out, 'S') {
		t.Errorf("expected Sus glyph S in output, got:\n%s", out)
	}
	if !strings.ContainsRune(out, 'E') {
		t.Errorf("expected End glyph E in output, got:\n%s", out)
	}
}
