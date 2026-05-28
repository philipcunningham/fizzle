package render

import (
	"testing"
)

func TestFormatBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		n    int
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{143360, "140.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1468006, "1.4 MB"},
	}
	for _, tt := range tests {
		if got := FormatBytes(tt.n); got != tt.want {
			t.Errorf("FormatBytes(%d): got %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestNoteNameMiddleC(t *testing.T) {
	t.Parallel()
	if got := NoteName(60); got != "C4" {
		t.Errorf("MIDI 60: got %q, want %q", got, "C4")
	}
}

func TestNoteNameRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		midi uint8
		want string
	}{
		{0, "C-1"},
		{24, "C1"},
		{36, "C2"},
		{48, "C3"},
		{60, "C4"},
		{69, "A4"},
		{72, "C5"},
		{96, "C7"},
		{127, "G9"},
	}
	for _, tt := range tests {
		if got := NoteName(tt.midi); got != tt.want {
			t.Errorf("NoteName(%d): got %q, want %q", tt.midi, got, tt.want)
		}
	}
}

func TestNoteNameSharps(t *testing.T) {
	t.Parallel()
	if got := NoteName(61); got != "C#4" {
		t.Errorf("MIDI 61: got %q, want %q", got, "C#4")
	}
	if got := NoteName(70); got != "A#4" {
		t.Errorf("MIDI 70: got %q, want %q", got, "A#4")
	}
}

func TestRateName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		idx  uint8
		want string
	}{
		{0, "36k"},
		{1, "18k"},
		{2, "9k"},
		{3, "?"},
		{255, "?"},
	}
	for _, tt := range tests {
		if got := RateName(tt.idx); got != tt.want {
			t.Errorf("RateName(%d): got %q, want %q", tt.idx, got, tt.want)
		}
	}
}

func TestNoteNamesLength(t *testing.T) {
	t.Parallel()
	if len(noteNames) != 12 {
		t.Errorf("noteNames length: got %d, want 12", len(noteNames))
	}
}

func TestRateNameCoversAllRates(t *testing.T) {
	t.Parallel()
	for idx := uint8(0); idx < 3; idx++ {
		if got := RateName(idx); got == "?" {
			t.Errorf("RateName(%d) returned %q, expected a valid rate name", idx, got)
		}
	}
}
