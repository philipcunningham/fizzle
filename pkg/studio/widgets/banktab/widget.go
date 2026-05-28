// Package banktab implements the upper-section bank tab widget for
// fizzle studio. One Widget represents one bank: a vertical Flex with
// three stacked sub-widgets (a bank-rename InputField at the top, a
// read-only area-list Table in the middle, and an area-detail editor
// Flex at the bottom).
//
// Bank rename is the only structural operation exposed on this tab;
// every other edit is a 1-byte (or 12-byte name) write at a fixed
// offset inside the bank sector.
//
// The Widget binds to a model.Model: it reads bytes directly via
// m.Bytes() to render the area-list table and detail editor, and
// writes via m.Apply / m.SetBankName for every commit. It subscribes
// to model notifications so external mutations (the voice list
// renaming a voice, say) refresh this widget's tables in place.
package banktab

import (
	"encoding/binary"
	"fmt"
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/studio/helpers"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

// Widget is the bank tab. Construct via New. The zero value is not usable.
type Widget struct {
	m       *model.Model
	bankIdx int

	flex      *tview.Flex
	nameField *tview.InputField
	areaTable *tview.Table
	detail    *tview.Flex

	// Detail-editor widgets, rebuilt each refresh so they always reflect
	// the currently-selected area row. Tracked here so tests can inspect
	// their state without reaching into the Flex.
	keyLow  *tview.InputField
	keyOrig *tview.InputField
	keyHigh *tview.InputField
	velLow  *tview.InputField
	velHigh *tview.InputField
	volume  *tview.InputField
	channel *tview.InputField
	output  *tview.DropDown

	// selectedArea is the area index currently shown in the detail editor.
	// Kept in sync with the table's selection (the table is the source of
	// truth at runtime; this field caches the value for SelectedArea so
	// callers don't have to translate (row, col) coordinates).
	selectedArea int

	onArea func(area, voiceSlot int)
	unsub  func()

	// refreshing guards against recursive refreshes when the refresh
	// path itself calls .Select on the table (which fires
	// SetSelectionChangedFunc). Without the guard we'd re-enter refresh
	// from inside refresh.
	refreshing bool

	// tApp is the tview.Application injected via SetApp so commit
	// handlers can advance focus on Tab.
	tApp *tview.Application

	// onShiftTab is invoked when the user presses Shift+Tab from any
	// field in this widget. The app shell wires it to shiftTabCycle so
	// Shift+Tab from the upper pane (which is what this widget lives
	// in) hops into the lower pane. nil before SetOnShiftTab is called.
	onShiftTab func()
}

// SetApp injects the tview.Application so the widget can advance focus
// on Tab. Call once after construction, before user interaction.
func (w *Widget) SetApp(tApp *tview.Application) { w.tApp = tApp }

// SetOnShiftTab registers the upper->lower pane handoff. Bank tab lives
// in the upper pane, so Shift+Tab from any of its fields should exit
// to the lower pane's section 0. The app shell wires this to
// shiftTabCycle.
func (w *Widget) SetOnShiftTab(fn func()) { w.onShiftTab = fn }

// outputOptions are the labels presented in the area-detail Output
// DropDown. They match disk.FormatAudioOut's vocabulary plus "poly" as
// an alias for "all" so the spec's preferred user-facing term works.
// ParseOutput accepts both.
var outputOptions = []string{"poly", "1", "2", "3", "4", "5", "6", "7", "8", "all"}

// New constructs a Widget bound to bank bankIdx of m. The widget
// subscribes to model notifications and renders the initial state; the
// caller wires Primitive() into a Pages/Flex tree and invokes Close()
// when the tab is destroyed.
func New(m *model.Model, bankIdx int) *Widget {
	w := &Widget{m: m, bankIdx: bankIdx}

	w.nameField = tview.NewInputField()
	w.nameField.SetLabel("Bank Name ")
	w.nameField.SetFieldWidth(disk.LabelSize + 2)
	w.nameField.SetAcceptanceFunc(helpers.AcceptName(disk.LabelSize))
	w.nameField.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter && key != tcell.KeyTab && key != tcell.KeyBacktab {
			// Esc reverts: re-read the model value into the field.
			w.nameField.SetText(w.m.BankName(w.bankIdx))
			return
		}
		text := w.nameField.GetText()
		_ = w.m.SetBankName(w.bankIdx, text)
		// Re-read after commit so any disk normalisation (pad/upcase) is
		// reflected in the field.
		w.nameField.SetText(w.m.BankName(w.bankIdx))
		w.handleDoneKey(key)
	})

	w.areaTable = tview.NewTable()
	// Box-method-chain trap (spec §9.7): set border on a separate
	// statement so we don't lose the *Table type.
	w.areaTable.SetBorders(false)
	w.areaTable.SetSelectable(true, false)
	w.areaTable.SetFixed(1, 0)
	w.areaTable.SetBorder(true)
	w.areaTable.SetTitle(" Areas ")
	w.areaTable.SetSelectionChangedFunc(func(row, _ int) {
		if w.refreshing {
			return
		}
		area := row - 1
		if area < 0 {
			area = 0
		}
		w.selectedArea = area
		w.rebindDetail()
		if w.onArea != nil {
			w.onArea(area, w.voiceSlotForArea(area))
		}
	})

	w.detail = tview.NewFlex()
	w.detail.SetDirection(tview.FlexRow)
	w.detail.SetBorder(true)
	w.detail.SetTitle(" Area Detail ")

	w.flex = tview.NewFlex()
	w.flex.SetDirection(tview.FlexRow)
	w.flex.AddItem(w.nameField, 1, 0, false)
	w.flex.AddItem(w.areaTable, 0, 1, true)
	w.flex.AddItem(w.detail, 0, 1, false)
	w.flex.SetBorder(true)
	w.flex.SetTitle(fmt.Sprintf(" Bank %d ", bankIdx+1))

	w.refresh()
	w.unsub = m.Subscribe(w.refresh)
	w.installFieldCycling()
	return w
}

