package lfovisual

import (
	"testing"
)

// TestView_AllWaveforms asserts every waveform value renders to a
// non-empty string at a reasonable cell size.
func TestView_AllWaveforms(t *testing.T) {
	cases := []struct {
		name string
		w    Waveform
	}{
		{"sine", Sine},
		{"saw-up", SawUp},
		{"saw-down", SawDown},
		{"triangle", Triangle},
		{"rectangle", Rectangle},
		{"random", Random},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := View(tc.w, 60, 12)
			if out == "" {
				t.Fatalf("View(%v, 60, 12) returned empty string", tc.w)
			}
		})
	}
}

// TestView_RandomDeterministic asserts that the random waveform is
// stable across renders: the LCG seed is fixed at 0, so the same
// dimensions must yield the same string.
func TestView_RandomDeterministic(t *testing.T) {
	a := View(Random, 60, 12)
	b := View(Random, 60, 12)
	if a != b {
		t.Errorf("View(Random, 60, 12) is non-deterministic:\nfirst:\n%s\nsecond:\n%s", a, b)
	}
}

// TestView_TinyDimensions drives the smallest sensible widget size.
// linechart still needs to honour the call without panicking.
func TestView_TinyDimensions(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("View panicked at tiny dimensions: %v", r)
		}
	}()
	_ = View(Sine, 10, 4)
}
