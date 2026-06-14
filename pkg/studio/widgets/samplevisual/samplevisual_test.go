package samplevisual

import (
	"math"
	"strings"
	"testing"
)

// sineWave produces 1024 samples of a single-cycle sine spanning
// the full int16 range. Good enough exercise for the binning logic
// without depending on real voice data. Length is fixed because all
// callers want the same shape; a parameter would be dead weight.
func sineWave() []int16 {
	const n = 1024
	data := make([]int16, n)
	for i := 0; i < n; i++ {
		phase := 2 * math.Pi * float64(i) / float64(n)
		data[i] = int16(math.Round(math.Sin(phase) * 32767))
	}
	return data
}

// TestView_SineWaveCaption asserts that the caption reports the gen
// range in the format "range N..M".
func TestView_SineWaveCaption(t *testing.T) {
	data := sineWave()
	s := Sample{
		Data:     data,
		GenStart: 0,
		GenEnd:   1023,
	}
	out := View(s, 60, 6)
	if out == "" {
		t.Fatal("View returned empty string")
	}
	if !strings.Contains(out, "range 0..1023") {
		t.Errorf("expected caption 'range 0..1023' in output, got:\n%s", out)
	}
}

// TestView_LoopReportedInCaption asserts that defining a single loop
// pair surfaces "loop 1: <start>..<end>" in the caption.
func TestView_LoopReportedInCaption(t *testing.T) {
	data := sineWave()
	s := Sample{
		Data:       data,
		GenStart:   0,
		GenEnd:     1023,
		LoopStarts: []int{256},
		LoopEnds:   []int{768},
	}
	out := View(s, 60, 6)
	if !strings.Contains(out, "loop 1: 256..768") {
		t.Errorf("expected caption 'loop 1: 256..768' in output, got:\n%s", out)
	}
}

// TestView_NoLoopsCaptionStaysShort asserts that a sample without
// loops keeps the caption to a single "range ..." entry.
func TestView_NoLoopsCaptionStaysShort(t *testing.T) {
	data := sineWave()
	s := Sample{
		Data:     data,
		GenStart: 0,
		GenEnd:   1023,
	}
	out := View(s, 60, 6)
	if strings.Contains(out, "loop") {
		t.Errorf("caption should not mention loops when none exist, got:\n%s", out)
	}
}

// TestView_NoDataMessage asserts the centred fallback message for an
// empty sample buffer.
func TestView_NoDataMessage(t *testing.T) {
	out := View(Sample{Data: nil}, 40, 8)
	if !strings.Contains(out, "no sample data") {
		t.Errorf("expected no-data message in output, got:\n%s", out)
	}
}

// TestView_TinyDimensionsNoPanic exercises the minimum-viable cell
// box. The renderer must not panic for very small widths/heights.
func TestView_TinyDimensionsNoPanic(t *testing.T) {
	data := sineWave()
	s := Sample{
		Data:       data,
		GenStart:   0,
		GenEnd:     1023,
		LoopStarts: []int{256},
		LoopEnds:   []int{768},
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("View panicked at tiny dimensions: %v", r)
		}
	}()
	out := View(s, 10, 4)
	if out == "" {
		t.Fatal("View returned empty string for tiny dimensions")
	}
}
