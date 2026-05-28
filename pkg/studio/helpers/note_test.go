package helpers

import (
	"testing"

	"github.com/philipcunningham/fizzle/pkg/render"
)

func TestParseNoteIntegers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want uint8
	}{
		{"0", 0},
		{"60", 60},
		{"127", 127},
	}
	for _, c := range cases {
		got, err := ParseNote(c.in)
		if err != nil {
			t.Errorf("ParseNote(%q) unexpected err: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseNote(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseNoteOutOfRangeInt(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"-1", "128", "999"} {
		if _, err := ParseNote(in); err == nil {
			t.Errorf("ParseNote(%q) want error, got nil", in)
		}
	}
}

func TestParseNoteNoteNames(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want uint8
	}{
		{"C-1", 0},
		{"C4", 60},
		{"c4", 60},
		{"C 4", 60},
		{"F#3", 54},
		{"Bb2", 46}, // Bb2 = A#2 = MIDI 46
		{"G9", 127},
	}
	for _, c := range cases {
		got, err := ParseNote(c.in)
		if err != nil {
			t.Errorf("ParseNote(%q) unexpected err: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseNote(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseNoteInvalid(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "Z3", "C##4", "C", "Cb"} {
		if _, err := ParseNote(in); err == nil {
			t.Errorf("ParseNote(%q) want error, got nil", in)
		}
	}
}

func TestFormatNoteRoundtrip(t *testing.T) {
	t.Parallel()
	// Every MIDI value that has a canonical name in render.NoteName must
	// round-trip through FormatNote -> ParseNote.
	for m := 0; m <= 127; m++ {
		name := FormatNote(uint8(m))
		got, err := ParseNote(name)
		if err != nil {
			t.Errorf("ParseNote(%q) for MIDI %d: %v", name, m, err)
			continue
		}
		if int(got) != m {
			t.Errorf("roundtrip MIDI %d -> %q -> %d", m, name, got)
		}
	}
	// And the canonical formatter matches render.NoteName.
	if FormatNote(60) != render.NoteName(60) {
		t.Errorf("FormatNote(60) != render.NoteName(60)")
	}
}
