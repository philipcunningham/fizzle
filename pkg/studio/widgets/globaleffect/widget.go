// Package globaleffect implements the Global Effect panel for fizzle
// studio.
//
// The panel is global per-file (no voice binding). It exposes the
// pitch-bend depth and the 3x7 controller-routing matrix from the bank-0
// effect block. Each matrix cell is its own InputField (not a Table) so
// every cell is editable; layout follows spec §2.3.3.
//
// The spec's mvol and suss bytes are unused per the format doc and are
// not displayed.
package globaleffect

import (
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/helpers"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
)

// maxByte is the documented upper bound for all effect-block byte fields.
const maxByte = 127

// Controller row labels match spec §2.3.3.
var controllerRows = []string{"Mod Wheel", "Foot Pedal", "Aftertouch"}

// Target column labels match spec §2.3.3.
var targetCols = []string{"LFO Pitch", "LFO Amp", "LFO Filter", "LFO Q", "DCA Level", "DCF Level", "DCF Q"}

// cellOffsets maps (row, col) -> effect-block byte offset (relative to
// BankEffectOffset). The matrix layout follows the spec's table:
//
//	row 0 = Mod Wheel  (mod_*)
//	row 1 = Foot Pedal (fot_*)
//	row 2 = Aftertouch (aft_*)
//
//	col 0 = LFO Pitch  (*_lfp)
//	col 1 = LFO Amp    (*_lfa)
//	col 2 = LFO Filter (*_lff)
//	col 3 = LFO Q      (*_lfq)
//	col 4 = DCA Level  (*_dca)
//	col 5 = DCF Level  (*_dcf)
//	col 6 = DCF Q      (*_dcq)
var cellOffsets = [3][7]int{
	{
		disk.EffectModLFPOffset,
		disk.EffectModLFAOffset,
		disk.EffectModLFFOffset,
		disk.EffectModLFQOffset,
		disk.EffectModDCAOffset,
		disk.EffectModDCFOffset,
		disk.EffectModDCQOffset,
	},
	{
		disk.EffectFotLFPOffset,
		disk.EffectFotLFAOffset,
		disk.EffectFotLFFOffset,
		disk.EffectFotLFQOffset,
		disk.EffectFotDCAOffset,
		disk.EffectFotDCFOffset,
		disk.EffectFotDCQOffset,
	},
	{
		disk.EffectAftLFPOffset,
		disk.EffectAftLFAOffset,
		disk.EffectAftLFFOffset,
		disk.EffectAftLFQOffset,
		disk.EffectAftDCAOffset,
		disk.EffectAftDCFOffset,
		disk.EffectAftDCQOffset,
	},
}

// Widget is the Global Effect panel.
type Widget struct {
	flex *tview.Flex
	m    *model.Model

	bendIF *tview.InputField
	cells  [3][7]*tview.InputField

	refreshing bool

	// currentSection tracks which "section" of the panel focus is in,
	// used by CycleSection (Shift+Tab) to advance forward with wrap.
	// 0 = Bend Range, 1 = controller matrix.
	currentSection int

	unsub func()

	// tApp is set via SetApp by the app shell so commit-on-Done +
	// non-InputField SetInputCapture handlers can advance focus when
	// Tab is pressed. Nil before SetApp is called; advance helpers
	// no-op safely.
	tApp *tview.Application

	// onCycleOut: see SetOnCycleOut.
	onCycleOut func()
}

// SetApp injects the tview.Application so the widget can advance focus
// on Tab. Call once after construction, before the user interacts.
func (w *Widget) SetApp(tApp *tview.Application) { w.tApp = tApp }

// SetOnCycleOut registers the cross-pane cycle continuation. Called by
// CycleSection when it would wrap from the last section back to the
// first; the app uses this to extend the Shift+Tab cycle into the
// upper pane.
func (w *Widget) SetOnCycleOut(fn func()) { w.onCycleOut = fn }

// Layout constants. cellW is wide enough for the longest column header
// ("LFO Filter" = 10) plus one column of padding. rowLabelW fits the
// longest controller name ("Aftertouch" = 10) plus padding.
const (
	cellW     = 11
	rowLabelW = 12
)

// New constructs the panel. The Global Effect block is in bank 0; there
// is no per-voice binding so the widget has no Bind method.
func New(m *model.Model) *Widget {
	w := &Widget{m: m}
	w.build()
	w.unsub = m.Subscribe(w.refresh)
	w.refresh()
	return w
}

// Primitive returns the panel's root primitive for embedding in the shell.
func (w *Widget) Primitive() tview.Primitive { return w.flex }