// Primitive returns the root tview primitive for embedding in a parent
// layout. Stable across the widget's lifetime.
func (w *Widget) Primitive() tview.Primitive { return w.flex }

// Focus moves keyboard focus into the area-list table, the primary
// navigation surface for this tab. tApp.SetFocus(Primitive()) lands on
// the wrapping Flex which has no visible focus state.
// Focus lands on the bank-name field, the top-most field in reading
// order, matching the user's mental model of Tab walking down the
// panel. From there: Tab -> Areas table -> key/vel/vol/ch/output ->
// back to the name field.
func (w *Widget) Focus(tApp *tview.Application) {
	if w.nameField != nil {
		tApp.SetFocus(w.nameField)
		return
	}
	if w.areaTable != nil {
		tApp.SetFocus(w.areaTable)
		return
	}
	tApp.SetFocus(w.flex)
}

// InputFields returns every InputField in the bank tab, in fieldList
// order. Used by the app shell's focused-field finder so a Ctrl+S
// flush after a mouse click can still locate the InputField whose
// embedded TextArea has focus.
func (w *Widget) InputFields() []*tview.InputField {
	var out []*tview.InputField
	for _, p := range w.fieldList() {
		if in, ok := p.(*tview.InputField); ok {
			out = append(out, in)
		}
	}
	return out
}

// fieldList returns every focusable primitive in the bank tab in
// reading order: bank-name input, area table, then the area-detail
// editor fields. Detail-editor widgets are rebuilt on every
// rebindDetail; fieldList reflects the current values each call.
func (w *Widget) fieldList() []tview.Primitive {
	var out []tview.Primitive
	add := func(p tview.Primitive) {
		if p == nil {
			return
		}
		out = append(out, p)
	}
	add(w.nameField)
	add(w.areaTable)
	add(w.keyLow)
	add(w.keyOrig)
	add(w.keyHigh)
	add(w.velLow)
	add(w.velHigh)
	add(w.volume)
	add(w.channel)
	add(w.output)
	return out
}

// focusNextField advances focus to the next primitive in fieldList,
// wrapping at the end. No-op until SetApp is called.
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

// handleDoneKey is the post-commit hook every InputField + DropDown
// commit handler calls. Tab advances field-by-field; Shift+Tab is the
// upper->lower pane handoff (the app's shiftTabCycle).
func (w *Widget) handleDoneKey(key tcell.Key) {
	switch key { //nolint:exhaustive // only Tab/Backtab handled here
	case tcell.KeyTab:
		w.focusNextField()
	case tcell.KeyBacktab:
		if w.onShiftTab != nil {
			w.onShiftTab()
		}
	}
}

