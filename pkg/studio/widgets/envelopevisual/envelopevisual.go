// Package envelopevisual renders an FZ 8-stage envelope (DCA or DCF)
// as a braille curve using ntcharts. Time is on X (in milliseconds),
// level on Y (0..99 display scale). Breakpoint glyphs mark each
// stage; an optional highlight glyph distinguishes the selected
// stage; `S` and `E` markers sit above the Sus and End stages.
//
// The widget mirrors the visual approach validated in
// ~/Code/fizzle-envelope-spike. Time computation is firmware
// faithful: the verified 128-entry envelope rate table at CS:0x0490
// drives `|level_delta| * 256 / envRateTable[rate & 0x7F]` ticks per
// stage, multiplied by ~25 ms per DCA service tick.
package envelopevisual

import (
	"github.com/NimbleMarkets/ntcharts/v2/canvas"
	"github.com/NimbleMarkets/ntcharts/v2/linechart"

	"charm.land/lipgloss/v2"

	"github.com/philipcunningham/fizzle/pkg/studio/theme"
)

// numStages is the FZ envelope stage count.
const numStages = 8

// Envelope captures the data needed to render an envelope curve.
type Envelope struct {
	// Sus is the stage index (0..7) that the envelope sustains at on
	// note-on.
	Sus int
	// End is the stage index (0..7) that the envelope ends at on
	// note-off.
	End int
	// Rates are the per-stage rate bytes. Bit 7 is the sign/direction
	// sentinel; bits 0..6 are the magnitude (0..127) used to index
	// the firmware rate table.
	Rates [numStages]uint8
	// StopLevels are the per-stage stop levels (0..255).
	StopLevels [numStages]uint8
}

// envRateTable is the 128-entry, 16-bit envelope-rate lookup table
// at CS:0x0490 in the FZ ROM. Verified against fizzlab's annotation of
// fn 36 voice_state_2_6_handler (F000:218B: `MOV AX, CS:[BX+0x0490]`)
// and the identical-shape DCA handler fn 33 at F000:2039.
//
// Per the firmware: each per-voice service tick reads the rate byte
// for the current envelope stage, indexes this table with `rate & 0x7F`
// to fetch the table value, negates the value if bit 7 of the rate
// byte was set, and adds the result to the 16-bit ramp accumulator at
// [DI+0x26] (DCF) / [DI+0x12] (DCA). When the accumulator high byte
// crosses the stage's stop level, the step counter advances and the
// envelope moves to the next stage.
//
// Source: docs/firmware/disasm/fizzlab-fz1-os-raw.asm rows 1169..1424
// (F000:0490..F000:058F) in fizzlab.
var envRateTable = [128]uint16{
	0x0000, 0x0003, 0x0006, 0x0009, 0x000D, 0x0010, 0x0014, 0x0018,
	0x001C, 0x0021, 0x0025, 0x002A, 0x002F, 0x0034, 0x003A, 0x0040,
	0x0046, 0x004D, 0x0054, 0x005B, 0x0063, 0x006B, 0x0073, 0x007C,
	0x0085, 0x008F, 0x0099, 0x00A4, 0x00AF, 0x00BB, 0x00C8, 0x00D5,
	0x00E3, 0x00F1, 0x0101, 0x0111, 0x0122, 0x0133, 0x0146, 0x015A,
	0x016E, 0x0184, 0x019B, 0x01B3, 0x01CC, 0x01E7, 0x0203, 0x0220,
	0x023F, 0x025F, 0x0281, 0x02A5, 0x02CB, 0x02F2, 0x031C, 0x0348,
	0x0376, 0x03A6, 0x03D9, 0x040E, 0x0446, 0x0481, 0x04BF, 0x0501,
	0x0545, 0x058D, 0x05D9, 0x0629, 0x067D, 0x06D5, 0x0731, 0x0793,
	0x07F9, 0x0865, 0x08D6, 0x094D, 0x09CA, 0x0A4D, 0x0AD7, 0x0B68,
	0x0C01, 0x0CA2, 0x0D4A, 0x0DFC, 0x0EB6, 0x0F7A, 0x1048, 0x1121,
	0x1205, 0x12F4, 0x13F0, 0x14F8, 0x160F, 0x1733, 0x1867, 0x19AA,
	0x1AFE, 0x1C63, 0x1DDA, 0x1F65, 0x2104, 0x22B8, 0x2483, 0x2665,
	0x2860, 0x2A75, 0x2CA5, 0x2EF2, 0x315D, 0x33E8, 0x3694, 0x3964,
	0x3C58, 0x3F73, 0x42B6, 0x4625, 0x49C1, 0x4D8C, 0x5188, 0x55B9,
	0x5A22, 0x5EC4, 0x63A2, 0x68C1, 0x6E23, 0x73CB, 0x79BE, 0x7FFF,
}