// Focus moves keyboard focus into the panel's first section (Bend
// Range). tApp.SetFocus(Primitive()) would land on the wrapping Flex
// which has no visible focus state.
func (w *Widget) Focus(tApp *tview.Application) {
	w.currentSection = 0
	w.focusSection(tApp, 0)
}

// CycleSection advances focus to the next section anchor. At the last
// section it resets to 0 and invokes onCycleOut so the app can hand
// focus to the upper pane (cross-pane Shift+Tab cycle). Without an
// onCycleOut callback it falls back to in-widget wraparound.
func (w *Widget) CycleSection(tApp *tview.Application) {
	anchors := w.sectionAnchors()
	if len(anchors) == 0 {
		return
	}
	if w.currentSection >= len(anchors)-1 {
		w.currentSection = 0
		if w.onCycleOut != nil {
			w.onCycleOut()
			return
		}
		w.focusSection(tApp, 0)
		return
	}
	w.currentSection++
	w.focusSection(tApp, w.currentSection)
}

func (w *Widget) sectionAnchors() []tview.Primitive {
	out := []tview.Primitive{}
	if w.bendIF != nil {
		out = append(out, w.bendIF)
	}
	if w.cells[0][0] != nil {
		out = append(out, w.cells[0][0])
	}
	return out
}

func (w *Widget) focusSection(tApp *tview.Application, idx int) {
	anchors := w.sectionAnchors()
	if len(anchors) == 0 {
		tApp.SetFocus(w.flex)
		return
	}
	if idx < 0 || idx >= len(anchors) {
		idx = 0
	}
	tApp.SetFocus(anchors[idx])
}

// InputFields returns every InputField in the panel: the Bend Range
// field followed by the 3x7 controller-routing matrix in row-major
// order. Used by the app shell's focused-field finder so a Ctrl+S
// flush after a mouse click can still locate the InputField whose
// embedded TextArea has focus.
func (w *Widget) InputFields() []*tview.InputField {
	out := []*tview.InputField{}
	if w.bendIF != nil {
		out = append(out, w.bendIF)
	}
	for r := 0; r < len(w.cells); r++ {
		for c := 0; c < len(w.cells[r]); c++ {
			if in := w.cells[r][c]; in != nil {
				out = append(out, in)
			}
		}
	}
	return out
}

// Close releases the model subscription. Idempotent.
func (w *Widget) Close() {
	if w.unsub != nil {
		w.unsub()
		w.unsub = nil
	}
}

// build assembles the tview primitives. Run once from New.
func (w *Widget) build() {
	// Bend Range InputField.
	w.bendIF = tview.NewInputField().SetLabel("Bend Range (0-127): ")
	w.bendIF.SetAcceptanceFunc(helpers.AcceptUnsigned(maxByte))
	w.bendIF.SetDoneFunc(func(key tcell.Key) {
		if w.refreshing || !isCommitKey(key) {
			return
		}
		s := w.bendIF.GetText()
		if s != "" {
			if v, err := strconv.Atoi(s); err == nil && v >= 0 && v <= maxByte {
				w.commitByte(disk.EffectBendOffset, byte(v)) //nolint:gosec // bounds checked
			}
		}
		w.handleDoneKey(key)
	})

	// 3x7 controller matrix. Layout uses tview.Grid so the column
	// headers, row labels, and value cells all align in fixed-width
	// columns; the previous Flex-of-Flexes with full per-cell labels
	// looked squashed because each row crammed 7 verbose labels into
	// the available width.
	//
	// Grid layout:
	//
	//   row 0:  [        ] LFO Pitch  LFO Amp   ... DCF Q
	//   row 1:  Mod Wheel    [val]    [val]    ...  [val]
	//   row 2:  Foot Pedal   [val]    [val]    ...  [val]
	//   row 3:  Aftertouch   [val]    [val]    ...  [val]
	//
	// Bend Range stays on its own line above the matrix.
	grid := tview.NewGrid()
	gridRows := make([]int, 1+len(controllerRows))
	for i := range gridRows {
		gridRows[i] = 1
	}
	grid.SetRows(gridRows...)
	gridCols := make([]int, 1+len(targetCols))
	gridCols[0] = rowLabelW
	for i := 1; i < len(gridCols); i++ {
		gridCols[i] = cellW
	}
	grid.SetColumns(gridCols...)

	// Column headers along row 0 (skipping the row-label column).
	for c, colName := range targetCols {
		header := tview.NewTextView()
		header.SetText(colName)
		header.SetTextColor(tview.Styles.SecondaryTextColor)
		grid.AddItem(header, 0, c+1, 1, 1, 0, 0, false)
	}

	// Row labels in column 0, plus cell InputFields filling the grid.
	for r, rowName := range controllerRows {
		label := tview.NewTextView()
		label.SetText(rowName)
		label.SetTextColor(tview.Styles.SecondaryTextColor)
		grid.AddItem(label, r+1, 0, 1, 1, 0, 0, false)

		for c := range targetCols {
			off := cellOffsets[r][c]
			in := w.newCellInput(off)
			w.cells[r][c] = in
			grid.AddItem(in, r+1, c+1, 1, 1, 0, 0, false)
		}
	}
	grid.SetBorder(true)
	grid.SetTitle(" Controller Routing ")

	w.flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(w.bendIF, 1, 0, false).
		AddItem(grid, 0, 1, false)
	w.flex.SetBorder(true)
	w.flex.SetTitle(" Global Effect ")
}

