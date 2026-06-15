// Package effectseditor is the per-bank effects editor modal. It
// exposes the FZ-1's `effectdata` block (BankEffectOffset = 0x3c0,
// 24 bytes per bank sector):
//
//   - bend depth (1/8-semitone units, 0..127)
//   - a 3x7 controller modulation matrix (Mod Wheel / Foot Pedal /
//     Aftertouch sources × LFO Pitch / LFO Amp / LFO Filter / LFO Q
//     / DCA / DCF / DCQ destinations)
//
// Two legacy fields (mvol and suss) are documented as "normally 0"
// by the spec and aren't editable here; they round-trip unchanged.
//
// The modal is scoped to ONE bank (per-bank semantics matching the
// file format). Studio1 simplified by editing only bank 0; studio
// honours the per-bank layout.
package effectseditor

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/theme"
)

// numCells = 1 (bend) + 3 rows × 7 cols.
const numCells = 22

// cellOffset maps a 0..21 field index to the relative byte offset
// inside the effect block. Index 0 is bend; indices 1..21 walk the
// matrix in row-major order (row 0 cols 0..6, row 1 cols 0..6,
// row 2 cols 0..6).
var cellOffset = [numCells]int{
	disk.EffectBendOffset,
	// Row 0: Mod Wheel.
	disk.EffectModLFPOffset,
	disk.EffectModLFAOffset,
	disk.EffectModLFFOffset,
	disk.EffectModLFQOffset,
	disk.EffectModDCAOffset,
	disk.EffectModDCFOffset,
	disk.EffectModDCQOffset,
	// Row 1: Foot Pedal.
	disk.EffectFotLFPOffset,
	disk.EffectFotLFAOffset,
	disk.EffectFotLFFOffset,
	disk.EffectFotLFQOffset,
	disk.EffectFotDCAOffset,
	disk.EffectFotDCFOffset,
	disk.EffectFotDCQOffset,
	// Row 2: Aftertouch.
	disk.EffectAftLFPOffset,
	disk.EffectAftLFAOffset,
	disk.EffectAftLFFOffset,
	disk.EffectAftLFQOffset,
	disk.EffectAftDCAOffset,
	disk.EffectAftDCFOffset,
	disk.EffectAftDCQOffset,
}

// rowLabels and colLabels are used by the renderer; kept short so
// the modal stays narrow enough for default terminals.
//
// Source rows (MIDI controllers):
//
//	Mod  = Mod Wheel
//	Foot = Foot Pedal
//	Aft  = Aftertouch
//
// Destination columns (rendered as two-line headers so the meaning is
// readable without a legend):
//
//	LFO Pitch  = controller -> LFO pitch-modulation depth
//	LFO Amp    = controller -> LFO amplitude-modulation depth
//	LFO Filt   = controller -> LFO filter-modulation depth
//	LFO Q      = controller -> LFO resonance-modulation depth
//	DC Amp     = controller -> direct amplitude offset
//	DC Filt    = controller -> direct filter cutoff offset
//	DC Q       = controller -> direct resonance offset
var rowLabels = []string{"Mod Wheel", "Foot Pedal", "Aftertouch"}

// colHeaders are short header tokens above each matrix column; a
// legend line below the matrix expands them:
//
//	LFP/LFA/LFF/LFQ = LFO Pitch / Amp / Filter / Q
//	DCA/DCF/DCQ     = direct Amp / Filter / Q
var colHeaders = []string{"LFP", "LFA", "LFF", "LFQ", "DCA", "DCF", "DCQ"}

// Matrix column geometry. Each cell is a 6-char field: " %5s" for
// headers, " %5d" for values. The leading space gives air between
// columns; the 5-char body fits "LFP" through 3-digit values.
const matrixColWidth = 6

// Matrix label column width. "Aftertouch" = 10 chars + 2 pad = 12.
const matrixLabelWidth = 12

// Model is the modal state.
type Model struct {
	open    bool
	bankIdx int

	cells       [numCells]uint8
	originCells [numCells]uint8
	field       int // 0..numCells-1
}

// SeedValues carries the 22 byte values the App reads from the bank
// sector to seed the modal.
type SeedValues struct {
	Cells [numCells]uint8
}

// OffsetAt returns the relative byte offset within the effect block
// for field index i (0..numCells-1). Provided as a method on
// SeedValues so the App can compute disk offsets at seed time, when
// no Model is bound yet.
func (SeedValues) OffsetAt(i int) int {
	if i < 0 || i >= numCells {
		return 0
	}
	return cellOffset[i]
}