// installFieldCycling wires Tab + Shift+Tab on the non-InputField
// focusables (the area table). InputFields handle Tab/Backtab through
// their own DoneFunc, which calls handleDoneKey directly. The Output
// DropDown's commit-on-selection path goes through DropDown.SetDoneFunc
// (set in detail.go) so it handles Tab the same way as InputFields.
func (w *Widget) installFieldCycling() {
	if w.areaTable == nil {
		return
	}
	w.areaTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() { //nolint:exhaustive // only Tab/Backtab handled
		case tcell.KeyTab:
			w.focusNextField()
			return nil
		case tcell.KeyBacktab:
			if w.onShiftTab != nil {
				w.onShiftTab()
			}
			return nil
		}
		return event
	})
}

// Close releases the model subscription. Safe to call multiple times.
func (w *Widget) Close() {
	if w.unsub != nil {
		w.unsub()
		w.unsub = nil
	}
}

// BankIdx returns the bank index this widget was constructed for.
func (w *Widget) BankIdx() int { return w.bankIdx }

// SelectedArea returns the area index (0-based) currently shown in the
// detail editor and highlighted in the area-list table.
func (w *Widget) SelectedArea() int { return w.selectedArea }

// SetSelectedArea programmatically moves the area-list table's
// selection to area. Clamped to the valid range. Fires the
// OnAreaChanged callback if one is registered and the selection
// changes.
func (w *Widget) SetSelectedArea(area int) {
	n := w.areaCount()
	if n == 0 {
		w.selectedArea = 0
		w.rebindDetail()
		return
	}
	if area < 0 {
		area = 0
	}
	if area >= n {
		area = n - 1
	}
	// Drive the table; its SetSelectionChangedFunc handler will update
	// selectedArea, rebind detail, and fire onArea. (Don't call Select
	// while refreshing; the guard inside the handler short-circuits the
	// rebind, which would leave detail stale.)
	w.areaTable.Select(area+1, 0)
}

// SetOnAreaChanged registers a callback fired when the selected area
// row changes (either via user navigation or SetSelectedArea). The
// slot int is the voice slot that area's vp[] entry references; the
// app shell uses this to rebind the lower-section detail panels.
//
// Passing nil clears the callback.
func (w *Widget) SetOnAreaChanged(fn func(area, voiceSlot int)) {
	w.onArea = fn
}

// areaCount returns the number of areas in this bank: the bstep field
// at BankVoiceCountOffset, clamped to MaxVoices and to what the byte
// slice can actually contain.
func (w *Widget) areaCount() int {
	bank := fzutil.BankSliceAt(w.m.Bytes(), w.bankIdx)
	if bank == nil {
		return 0
	}
	n := int(binary.LittleEndian.Uint16(bank[disk.BankVoiceCountOffset : disk.BankVoiceCountOffset+2]))
	if n < 0 {
		return 0
	}
	if n > disk.MaxVoices {
		n = disk.MaxVoices
	}
	// Cap at the number of vp[] entries the sector can actually hold.
	// vp[] is 2 bytes per area starting at BankVoiceNumOffset; the
	// sector is SectorSize bytes total.
	maxAreas := (disk.SectorSize - disk.BankVoiceNumOffset) / 2
	if n > maxAreas {
		n = maxAreas
	}
	return n
}

// voiceSlotForArea reads the low byte of vp[area] from the bank
// sector. The spec stores vp[] as uint16 little-endian, but only the
// low byte holds the voice slot index (slot count is 0..63).
func (w *Widget) voiceSlotForArea(area int) int {
	bank := fzutil.BankSliceAt(w.m.Bytes(), w.bankIdx)
	if bank == nil {
		return 0
	}
	off := disk.BankVoiceNumOffset + 2*area
	if off+2 > len(bank) {
		return 0
	}
	return int(bank[off])
}

