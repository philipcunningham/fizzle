// Package loopdetails implements the Loop Details panel for fizzle studio.
//
// The panel follows-focus on a single voice slot. The host shell calls
// Bind(slot) whenever the upper section's voice changes. The widget exposes
// two top-row DropDowns (sustain/release loop), two read-only address
// labels (wave/gen), a read-only 8-row stage table, and a per-stage editor
// Flex with InputFields and a DropDown.
//
// Layout matches spec §2.3.2. Edits commit on Tab/Enter (InputField
// DoneFunc) and immediately on DropDown selection. All commits go through
// model.ApplyVoicePatch; offsets are voice-header-relative and the model
// translates to FZF-absolute.
package loopdetails

import (
	"encoding/binary"
	"fmt"
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/helpers"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
)

// stageCount is the number of loop stages in a voice header. Spec §2-1.
const stageCount = 8

// maxLoopStart is the 24-bit address mask for loopst[i] (low 24 bits).
const maxLoopStart = 0x00FFFFFF

// maxLoopEnd is the 31-bit address mask for looped[i] (low 31 bits).
const maxLoopEnd = 0x7FFFFFFF

// maxLoopXF / maxLoopTm are the documented hardware ranges for loopxf/looptm.
const (
	maxLoopXF = 1023
	minLoopTm = 1
	maxLoopTm = 1022
)

