package webcore

import (
	"testing"
)

// Peaks serves the zoomable waveform (R17): min/max pairs per bucket
// over a requested frame window, decoded by the same code the CLI's
// extract uses.
func TestPeaksShape(t *testing.T) {
	s, voice := importedSession(t)

	pairs, cerr := s.Peaks(voice, 0, 3000, 100)
	if cerr != nil {
		t.Fatalf("Peaks: %v", cerr)
	}
	if len(pairs) != 200 {
		t.Fatalf("len = %d, want 100 min/max pairs interleaved", len(pairs))
	}
	for i := 0; i < len(pairs); i += 2 {
		if pairs[i] > pairs[i+1] {
			t.Fatalf("bucket %d: min %d > max %d", i/2, pairs[i], pairs[i+1])
		}
	}
}

func TestPeaksWindowing(t *testing.T) {
	s, voice := importedSession(t)

	// The imported ramp repeats every 199 samples, so distinct windows
	// carry distinct peaks; a windowed request must not return the
	// whole file's shape.
	whole, cerr := s.Peaks(voice, 0, 3000, 10)
	if cerr != nil {
		t.Fatalf("Peaks: %v", cerr)
	}
	window, cerr := s.Peaks(voice, 0, 100, 10)
	if cerr != nil {
		t.Fatalf("Peaks window: %v", cerr)
	}
	same := true
	for i := range whole {
		if whole[i] != window[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("windowed peaks equal whole-file peaks")
	}
}

func TestPeaksClampsAndRejects(t *testing.T) {
	s, voice := importedSession(t)

	// A window past the end clamps rather than failing.
	pairs, cerr := s.Peaks(voice, 2900, 90000, 10)
	if cerr != nil {
		t.Fatalf("Peaks: %v", cerr)
	}
	if len(pairs) != 20 {
		t.Fatalf("len = %d, want 20", len(pairs))
	}

	if _, cerr := s.Peaks(voice, 0, 100, 0); cerr == nil || cerr.Code != codeInvalidValue {
		t.Fatalf("zero buckets: %v, want invalid-value", cerr)
	}
	if _, cerr := s.Peaks("NOWHERE", 0, 100, 10); cerr == nil || cerr.Code != codeNotFound {
		t.Fatalf("missing file: %v, want not-found", cerr)
	}
	if _, cerr := NewSession().Peaks(voice, 0, 100, 10); cerr == nil || cerr.Code != codeNoDisk {
		t.Fatalf("no disk: %v, want no-disk", cerr)
	}
}
