package lfovisual

import (
	"math"
	"regexp"
	"strings"
	"testing"
)

// ansiRe strips SGR escape sequences so axis-label assertions see plain
// text (ntcharts styles each label rune individually, so "-1" is not a
// literal substring of the raw output).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// TestWaveformY_TwoCyclesNotFour pins F-QA-29: the documented "two
// cycles" must actually be drawn. Each formula must have period 1 in x
// (so [0, 2] = two cycles), not period 0.5 (which rendered four). SawUp
// is the clearest probe.
func TestWaveformY_TwoCyclesNotFour(t *testing.T) {
	const eps = 1e-9
	if a, b := waveformY(SawUp, 0.1, 0), waveformY(SawUp, 1.1, 0); math.Abs(a-b) > eps {
		t.Errorf("SawUp should repeat with period 1: f(0.1)=%v f(1.1)=%v", a, b)
	}
	if a, b := waveformY(SawUp, 0.1, 0), waveformY(SawUp, 0.6, 0); math.Abs(a-b) <= eps {
		t.Errorf("SawUp has period 0.5 (4 cycles over [0,2]); want period 1 (2 cycles): f(0.1)=%v f(0.6)=%v", a, b)
	}
}

// TestSamplePoints_FullAmplitudeWithinAxis pins F-QA-28's data side: the
// Y range is exactly ±1 and the curve uses the full amplitude (peaks at
// ±1, no 0.9 squish), so peaks sit on the gridlines, never above them.
func TestSamplePoints_FullAmplitudeWithinAxis(t *testing.T) {
	if yMin != -1.0 || yMax != 1.0 {
		t.Fatalf("Y range = [%v, %v], want [-1, 1]", yMin, yMax)
	}
	const eps = 1e-9
	for _, w := range []Waveform{Sine, SawUp, SawDown, Triangle, Rectangle} {
		var maxY, minY float64
		for _, p := range samplePoints(w) {
			if p.Y > 1+eps || p.Y < -1-eps {
				t.Errorf("waveform %v: sample Y=%v outside [-1, 1]", w, p.Y)
			}
			maxY = math.Max(maxY, p.Y)
			minY = math.Min(minY, p.Y)
		}
		// Peaks should reach ~±1 (sampling can miss the exact sawtooth tip
		// by one step); the old 0.9 squish would land near ±0.88.
		if maxY < 0.95 || minY > -0.95 {
			t.Errorf("waveform %v: amplitude not full (max=%v min=%v); peaks should reach ±1", w, maxY, minY)
		}
	}
}

// TestAxisLabels pins F-QA-28: labels are placed by index at the true
// ends/middle (-1/0/1 on Y, 0/1/2 on X) and "-0" is impossible.
func TestAxisLabels(t *testing.T) {
	const gh = 8
	if got := yLabelAt(0, gh); got != "-1" {
		t.Errorf("yLabelAt(bottom) = %q, want -1", got)
	}
	if got := yLabelAt(gh, gh); got != "1" {
		t.Errorf("yLabelAt(top) = %q, want 1", got)
	}
	if got := yLabelAt(gh/2, gh); got != "0" {
		t.Errorf("yLabelAt(middle) = %q, want 0", got)
	}
	for i := 0; i <= gh; i++ {
		if got := yLabelAt(i, gh); got == "-0" {
			t.Errorf("yLabelAt(%d) produced a -0 label", i)
		}
	}
	const gw = 12
	if got := xLabelAt(0, gw); got != "0" {
		t.Errorf("xLabelAt(left) = %q, want 0", got)
	}
	if got := xLabelAt(gw-1, gw); got != "2" {
		t.Errorf("xLabelAt(right edge) = %q, want 2", got)
	}
	if got := xLabelAt(gw/2, gw); got != "1" {
		t.Errorf("xLabelAt(middle) = %q, want 1", got)
	}
}

// TestView_NoNegativeZeroLabel pins that the rendered Y gutter never
// shows "-0" and does carry the "-1" end label.
func TestView_NoNegativeZeroLabel(t *testing.T) {
	out := stripANSI(View(Sine, 60, 12))
	if strings.Contains(out, "-0") {
		t.Errorf("rendered chart contains a -0 axis label:\n%s", out)
	}
	if !strings.Contains(out, "-1") {
		t.Errorf("rendered chart missing the -1 axis label:\n%s", out)
	}
}

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
