// Package lfovisual renders one of the six FZ-1 LFO waveforms as a
// braille curve using ntcharts. Two cycles are drawn horizontally so
// the shape of each waveform reads at a glance, independent of any
// configured LFO rate. The visualisation is purely illustrative; it
// does not depend on the firmware LFO state.
package lfovisual

import (
	"math"

	"github.com/NimbleMarkets/ntcharts/v2/canvas"
	"github.com/NimbleMarkets/ntcharts/v2/linechart"

	"charm.land/lipgloss/v2"

	"github.com/philipcunningham/fizzle/pkg/studio/theme"
)

// Waveform enumerates the six FZ LFO waveforms in the order the
// firmware uses.
type Waveform int

const (
	// Sine is a smooth sinusoid.
	Sine Waveform = iota
	// SawUp is a rising sawtooth (ramps up, snaps down).
	SawUp
	// SawDown is a falling sawtooth (ramps down, snaps up).
	SawDown
	// Triangle is a symmetric triangle wave.
	Triangle
	// Rectangle is a 50% duty-cycle square wave.
	Rectangle
	// Random is a deterministic pseudo-random waveform.
	Random
)

// numSamples is the number of points sampled across the X axis. 200
// keeps the curve smooth in braille without wasting work.
const numSamples = 200

// xMin / xMax cover two cycles of the waveform.
const (
	xMin = 0.0
	xMax = 2.0
)

// yMin / yMax give the peaks a small margin so they are not clipped
// against the chart edge.
const (
	yMin = -1.1
	yMax = 1.1
)

// Styles for the rendered curve. Pulled from the studio palette so
// the widget matches the rest of the TUI.
var (
	axisStyle  = lipgloss.NewStyle().Foreground(theme.Graphics)
	labelStyle = lipgloss.NewStyle().Foreground(theme.Secondary)
	lineStyle  = lipgloss.NewStyle().Foreground(theme.Secondary)
)

// View renders the selected waveform as a braille curve. width and
// height are the cell dimensions; the function returns the rendered
// string ready to embed in a parent view.
func View(w Waveform, width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	lc := linechart.New(
		width, height,
		xMin, xMax,
		yMin, yMax,
		linechart.WithXYSteps(1, 1),
		linechart.WithStyles(axisStyle, labelStyle, lineStyle),
	)
	lc.Clear()
	lc.DrawXYAxisAndLabel()

	pts := samplePoints(w)
	for i := 0; i < len(pts)-1; i++ {
		lc.DrawBrailleLineWithStyle(pts[i], pts[i+1], lineStyle)
	}

	return lc.View()
}

// samplePoints produces the (x, y) sample pairs for the requested
// waveform. X spans [xMin, xMax]; Y is in [-1, +1].
func samplePoints(w Waveform) []canvas.Float64Point {
	pts := make([]canvas.Float64Point, numSamples)
	step := (xMax - xMin) / float64(numSamples-1)
	for i := 0; i < numSamples; i++ {
		x := xMin + float64(i)*step
		pts[i] = canvas.Float64Point{X: x, Y: waveformY(w, x, i)}
	}
	return pts
}

// waveformY returns the Y value for the given waveform at sample
// index i (X = x). The formulas mirror the spec in the package
// documentation; two cycles fit in the X range [0, 2] because every
// formula folds X by a factor of 2.
func waveformY(w Waveform, x float64, i int) float64 {
	switch w {
	case Sine:
		return math.Sin(2 * math.Pi * x * 2)
	case SawUp:
		return 2*frac(x*2) - 1
	case SawDown:
		return 1 - 2*frac(x*2)
	case Triangle:
		return 4*math.Abs(frac(x*2+0.25)-0.5) - 1
	case Rectangle:
		if frac(x*2) < 0.5 {
			return 1
		}
		return -1
	case Random:
		return randomY(i)
	}
	return 0
}

// frac returns the fractional part of x in [0, 1). math.Mod can
// return a negative result for negative inputs; this helper handles
// only the non-negative case (the only one used here) and avoids the
// extra branch.
func frac(x float64) float64 {
	return x - math.Floor(x)
}

// lcgState is the deterministic linear-congruential-generator state
// used by randomY. Constants are the classic Numerical Recipes ranqd1
// values; the choice is arbitrary, only determinism matters.
const (
	lcgMul  = 1664525
	lcgAdd  = 1013904223
	lcgMask = uint32(0xFFFFFFFF)
)

// randomY advances a deterministic LCG by i steps from seed 0 and
// normalises the output to [-1, +1]. The same i always yields the
// same Y, so the visualisation is stable across renders.
func randomY(i int) float64 {
	s := uint32(0)
	for k := 0; k <= i; k++ {
		s = (s*lcgMul + lcgAdd) & lcgMask
	}
	// Top 16 bits give a smoother distribution than the bottom 16
	// (classic LCG weakness). Scale 0..65535 to -1..+1.
	high := float64((s >> 16) & 0xFFFF)
	return high/32767.5 - 1.0
}