// displayMax mirrors disk.DisplayMax: levels are surfaced on the
// 0..99 scale that matches the FZ-1 / FZ-10M front panel.
const displayMax = 99

// envelopeFullLevel mirrors disk.EnvelopeFullLevel: the maximum stop
// level byte.
const envelopeFullLevel = 255

// msPerDCATick is the approximate wall-clock time between two calls
// to the DCA state-3/7 handler. Derived from fizzlab's
// fizzlab-voice-state-machine.md: the per-voice service routine at
// 0x1CD8 runs every 8 timer IRQs (~6.4 ms each), an 8-phase round is
// ~50 ms wall-clock, and state 3 / state 7 each run once per round,
// so the DCA state runs roughly every 25 ms. Approximate, not
// measured against hardware.
const msPerDCATick = 25.0

// minMs is the minimum visible spacing between two breakpoints. A
// plateau (level_delta = 0) would otherwise collapse to zero width.
const minMs = 5.0

// minTotalMs is the minimum visible X range. Very short envelopes
// would otherwise collapse the chart.
const minTotalMs = 500.0

// labelOffsetY is the Y offset (in chart units) used to lift S / E
// glyphs above the breakpoint dots so they do not collide.
const labelOffsetY = 7.0

// stopByteToDisplay converts a stop level byte (0..255) to the 0..99
// display scale. Mirrors disk.StopByteToDisplay.
func stopByteToDisplay(b uint8) int {
	if b == 0 {
		return 0
	}
	return (int(b)*displayMax + envelopeFullLevel - 1) / envelopeFullLevel
}

// lastVisibleStage is the highest stage index the chart must draw.
// It includes both End (last played stage) and Sus (sustain pointer),
// since either can sit beyond the other depending on the voice (e.g.,
// FZ-1 factory piano has Sus = 7, End = 4).
func lastVisibleStage(env Envelope) int {
	last := env.End
	if env.Sus > last {
		last = env.Sus
	}
	if last < 0 {
		last = 0
	}
	if last > numStages-1 {
		last = numStages - 1
	}
	return last
}

// breakpointTimes returns the cumulative time in ms at which each
// breakpoint from stage 0..last is reached. Mirrors the firmware:
// per-tick the ramp accumulator advances by envRateTable[rate & 0x7F],
// and a stage completes when the accumulator high byte crosses the
// stop level. Ticks per stage are |level_delta| * 256 / table value,
// converted to ms via msPerDCATick. A table value of 0 (rate 0) would
// freeze the envelope; floor to 1 so the breakpoint stays visible.
func breakpointTimes(env Envelope, last int) []float64 {
	const highByteScale = 256.0
	t := make([]float64, last+1)
	for i := 1; i <= last; i++ {
		prev := int(env.StopLevels[i-1])
		cur := int(env.StopLevels[i])
		delta := cur - prev
		if delta < 0 {
			delta = -delta
		}
		tbl := float64(envRateTable[env.Rates[i]&0x7f])
		if tbl < 1 {
			tbl = 1
		}
		ticks := float64(delta) * highByteScale / tbl
		dtMs := ticks * msPerDCATick
		if dtMs < minMs {
			dtMs = minMs
		}
		t[i] = t[i-1] + dtMs
	}
	return t
}