// newCellInput builds a label-less InputField wired with
// AcceptUnsigned(127) and a commit callback that writes one byte at the
// matching effect-block offset. The grid layout provides the column
// header and row label, so the cell itself just shows the value.
func (w *Widget) newCellInput(relOffset int) *tview.InputField {
	in := tview.NewInputField()
	in.SetFieldWidth(4)
	in.SetAcceptanceFunc(helpers.AcceptUnsigned(maxByte))
	in.SetDoneFunc(func(key tcell.Key) {
		if w.refreshing || !isCommitKey(key) {
			return
		}
		s := in.GetText()
		if s != "" {
			if v, err := strconv.Atoi(s); err == nil && v >= 0 && v <= maxByte {
				w.commitByte(relOffset, byte(v)) //nolint:gosec // bounds checked
			}
		}
		w.handleDoneKey(key)
	})
	return in
}

// isCommitKey is true for the keys that should trigger an InputField
// commit (Tab forward/back, Enter). Anything else (Escape, etc.) bows out.
func isCommitKey(k tcell.Key) bool {
	return k == tcell.KeyEnter || k == tcell.KeyTab || k == tcell.KeyBacktab
}

// fieldList returns every focusable primitive in the panel ordered for
// Tab navigation: Bend Range, then the 3x7 matrix row by row.
func (w *Widget) fieldList() []tview.Primitive {
	var out []tview.Primitive
	if w.bendIF != nil {
		out = append(out, w.bendIF)
	}
	for r := 0; r < len(w.cells); r++ {
		for c := 0; c < len(w.cells[r]); c++ {
			if w.cells[r][c] != nil {
				out = append(out, w.cells[r][c])
			}
		}
	}
	return out
}

// focusNextField advances focus to the next primitive in fieldList,
// wrapping at the end. No-op if SetApp hasn't been called.
func (w *Widget) focusNextField() {
	if w.tApp == nil {
		return
	}
	list := w.fieldList()
	if len(list) == 0 {
		return
	}
	current := w.tApp.GetFocus()
	for i, p := range list {
		if p == current {
			w.tApp.SetFocus(list[(i+1)%len(list)])
			return
		}
	}
	w.tApp.SetFocus(list[0])
}

// handleDoneKey applies focus-advance / section-cycle after a commit.
func (w *Widget) handleDoneKey(key tcell.Key) {
	switch key { //nolint:exhaustive // only Tab/Backtab here
	case tcell.KeyTab:
		w.focusNextField()
	case tcell.KeyBacktab:
		w.CycleSection(w.tApp)
	}
}

// effectByte returns the byte at the given effect-block-relative offset.
// Bank 0 starts at FZF offset 0, so the absolute offset is
// BankEffectOffset + rel.
func (w *Widget) effectByte(rel int) (byte, bool) {
	abs := disk.BankEffectOffset + rel
	if abs < 0 || abs >= len(w.m.Bytes()) {
		return 0, false
	}
	return w.m.Bytes()[abs], true
}

// refresh repaints every InputField from the in-memory bytes.
func (w *Widget) refresh() {
	w.refreshing = true
	defer func() { w.refreshing = false }()

	if b, ok := w.effectByte(disk.EffectBendOffset); ok {
		w.bendIF.SetText(strconv.Itoa(int(b)))
	}
	for r := 0; r < 3; r++ {
		for c := 0; c < 7; c++ {
			if b, ok := w.effectByte(cellOffsets[r][c]); ok {
				w.cells[r][c].SetText(strconv.Itoa(int(b)))
			}
		}
	}
}

// commitByte writes a single byte at BankEffectOffset+rel via Apply
// (FZF-absolute; not voice-relative).
func (w *Widget) commitByte(rel int, b byte) {
	_ = w.m.Apply(voiceedit.Patch{
		Offset: disk.BankEffectOffset + rel,
		Size:   1,
		Value:  uint16(b),
	})
}