// New returns a closed modal.
func New() Model { return Model{} }

// Open binds the modal to bankIdx with the supplied seed values.
// Focus starts on the bend cell.
func (m *Model) Open(bankIdx int, vals SeedValues) {
	m.open = true
	m.bankIdx = bankIdx
	m.cells = vals.Cells
	m.originCells = vals.Cells
	m.field = 0
}

// Close clears modal state.
func (m *Model) Close() { m.open = false }

// IsOpen reports whether the modal is shown.
func (m Model) IsOpen() bool { return m.open }

// BankIdx returns the modal's target.
func (m Model) BankIdx() int { return m.bankIdx }

// Cells returns the current editable byte values, indexed by cellOffset.
func (m Model) Cells() [numCells]uint8 { return m.cells }

// CellOffset returns the relative byte offset (within the effect
// block) for field index i. Callers add BankEffectOffset to it to
// get the bank-sector offset.
func (m Model) CellOffset(i int) int {
	if i < 0 || i >= numCells {
		return 0
	}
	return cellOffset[i]
}

// Changed reports whether any cell diverged from the seed.
func (m Model) Changed() bool {
	for i := 0; i < numCells; i++ {
		if m.cells[i] != m.originCells[i] {
			return true
		}
	}
	return false
}

// HandleKey advances modal state for one keypress. Tab / Shift+Tab
// cycle through cells row-major. Up/Down step the focused cell by
// 1; Shift+Up/Down by 10.
func (m *Model) HandleKey(s string) {
	switch s {
	case "tab":
		m.field = (m.field + 1) % numCells
	case "shift+tab":
		m.field = (m.field - 1 + numCells) % numCells
	case "up":
		m.step(+1)
	case "down":
		m.step(-1)
	case "shift+up":
		m.step(+10)
	case "shift+down":
		m.step(-10)
	}
}

func (m *Model) step(delta int) {
	v := int(m.cells[m.field]) + delta
	if v < 0 {
		v = 0
	}
	if v > 127 {
		v = 127
	}
	m.cells[m.field] = uint8(v)
}

// View renders the modal body.
func (m Model) View() string {
	if !m.open {
		return ""
	}
	title := theme.Heading.Render(fmt.Sprintf("Edit Effects: Bank %d", m.bankIdx+1))

	// Bend row.
	bendVal := fmt.Sprintf("%d (%.2f semitones)", m.cells[0], float64(m.cells[0])/8.0)
	bendLine := theme.Field("Bend Depth", bendVal, m.field == 0)

	// Modulation matrix: 3 rows × 7 cols, hand-rolled with fixed-width
	// columns. Each numeric cell is right-aligned in 5 chars + a
	// leading space (matrixColWidth = 6), and the row-label column
	// is left-aligned in matrixLabelWidth chars so the header band,
	// data rows, and legend underneath all line up.
	var hdr strings.Builder
	hdr.WriteString(strings.Repeat(" ", matrixLabelWidth))
	for _, h := range colHeaders {
		hdr.WriteString(theme.DimText.Render(fmt.Sprintf(" %*s", matrixColWidth-1, h)))
	}
	matrixLines := []string{hdr.String()}
	for r := 0; r < 3; r++ {
		var line strings.Builder
		line.WriteString(theme.PrimaryText.Render(fmt.Sprintf("%-*s", matrixLabelWidth, rowLabels[r])))
		for c := 0; c < 7; c++ {
			idx := 1 + r*7 + c
			if m.field == idx {
				// Bracket the focused cell so focus reads on shape, not
				// colour alone (N-02), while keeping the 6-char column
				// width: "[" + 4-wide value + "]".
				cell := fmt.Sprintf("[%*d]", matrixColWidth-2, m.cells[idx])
				line.WriteString(theme.AccentText.Underline(true).Render(cell))
			} else {
				cell := fmt.Sprintf(" %*d", matrixColWidth-1, m.cells[idx])
				line.WriteString(theme.DimText.Render(cell))
			}
		}
		matrixLines = append(matrixLines, line.String())
	}
	legend := theme.DimText.Render(
		"  LF*=LFO Pitch/Amp/Filter/Q   DC*=direct Amp/Filter/Q")
	matrixLines = append(matrixLines, "", legend)
	matrix := strings.Join(matrixLines, "\n")

	hint := theme.DimText.Render(
		"tab cycle field  •  up/down step  •  shift+up/down big step  •  enter commit  •  esc cancel")

	body := strings.Join([]string{
		title,
		"",
		bendLine,
		"",
		matrix,
		"",
		hint,
	}, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 3).
		Render(body)
}