// Styles for the rendered curve, breakpoints, and overlay glyphs.
// Pulled from the studio palette so the widget matches the rest of
// the TUI.
var (
	axisStyle     = lipgloss.NewStyle().Foreground(theme.Graphics)
	labelStyle    = lipgloss.NewStyle().Foreground(theme.Secondary)
	lineStyle     = lipgloss.NewStyle().Foreground(theme.Secondary)
	markerStyle   = lipgloss.NewStyle().Foreground(theme.Secondary)
	selectedStyle = lipgloss.NewStyle().Foreground(theme.Primary)
	susEndStyle   = lipgloss.NewStyle().Foreground(theme.Primary)
)

// View renders the envelope as a braille curve.
//
// selectedStage is the stage that should be highlighted (with a
// different breakpoint glyph). Pass -1 for no highlight. width and
// height are the cell dimensions in terminal cells.
func View(env Envelope, selectedStage, width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	last := lastVisibleStage(env)
	times := breakpointTimes(env, last)

	totalT := times[len(times)-1]
	if totalT < minTotalMs {
		totalT = minTotalMs
	}

	// Data X range is set wide enough to cover any factory envelope;
	// SetViewXRange scales the visible range to the current envelope.
	lc := linechart.New(
		width, height,
		0, totalT,
		0, 100,
		linechart.WithXYSteps(1, 10),
		linechart.WithStyles(axisStyle, labelStyle, lineStyle),
	)
	lc.SetViewXRange(0, totalT)
	lc.Clear()
	lc.DrawXYAxisAndLabel()

	pts := make([]canvas.Float64Point, last+1)
	for i := 0; i <= last; i++ {
		pts[i] = canvas.Float64Point{
			X: times[i],
			Y: float64(stopByteToDisplay(env.StopLevels[i])),
		}
	}

	// Curve segments. Each segment's slope reflects its rate: high
	// rate compresses X, giving a steep line; low rate stretches X,
	// giving a shallow one.
	for i := 0; i < len(pts)-1; i++ {
		lc.DrawBrailleLineWithStyle(pts[i], pts[i+1], lineStyle)
	}

	// Breakpoint markers. Selected stage uses a diamond in the primary
	// colour; others use a dot in the secondary colour.
	for i, p := range pts {
		marker := '●'
		style := markerStyle
		if i == selectedStage {
			marker = '◆'
			style = selectedStyle
		}
		lc.DrawRuneWithStyle(p, marker, style)
	}

	// S / E overlay markers, lifted above each breakpoint so they do
	// not collide with the dot. If Sus and End land in the same chart
	// cell, stack them. Stages with the same level and only minMs
	// apart often collapse to a single cell at typical widget widths
	// (the piano voice's Sus=7 / End=4 case is the canonical example),
	// so we test in chart units, not just on stage-index equality.
	susValid := env.Sus >= 0 && env.Sus <= last
	endValid := env.End >= 0 && env.End <= last
	collide := susValid && endValid && cellsClose(pts[env.Sus], pts[env.End], totalT, width)
	if endValid {
		endPt := canvas.Float64Point{
			X: pts[env.End].X,
			Y: pts[env.End].Y + labelOffsetY,
		}
		lc.DrawRuneWithStyle(endPt, 'E', susEndStyle)
	}
	if susValid {
		susPt := canvas.Float64Point{
			X: pts[env.Sus].X,
			Y: pts[env.Sus].Y + labelOffsetY,
		}
		if collide {
			susPt.Y += labelOffsetY
		}
		lc.DrawRuneWithStyle(susPt, 'S', susEndStyle)
	}

	return lc.View()
}

// cellsClose reports whether two chart points would render into the
// same (or adjacent) terminal cell on the X axis. The linechart
// reserves a few cells for the Y-axis labels and gutter, so we use
// width-4 as a conservative estimate of the plot width.
func cellsClose(a, b canvas.Float64Point, totalT float64, width int) bool {
	if totalT <= 0 {
		return true
	}
	plot := float64(width - 4)
	if plot < 1 {
		plot = 1
	}
	dx := a.X - b.X
	if dx < 0 {
		dx = -dx
	}
	// Two points collide when the X delta is less than one cell of the
	// plot area's horizontal resolution.
	return dx*plot < totalT
}
