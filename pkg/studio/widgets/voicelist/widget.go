// Package voicelist implements the read-only voice list widget used by the
// upper section's "Voices" tab in fizzle studio (spec §2.2.1).
//
// The widget owns a tview.Table populated from the in-memory Model. One row
// per voice slot, columns matching spec §2.2.1: #, Name, kHz, Low, Orig,
// High, Tune, Samples, Duration. The table is read-only: editing happens
// through the Voice Details panel (pkg/studio/widgets/voicedetails).
//
// The widget subscribes to model change notifications so a name patch (or
// any other edit) re-renders the table without explicit refresh from the
// caller. Audition is wired by the app shell via its own SetInputCapture;
// this package leaves the Space key unbound on the table.
package voicelist

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/philipcunningham/fizzle/pkg/studio/helpers"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

// headerRows is the number of header rows in the table. Row 0 holds column
// titles, so slot N lives at row N+1.
const headerRows = 1

// Widget is a read-only voice list backed by a Model. Construct via New.
type Widget struct {
	table *tview.Table
	m     *model.Model
	unsub func()
	onSel func(slot int)
}

// New builds a Widget bound to m. The table is populated immediately from
// m.Voice(...) and a Subscribe callback is registered so the table refreshes
// on any model mutation. The caller must call Close to release the
// subscription when the widget is no longer in use.
func New(m *model.Model) *Widget {
	w := &Widget{m: m}
	t := tview.NewTable().SetSelectable(true, false).SetFixed(headerRows, 0)
	// Box-method-chain trap (spec §9.7): SetBorder returns *Box, not *Table.
	// Apply borders on a separate statement so subsequent *Table calls work.
	t.SetBorder(true).SetTitle(" Voices ")
	w.table = t

	// Translate row -> slot for the consumer-facing selection-changed hook.
	t.SetSelectionChangedFunc(func(row, _ int) {
		if w.onSel == nil {
			return
		}
		slot := row - headerRows
		if slot < 0 || slot >= w.m.Header().NVoice {
			return
		}
		w.onSel(slot)
	})

	w.refresh()
	w.unsub = m.Subscribe(w.refresh)
	// Default to the first voice row so consumers always have a valid
	// SelectedSlot() before the user interacts with the table.
	if m.Header().NVoice > 0 {
		t.Select(headerRows, 0)
	}
	return w
}

// Primitive exposes the underlying tview primitive so the app shell can
// embed the widget in a Flex / Pages layout.
func (w *Widget) Primitive() tview.Primitive { return w.table }

// Focus moves keyboard focus to the voice table. Primitive() is itself
// the focusable table so SetFocus(Primitive()) also works; this method
// exists to give the app shell a uniform way to focus any widget.
func (w *Widget) Focus(tApp *tview.Application) {
	tApp.SetFocus(w.table)
}

// Close unsubscribes from the model. Safe to call multiple times.
func (w *Widget) Close() {
	if w.unsub != nil {
		w.unsub()
		w.unsub = nil
	}
}

// SetOnSelectionChanged registers fn to be invoked whenever the user moves
// the selection to a different voice row. The slot is 0-indexed.
func (w *Widget) SetOnSelectionChanged(fn func(slot int)) {
	w.onSel = fn
}

// SelectedSlot returns the 0-indexed slot currently highlighted in the
// table. Returns 0 when there are no voices (defensive; callers should
// check Header().NVoice first).
func (w *Widget) SelectedSlot() int {
	row, _ := w.table.GetSelection()
	slot := row - headerRows
	if slot < 0 {
		return 0
	}
	if n := w.m.Header().NVoice; slot >= n {
		if n > 0 {
			return n - 1
		}
		return 0
	}
	return slot
}

// SetSelectedSlot programmatically moves the selection to the given voice
// slot. Out-of-range slots are clamped to the valid range; this is used by
// the app shell to restore selection across tab switches.
func (w *Widget) SetSelectedSlot(slot int) {
	n := w.m.Header().NVoice
	if n <= 0 {
		return
	}
	if slot < 0 {
		slot = 0
	}
	if slot >= n {
		slot = n - 1
	}
	w.table.Select(slot+headerRows, 0)
}

// refresh repopulates the table from the model. Called from New and from the
// Subscribe callback whenever the model mutates. Cleared first so a shrunk
// voice list doesn't leave stale rows (defensive: structural ops aren't
// supported today, but the model API doesn't forbid them).
func (w *Widget) refresh() {
	w.table.Clear()
	setHeaderCells(w.table)
	hdr := w.m.Header()
	for slot := 0; slot < hdr.NVoice; slot++ {
		row := slot + headerRows
		w.setSlotRow(row, slot)
	}
}

// setHeaderCells writes the column titles into row 0. Cells are not
// selectable so up-arrow from the first data row stops at row 1 rather than
// landing on the header.
func setHeaderCells(t *tview.Table) {
	titles := []string{"#", "Name", "kHz", "Low", "Orig", "High", "Tune", "Samples", "Duration"}
	for col, title := range titles {
		c := tview.NewTableCell(title).
			SetTextColor(tview.Styles.SecondaryTextColor).
			SetSelectable(false).
			SetAlign(tview.AlignLeft)
		t.SetCell(0, col, c)
	}
}

// setSlotRow renders one voice slot into the table. On a Voice() parse error
// the row is filled with placeholders so the user sees the slot index and a
// hint rather than a missing row.
func (w *Widget) setSlotRow(row, slot int) {
	v, err := w.m.Voice(slot)
	idx := fmt.Sprintf("%d", slot+1)
	if err != nil || v == nil {
		w.table.SetCell(row, 0, tview.NewTableCell(idx))
		w.table.SetCell(row, 1, tview.NewTableCell("(parse error)").SetTextColor(tcell.ColorRed))
		for col := 2; col < 9; col++ {
			w.table.SetCell(row, col, tview.NewTableCell("-"))
		}
		return
	}
	cells := []string{
		idx,
		v.Name,
		formatKHz(v.SampleRate),
		helpers.FormatNote(v.KeyLow),
		helpers.FormatNote(v.KeyCentre),
		helpers.FormatNote(v.KeyHigh),
		formatTune(v.Transpose),
		fmt.Sprintf("%d", v.Samples),
		formatDuration(v.Duration),
	}
	for col, text := range cells {
		w.table.SetCell(row, col, tview.NewTableCell(text))
	}
}

// formatKHz renders a sample rate (Hz) as a kHz integer suitable for the
// kHz column. The spec lists 36/18/9 kHz as the FZ's three rates.
func formatKHz(hz uint32) string {
	if hz == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", hz/1000)
}

// formatTune renders a transpose value in semitones with a leading sign so
// `+0` reads clearly distinct from `0` (unset). VoiceParams.Transpose is
// already in semitones.
func formatTune(semis int16) string {
	return fmt.Sprintf("%+d", semis)
}

// formatDuration renders seconds with two decimals.
func formatDuration(secs float64) string {
	return fmt.Sprintf("%.2fs", secs)
}
