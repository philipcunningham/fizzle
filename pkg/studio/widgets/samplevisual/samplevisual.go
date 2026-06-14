// Package samplevisual renders a voice's PCM sample data as a plain
// braille waveform shape, with the playable range and loop pointers
// described in plain text below the shape.
//
// Design rationale: a TUI sample overview has very limited resolution,
// and the user does not benefit from PCM amplitude axis labels (the
// numeric range -32768..32767 is meaningless to a musician), sample-
// index X-axis ticks (the relative position within the buffer is
// already evident), or coloured region bands (hard to read on many
// terminals). The shape carries the only information that matters
// visually; everything else is a single line of text.
//
// The widget targets the studio Sound space's Sample row visual cell.
package samplevisual

import (
	"fmt"
	"strings"

	"github.com/NimbleMarkets/ntcharts/v2/canvas"
	"github.com/NimbleMarkets/ntcharts/v2/canvas/graph"

	"charm.land/lipgloss/v2"

	"github.com/philipcunningham/fizzle/pkg/studio/theme"
)

// Sample describes the audio data to visualise.
type Sample struct {
	// Data is signed 16-bit PCM samples; range -32768..32767.
	Data []int16
	// GenStart and GenEnd are the playable region in samples
	// (absolute indices into Data). Reported in the text caption.
	GenStart, GenEnd int
	// LoopStarts and LoopEnds are pairs of loop boundary sample
	// indices. Empty if the voice has no loops.
	LoopStarts []int
	LoopEnds   []int
}

// View renders the sample data. width and height are the cell
// dimensions of the visual area; the text caption is rendered below
// and adds extra rows.
func View(s Sample, width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	if len(s.Data) == 0 {
		return theme.SilverText.Render(
			lipgloss.Place(
				width, height,
				lipgloss.Center, lipgloss.Center,
				"no sample data",
			),
		)
	}

	shape := renderShape(s.Data, width, height)
	caption := renderCaption(s)
	return shape + "\n" + caption
}

// renderShape draws the waveform as a centred min/max braille shape.
// No axes, no labels: every braille sub-column is one column of the
// envelope, and amplitude maps to a vertical sub-row in the canvas.
func renderShape(data []int16, width, height int) string {
	c := canvas.New(width, height)

	subCols := width * 2  // braille has 2 sub-columns per cell on X
	subRows := height * 4 // ... and 4 sub-rows per cell on Y
	if subCols < 1 || subRows < 1 {
		return c.View()
	}
	mid := subRows / 2
	n := len(data)

	bg := graph.NewBrailleGrid(width, height, 0, float64(subCols), 0, float64(subRows))
	for sc := 0; sc < subCols; sc++ {
		startIdx := sc * n / subCols
		endIdx := (sc + 1) * n / subCols
		if endIdx <= startIdx {
			endIdx = startIdx + 1
		}
		if endIdx > n {
			endIdx = n
		}
		if startIdx >= n {
			break
		}
		minV := int32(data[startIdx])
		maxV := minV
		for i := startIdx + 1; i < endIdx; i++ {
			v := int32(data[i])
			if v < minV {
				minV = v
			}
			if v > maxV {
				maxV = v
			}
		}
		// Map amplitude to braille sub-row. maxV positive lands above
		// the midline; minV negative lands below.
		topRow := mid - int(int64(maxV)*int64(mid)/32768)
		botRow := mid - int(int64(minV)*int64(mid)/32768)
		if topRow < 0 {
			topRow = 0
		}
		if botRow >= subRows {
			botRow = subRows - 1
		}
		for y := topRow; y <= botRow; y++ {
			bg.Set(bg.GridPoint(canvas.Float64Point{X: float64(sc), Y: float64(y)}))
		}
	}
	graph.DrawBraillePatterns(&c, canvas.Point{X: 0, Y: 0}, bg.BraillePatterns(), waveformStyle)
	return c.View()
}

var (
	waveformStyle = lipgloss.NewStyle().Foreground(theme.Secondary)
	captionStyle  = theme.DimText
)

// renderCaption writes the gen range and active loops as plain text
// below the waveform. Inactive loops are skipped so a one-shot voice
// shows a one-line caption.
func renderCaption(s Sample) string {
	parts := []string{
		fmt.Sprintf("range %d..%d", s.GenStart, s.GenEnd),
	}
	for i := range s.LoopStarts {
		if i >= len(s.LoopEnds) {
			break
		}
		parts = append(parts,
			fmt.Sprintf("loop %d: %d..%d", i+1, s.LoopStarts[i], s.LoopEnds[i]))
	}
	return captionStyle.Render(strings.Join(parts, "    "))
}