// Widget is the Loop Details panel.
type Widget struct {
	flex *tview.Flex

	// currentSection tracks which "section" of the panel focus is in,
	// used by CycleSection (Shift+Tab) to advance forward with wrap.
	// 0 = top selectors, 1 = stage table, 2 = per-stage editor.
	currentSection int
	m              *model.Model
	slot           int

	// Top-row dropdowns.
	susDD *tview.DropDown
	endDD *tview.DropDown

	// Read-only labels.
	waveLabel *tview.TextView
	genLabel  *tview.TextView

	// Stage table.
	table *tview.Table

	// Per-stage editor inputs (bound to selStage).
	startIF *tview.InputField
	endIF   *tview.InputField
	xfIF    *tview.InputField
	tmIF    *tview.InputField
	nextDD  *tview.DropDown
	fineIF  *tview.InputField

	// selStage is the stage row currently bound to the editor (0..7).
	selStage int

	// refreshing suppresses commit handlers while we repaint widgets from
	// the model. Without this, programmatic SetText / SetCurrentOption
	// calls fire their handlers and patch the model with stale values.
	refreshing bool

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

// New constructs the panel bound to voice slot 0. Call Bind to rebind.
func New(m *model.Model) *Widget {
	w := &Widget{m: m, slot: 0, selStage: 0}
	w.build()
	w.unsub = m.Subscribe(w.refresh)
	w.refresh()
	return w
}

// Primitive returns the panel's root primitive for embedding in the shell.
func (w *Widget) Primitive() tview.Primitive { return w.flex }

// Focus moves keyboard focus into the panel's first section (the
// sustain/release loop selectors). The root Flex already marks the
// stage table as its focus child, but uniform widget-API: callers
// prefer this method.
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
	if w.susDD != nil {
		out = append(out, w.susDD)
	}
	if w.table != nil {
		out = append(out, w.table)
	}
	if w.startIF != nil {
		out = append(out, w.startIF)
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

// InputFields returns every InputField in the loop-details panel.
// Used by the app shell's focused-field finder so a Ctrl+S flush after
// a mouse click can still locate the InputField whose embedded
// TextArea has focus.
func (w *Widget) InputFields() []*tview.InputField {
	out := []*tview.InputField{}
	for _, in := range []*tview.InputField{w.startIF, w.endIF, w.xfIF, w.tmIF, w.fineIF} {
		if in != nil {
			out = append(out, in)
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

// Bind switches the panel to display voice slot. Out-of-range slot values
// are clamped; the model's Voice() will report an error elsewhere.
func (w *Widget) Bind(slot int) {
	w.slot = slot
	w.selStage = 0
	w.refresh()
}

// build assembles the tview primitives. Run once from New.
func (w *Widget) build() {
	// Sustain DropDown: options 0..7 + "none" (byte value 8).
	w.susDD = tview.NewDropDown().SetLabel("Sustain loop (0-7 or none): ")
	w.susDD.SetOptions(loopOptions("none"), func(_ string, idx int) {
		if w.refreshing {
			return
		}
		w.commitSusLoop(byte(idx)) //nolint:gosec // idx is 0..8 from options array
	})

	// Release DropDown: options 0..7 + "all" (byte value 8).
	w.endDD = tview.NewDropDown().SetLabel("Release loop (0-7 or all): ")
	w.endDD.SetOptions(loopOptions("all"), func(_ string, idx int) {
		if w.refreshing {
			return
		}
		w.commitEndLoop(byte(idx)) //nolint:gosec // idx is 0..8 from options array
	})

	topRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(w.susDD, 0, 1, false).
		AddItem(w.endDD, 0, 1, false)

	// Read-only address labels.
	w.waveLabel = tview.NewTextView().SetDynamicColors(true)
	w.genLabel = tview.NewTextView().SetDynamicColors(true)
	addrRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(w.waveLabel, 0, 1, false).
		AddItem(w.genLabel, 0, 1, false)

	// Stage table.
	w.table = tview.NewTable().
		SetSelectable(true, false).
		SetFixed(1, 0)
	w.table.SetBorder(true)
	w.table.SetTitle(" Loop Stages ")
	w.table.SetSelectionChangedFunc(func(row, _ int) {
		// Row 0 is the header; clamp to 1..stageCount.
		if row < 1 {
			row = 1
		}
		if row > stageCount {
			row = stageCount
		}
		w.selStage = row - 1
		w.refreshStageEditor()
	})

	// Per-stage editor.
	w.startIF = w.newUnsignedInput("Start (0..16777215): ", maxLoopStart, func(v int) {
		w.commitStart(uint32(v)) //nolint:gosec // accept-fn bounds 0..maxLoopStart
	})
	w.endIF = w.newUnsignedInput("End (0..2147483647): ", maxLoopEnd, func(v int) {
		w.commitEnd(uint32(v)) //nolint:gosec // accept-fn bounds 0..maxLoopEnd
	})
	w.xfIF = w.newUnsignedInput("XFade (0-1023): ", maxLoopXF, func(v int) {
		w.commitXFade(uint16(v)) //nolint:gosec // accept-fn bounds 0..1023
	})
	w.tmIF = w.newSignedInput("Time (1-1022): ", minLoopTm, maxLoopTm, func(v int) {
		w.commitTime(uint16(v)) //nolint:gosec // accept-fn bounds 1..1022
	})
	w.nextDD = tview.NewDropDown().SetLabel("Next: ")
	w.nextDD.SetOptions([]string{"Trace", "Skip"}, func(_ string, idx int) {
		if w.refreshing {
			return
		}
		w.commitNext(idx == 1)
	})
	w.fineIF = w.newUnsignedInput("Loop Fine (0-255): ", 255, func(v int) {
		w.commitFine(byte(v)) //nolint:gosec // accept-fn bounds 0..255
	})

	editor := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(w.startIF, 1, 0, false).
		AddItem(w.endIF, 1, 0, false).
		AddItem(w.xfIF, 1, 0, false).
		AddItem(w.tmIF, 1, 0, false).
		AddItem(w.nextDD, 1, 0, false).
		AddItem(w.fineIF, 1, 0, false)
	editor.SetBorder(true)
	editor.SetTitle(" Edit Stage ")

	w.flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(topRow, 1, 0, false).
		AddItem(addrRow, 1, 0, false).
		AddItem(w.table, 0, 2, true).
		AddItem(editor, 0, 3, false)
	w.flex.SetBorder(true)
	w.flex.SetTitle(" Loop Details ")
	w.installFieldCycling()
}

// loopOptions returns ["0", "1", ..., "7", sentinel].
func loopOptions(sentinel string) []string {
	out := make([]string, 0, 9)
	for i := 0; i < stageCount; i++ {
		out = append(out, strconv.Itoa(i))
	}
	return append(out, sentinel)
}

// isCommitKey is true for the keys that should trigger an InputField
// commit (Tab forward/back, Enter). Anything else (Escape, etc.) bows out.
func isCommitKey(k tcell.Key) bool {
	return k == tcell.KeyEnter || k == tcell.KeyTab || k == tcell.KeyBacktab
}

// fieldList returns every focusable primitive in the panel, ordered
// top-to-bottom for Tab navigation.
func (w *Widget) fieldList() []tview.Primitive {
	var out []tview.Primitive
	add := func(p tview.Primitive) {
		if p == nil {
			return
		}
		out = append(out, p)
	}
	add(w.susDD)
	add(w.endDD)
	add(w.table)
	add(w.startIF)
	add(w.endIF)
	add(w.xfIF)
	add(w.tmIF)
	add(w.nextDD)
	add(w.fineIF)
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
// Called from every InputField's DoneFunc and from SetInputCapture
// handlers on non-InputField focusables.
func (w *Widget) handleDoneKey(key tcell.Key) {
	switch key { //nolint:exhaustive // only Tab/Backtab here
	case tcell.KeyTab:
		w.focusNextField()
	case tcell.KeyBacktab:
		w.CycleSection(w.tApp)
	}
}

// installFieldCycling wires Tab and Shift+Tab on every non-InputField
// focusable. InputFields handle Tab/Backtab via SetDoneFunc directly.
func (w *Widget) installFieldCycling() {
	capture := func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() { //nolint:exhaustive // only Tab/Backtab handled
		case tcell.KeyTab:
			w.focusNextField()
			return nil
		case tcell.KeyBacktab:
			w.CycleSection(w.tApp)
			return nil
		}
		return event
	}
	if w.susDD != nil {
		w.susDD.SetInputCapture(capture)
	}
	if w.endDD != nil {
		w.endDD.SetInputCapture(capture)
	}
	if w.table != nil {
		w.table.SetInputCapture(capture)
	}
	if w.nextDD != nil {
		w.nextDD.SetInputCapture(capture)
	}
}

// newUnsignedInput builds an InputField wired with AcceptUnsigned(hi) and a
// commit callback fired on Enter/Tab/BackTab when the text parses cleanly.
func (w *Widget) newUnsignedInput(label string, hi int, commit func(int)) *tview.InputField {
	in := tview.NewInputField().SetLabel(label)
	in.SetAcceptanceFunc(helpers.AcceptUnsigned(hi))
	in.SetDoneFunc(func(key tcell.Key) {
		if w.refreshing || !isCommitKey(key) {
			return
		}
		s := in.GetText()
		if s != "" {
			if v, err := strconv.Atoi(s); err == nil && v >= 0 && v <= hi {
				commit(v)
			}
		}
		w.handleDoneKey(key)
	})
	return in
}

// newSignedInput is the AcceptSigned(lo, hi) twin of newUnsignedInput.
func (w *Widget) newSignedInput(label string, lo, hi int, commit func(int)) *tview.InputField {
	in := tview.NewInputField().SetLabel(label)
	in.SetAcceptanceFunc(helpers.AcceptSigned(lo, hi))
	in.SetDoneFunc(func(key tcell.Key) {
		if w.refreshing || !isCommitKey(key) {
			return
		}
		s := in.GetText()
		if s != "" && s != "-" && s != "+" {
			if v, err := strconv.Atoi(s); err == nil && v >= lo && v <= hi {
				commit(v)
			}
		}
		w.handleDoneKey(key)
	})
	return in
}

// voiceOffset returns the FZF-absolute offset of the bound voice's header.
func (w *Widget) voiceOffset() (int, bool) {
	hdr := w.m.Header()
	if hdr == nil {
		return 0, false
	}
	if w.slot < 0 || w.slot >= hdr.NVoice {
		return 0, false
	}
	off := disk.VoiceSlotOffset(hdr.VoiceAreaStart, w.slot)
	if off < 0 || off+disk.VoiceHeaderUsed > len(w.m.Bytes()) {
		return 0, false
	}
	return off, true
}

// readVoiceByte returns the byte at the given voice-header-relative offset.
func (w *Widget) readVoiceByte(rel int) (byte, bool) {
	off, ok := w.voiceOffset()
	if !ok {
		return 0, false
	}
	return w.m.Bytes()[off+rel], true
}

// readVoiceUint32 reads 4 little-endian bytes at the given voice-header-relative offset.
func (w *Widget) readVoiceUint32(rel int) (uint32, bool) {
	off, ok := w.voiceOffset()
	if !ok {
		return 0, false
	}
	return binary.LittleEndian.Uint32(w.m.Bytes()[off+rel:]), true
}

// readVoiceUint16 reads 2 little-endian bytes at the given
// voice-header-relative offset, or 0 if the slot offset is out of range.
func (w *Widget) readVoiceUint16(rel int) uint16 {
	off, ok := w.voiceOffset()
	if !ok {
		return 0
	}
	return binary.LittleEndian.Uint16(w.m.Bytes()[off+rel:])
}

// stageStOffset returns the voice-header-relative offset of loopst[i].
func stageStOffset(i int) int { return disk.VoiceLoopSt0Offset + i*4 }

// stageEdOffset returns the voice-header-relative offset of looped[i].
func stageEdOffset(i int) int { return disk.VoiceLoopEd0Offset + i*4 }

// stageXfOffset returns the voice-header-relative offset of loopxf[i].
func stageXfOffset(i int) int { return disk.LoopXfStart + i*disk.LoopXFEntrySize }

// stageTmOffset returns the voice-header-relative offset of looptm[i].
func stageTmOffset(i int) int { return disk.LoopTmStart + i*disk.LoopTmEntrySize }

// refresh rebuilds the widget contents from the model. Triggered by the
// Subscribe callback (after every Apply / Undo / Redo / Save) and by Bind.
func (w *Widget) refresh() {
	w.refreshing = true
	defer func() { w.refreshing = false }()

	// Top-row dropdowns.
	if b, ok := w.readVoiceByte(disk.VoiceLoopSusOffset); ok {
		w.susDD.SetCurrentOption(loopOptionIndex(b))
	}
	if b, ok := w.readVoiceByte(disk.VoiceLoopEndOffset); ok {
		w.endDD.SetCurrentOption(loopOptionIndex(b))
	}

	// Address labels.
	if wavst, ok := w.readVoiceUint32(disk.VoiceWaveStartOffset); ok {
		if waved, ok := w.readVoiceUint32(disk.VoiceWaveEndOffset); ok {
			w.waveLabel.SetText(fmt.Sprintf("Wave: %d-%d", wavst, waved))
		}
	}
	if genst, ok := w.readVoiceUint32(disk.VoiceGenStartOffset); ok {
		if gened, ok := w.readVoiceUint32(disk.VoiceGenEndOffset); ok {
			w.genLabel.SetText(fmt.Sprintf("Gen: %d-%d", genst, gened))
		}
	}

	// Stage table.
	w.populateTable()

	// Per-stage editor.
	w.refreshStageEditor()
}

// loopOptionIndex maps a stored byte to an option index. Bytes outside
// 0..stageCount map to the "none"/"all" sentinel (index stageCount).
func loopOptionIndex(b byte) int {
	if int(b) >= stageCount {
		return stageCount
	}
	return int(b)
}

// populateTable rewrites the read-only stage table from the voice header.
func (w *Widget) populateTable() {
	t := w.table
	t.Clear()
	headers := []string{"#", "Start", "End", "XFade", "Time", "Next"}
	for col, h := range headers {
		cell := tview.NewTableCell(h).
			SetTextColor(tview.Styles.SecondaryTextColor).
			SetSelectable(false)
		t.SetCell(0, col, cell)
	}
	for i := 0; i < stageCount; i++ {
		st, _ := w.readVoiceUint32(stageStOffset(i))
		ed, _ := w.readVoiceUint32(stageEdOffset(i))
		xf := w.readVoiceUint16(stageXfOffset(i))
		tm := w.readVoiceUint16(stageTmOffset(i))

		startAddr := st & disk.LoopStartAddressMask
		fine := byte(st >> disk.LoopStartFineShift) //nolint:gosec // shift to byte
		startStr := strconv.FormatUint(uint64(startAddr), 10)
		if fine != 0 {
			startStr = fmt.Sprintf("%d.%02d", startAddr, fine)
		}

		endStr := strconv.FormatUint(uint64(disk.LoopEndAddress(ed)), 10)
		nextStr := "Trace"
		if ed&disk.LoopEndSkipMask != 0 {
			nextStr = "Skip"
		}
		row := i + 1
		t.SetCell(row, 0, tview.NewTableCell(strconv.Itoa(i)))
		t.SetCell(row, 1, tview.NewTableCell(startStr))
		t.SetCell(row, 2, tview.NewTableCell(endStr))
		t.SetCell(row, 3, tview.NewTableCell(strconv.Itoa(int(xf))))
		t.SetCell(row, 4, tview.NewTableCell(strconv.Itoa(int(tm))))
		t.SetCell(row, 5, tview.NewTableCell(nextStr))
	}
	// Keep the row selection in sync with selStage.
	t.Select(w.selStage+1, 0)
}

// refreshStageEditor reloads the per-stage InputFields from the model.
func (w *Widget) refreshStageEditor() {
	st, _ := w.readVoiceUint32(stageStOffset(w.selStage))
	ed, _ := w.readVoiceUint32(stageEdOffset(w.selStage))
	xf := w.readVoiceUint16(stageXfOffset(w.selStage))
	tm := w.readVoiceUint16(stageTmOffset(w.selStage))

	prev := w.refreshing
	w.refreshing = true
	defer func() { w.refreshing = prev }()

	w.startIF.SetText(strconv.FormatUint(uint64(st&disk.LoopStartAddressMask), 10))
	w.endIF.SetText(strconv.FormatUint(uint64(disk.LoopEndAddress(ed)), 10))
	w.xfIF.SetText(strconv.Itoa(int(xf)))
	w.tmIF.SetText(strconv.Itoa(int(tm)))
	if ed&disk.LoopEndSkipMask != 0 {
		w.nextDD.SetCurrentOption(1)
	} else {
		w.nextDD.SetCurrentOption(0)
	}
	fine := byte(st >> disk.LoopStartFineShift) //nolint:gosec // shift to byte
	w.fineIF.SetText(strconv.Itoa(int(fine)))
}

// commitSusLoop writes a 1-byte patch at VoiceLoopSusOffset.
func (w *Widget) commitSusLoop(b byte) {
	w.applyByte(disk.VoiceLoopSusOffset, b)
}

// commitEndLoop writes a 1-byte patch at VoiceLoopEndOffset.
func (w *Widget) commitEndLoop(b byte) {
	w.applyByte(disk.VoiceLoopEndOffset, b)
}

// commitStart preserves the loop-fine upper byte and writes a new 24-bit
// start address.
func (w *Widget) commitStart(addr uint32) {
	cur, ok := w.readVoiceUint32(stageStOffset(w.selStage))
	if !ok {
		return
	}
	addr &= disk.LoopStartAddressMask
	combined := (cur &^ disk.LoopStartAddressMask) | addr
	w.applyUint32(stageStOffset(w.selStage), combined)
}

// commitFine preserves the address bits and writes a new fine byte.
func (w *Widget) commitFine(fine byte) {
	cur, ok := w.readVoiceUint32(stageStOffset(w.selStage))
	if !ok {
		return
	}
	combined := (cur & disk.LoopStartAddressMask) | (uint32(fine) << disk.LoopStartFineShift)
	w.applyUint32(stageStOffset(w.selStage), combined)
}

// commitEnd preserves the skip flag and writes a new 31-bit address.
func (w *Widget) commitEnd(addr uint32) {
	cur, ok := w.readVoiceUint32(stageEdOffset(w.selStage))
	if !ok {
		return
	}
	addr &= disk.LoopEndAddressMask
	combined := (cur & disk.LoopEndSkipMask) | addr
	w.applyUint32(stageEdOffset(w.selStage), combined)
}

// commitNext preserves the address bits and writes the skip flag.
func (w *Widget) commitNext(skip bool) {
	cur, ok := w.readVoiceUint32(stageEdOffset(w.selStage))
	if !ok {
		return
	}
	combined := cur &^ disk.LoopEndSkipMask
	if skip {
		combined |= disk.LoopEndSkipMask
	}
	w.applyUint32(stageEdOffset(w.selStage), combined)
}

// commitXFade writes 2 little-endian bytes at loopxf[selStage].
func (w *Widget) commitXFade(v uint16) {
	w.applyUint16(stageXfOffset(w.selStage), v)
}

// commitTime writes 2 little-endian bytes at looptm[selStage].
func (w *Widget) commitTime(v uint16) {
	w.applyUint16(stageTmOffset(w.selStage), v)
}

// applyByte applies a 1-byte patch via ApplyVoicePatch. Errors are
// swallowed; the widget can't surface them today (no status line API).
func (w *Widget) applyByte(rel int, b byte) {
	_ = w.m.ApplyVoicePatch(w.slot, voiceedit.Patch{
		Offset: rel,
		Size:   1,
		Value:  uint16(b),
	})
}

// applyUint16 applies a 2-byte little-endian patch.
func (w *Widget) applyUint16(rel int, v uint16) {
	_ = w.m.ApplyVoicePatch(w.slot, voiceedit.Patch{
		Offset: rel,
		Size:   2,
		Value:  v,
	})
}

// applyUint32 applies a 4-byte little-endian patch. ApplyVoicePatch's
// Bytes-payload path skips the size-2 fast path and writes the slice
// verbatim, which is exactly what we want for the 32-bit loop-pointer
// fields (loopst, looped).
func (w *Widget) applyUint32(rel int, v uint32) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, v)
	_ = w.m.ApplyVoicePatch(w.slot, voiceedit.Patch{
		Offset: rel,
		Bytes:  buf,
	})
}