// refresh re-reads model state and rebuilds every sub-widget's display.
// Called once at construction and on every model notification.
func (w *Widget) refresh() {
	if w.refreshing {
		return
	}
	w.refreshing = true
	defer func() { w.refreshing = false }()

	// Capture which detail-editor field has focus (if any) before
	// rebindDetail rebuilds the InputFields. Without this, committing
	// a detail-editor value via Tab (which triggers Subscribe ->
	// refresh -> rebindDetail) detaches the focused primitive, so
	// the post-commit Tab advance can't find it in fieldList and
	// falls back to the bank-name field. Restore focus to the
	// equivalent new field after the rebuild so handleDoneKey's
	// Tab advance finds the right position.
	refocusIdx := -1
	if w.tApp != nil {
		focused := w.tApp.GetFocus()
		for i, p := range w.detailFields() {
			if p != nil && p == focused {
				refocusIdx = i
				break
			}
		}
	}

	// Name field: only update if the user isn't actively editing it
	// (i.e. the field's text already matches the model). We can't
	// reliably detect "user is editing" without focus tracking, so we
	// always re-sync. Apply has already landed by the time Subscribe
	// fires, and the user-typed draft buffer was committed in the
	// DoneFunc before m.Apply was called. The only loss is if a
	// concurrent external mutation lands while the user is mid-type;
	// that's an acceptable trade-off.
	w.nameField.SetText(w.m.BankName(w.bankIdx))

	w.rebuildTable()
	w.rebindDetail()

	if refocusIdx >= 0 && w.tApp != nil {
		fields := w.detailFields()
		if refocusIdx < len(fields) && fields[refocusIdx] != nil {
			w.tApp.SetFocus(fields[refocusIdx])
		}
	}
}

// detailFields returns the area-detail editor's focusables in the
// stable positional order rebindDetail builds them. Used by refresh()
// to preserve focus across rebuilds.
func (w *Widget) detailFields() []tview.Primitive {
	return []tview.Primitive{
		w.keyLow, w.keyOrig, w.keyHigh,
		w.velLow, w.velHigh,
		w.volume, w.channel, w.output,
	}
}

// rebuildTable clears and repopulates the area-list table from the
// current bank-sector bytes. Preserves the user's selection if it's
// still in range.
func (w *Widget) rebuildTable() {
	w.areaTable.Clear()

	headers := []string{"#", "Slot", "Name", "Key Range", "Vel Range", "Vol", "Ch", "Out"}
	for col, h := range headers {
		cell := tview.NewTableCell(h)
		cell.SetSelectable(false)
		cell.SetTextColor(tview.Styles.SecondaryTextColor)
		w.areaTable.SetCell(0, col, cell)
	}

	n := w.areaCount()
	bank := fzutil.BankSliceAt(w.m.Bytes(), w.bankIdx)
	if bank == nil || n == 0 {
		// Even with no areas we want at least the header row present so
		// the layout doesn't collapse.
		return
	}

	for i := 0; i < n; i++ {
		slot := w.voiceSlotForArea(i)
		name := "(unnamed)"
		if v, err := w.m.Voice(slot); err == nil && v != nil && v.Name != "" {
			name = v.Name
		}
		keyRange := fmt.Sprintf("%s - %s",
			helpers.FormatNote(bank[disk.BankKeyLowOffset+i]),
			helpers.FormatNote(bank[disk.BankKeyHighOffset+i]),
		)
		velRange := fmt.Sprintf("%d - %d",
			bank[disk.BankVelLowOffset+i],
			bank[disk.BankVelHighOffset+i],
		)
		vol := strconv.Itoa(int(bank[disk.BankVolumeOffset+i]))
		ch := strconv.Itoa(int(bank[disk.BankMIDIRecvChanOffset+i]) + 1)
		out := helpers.FormatOutput(bank[disk.BankAudioOutOffset+i])

		row := i + 1
		w.areaTable.SetCell(row, 0, tview.NewTableCell(strconv.Itoa(i+1)))
		w.areaTable.SetCell(row, 1, tview.NewTableCell(strconv.Itoa(slot+1)))
		w.areaTable.SetCell(row, 2, tview.NewTableCell(name))
		w.areaTable.SetCell(row, 3, tview.NewTableCell(keyRange))
		w.areaTable.SetCell(row, 4, tview.NewTableCell(velRange))
		w.areaTable.SetCell(row, 5, tview.NewTableCell(vol))
		w.areaTable.SetCell(row, 6, tview.NewTableCell(ch))
		w.areaTable.SetCell(row, 7, tview.NewTableCell(out))
	}

	// Restore (or clamp) the selection. The Selectable property keeps
	// row 0 (header) unselectable; we always target row >= 1.
	if w.selectedArea >= n {
		w.selectedArea = n - 1
	}
	if w.selectedArea < 0 {
		w.selectedArea = 0
	}
	w.areaTable.Select(w.selectedArea+1, 0)
}
