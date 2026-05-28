package voicelist

import (
	"strconv"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
)

const testVoiceAlpha = "ALPHA"

func newTestModel(t *testing.T, names []string) *model.Model {
	t.Helper()
	_, path := fzfbuilder.MakeTestFZF(t, names)
	m, err := model.New(path)
	if err != nil {
		t.Fatalf("model.New: %v", err)
	}
	return m
}

// TestNewPopulatesRows verifies the widget's table has a header row plus one
// row per voice slot, and that the # / Name cells match the in-memory model.
func TestNewPopulatesRows(t *testing.T) {
	t.Parallel()
	names := []string{testVoiceAlpha, "BRAVO", "GAMMA"}
	m := newTestModel(t, names)
	w := New(m)
	defer w.Close()

	got := w.table.GetRowCount()
	if got != 1+3 {
		t.Fatalf("RowCount = %d, want 4 (1 header + 3 voices)", got)
	}
	for slot, name := range names {
		row := slot + headerRows
		if c := w.table.GetCell(row, 0); c.Text != itoaSlot(slot+1) {
			t.Errorf("row %d # = %q, want %q", row, c.Text, itoaSlot(slot+1))
		}
		if c := w.table.GetCell(row, 1); c.Text != name {
			t.Errorf("row %d Name = %q, want %q", row, c.Text, name)
		}
	}
}

func itoaSlot(slot int) string { return strconv.Itoa(slot) }

// TestNewSetsHeader verifies the column titles in row 0 match the spec
// (§2.2.1).
func TestNewSetsHeader(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{"X"})
	w := New(m)
	defer w.Close()

	want := []string{"#", "Name", "kHz", "Low", "Orig", "High", "Tune", "Samples", "Duration"}
	for col, title := range want {
		got := w.table.GetCell(0, col).Text
		if got != title {
			t.Errorf("header col %d = %q, want %q", col, got, title)
		}
	}
}

// TestRefreshOnNamePatch verifies that mutating the model via Apply triggers
// the Subscribe callback and the table reflects the new voice name.
func TestRefreshOnNamePatch(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{testVoiceAlpha, "BRAVO"})
	w := New(m)
	defer w.Close()

	if got := w.table.GetCell(1, 1).Text; got != testVoiceAlpha {
		t.Fatalf("pre-edit Name = %q, want %s", got, testVoiceAlpha)
	}

	patches, err := voiceedit.BuildNamePatch("NEWNAME")
	if err != nil {
		t.Fatalf("BuildNamePatch: %v", err)
	}
	for _, p := range patches {
		if err := m.ApplyVoicePatch(0, p); err != nil {
			t.Fatalf("ApplyVoicePatch: %v", err)
		}
	}
	if got := w.table.GetCell(1, 1).Text; got != "NEWNAME" {
		t.Errorf("post-edit Name = %q, want NEWNAME", got)
	}
}

// TestSetSelectedSlotAndSelectedSlotRoundtrip verifies programmatic
// selection works for the app shell's tab-restore path.
func TestSetSelectedSlotAndSelectedSlotRoundtrip(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{"A", "B", "C", "D"})
	w := New(m)
	defer w.Close()

	w.SetSelectedSlot(2)
	if got := w.SelectedSlot(); got != 2 {
		t.Errorf("SelectedSlot = %d, want 2", got)
	}
	// Out-of-range clamps to last voice.
	w.SetSelectedSlot(99)
	if got := w.SelectedSlot(); got != 3 {
		t.Errorf("SelectedSlot after clamp = %d, want 3", got)
	}
	// Negative clamps to first voice.
	w.SetSelectedSlot(-5)
	if got := w.SelectedSlot(); got != 0 {
		t.Errorf("SelectedSlot after neg = %d, want 0", got)
	}
}

// TestSelectionChangedCallback verifies the consumer-facing hook fires with
// a translated slot index (not the raw row).
func TestSelectionChangedCallback(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{"A", "B", "C"})
	w := New(m)
	defer w.Close()

	var got int
	got = -1
	w.SetOnSelectionChanged(func(slot int) { got = slot })
	w.SetSelectedSlot(1)
	if got != 1 {
		t.Errorf("onSel slot = %d, want 1", got)
	}
	w.SetSelectedSlot(2)
	if got != 2 {
		t.Errorf("onSel slot = %d, want 2", got)
	}
}

// TestCloseUnsubscribes verifies Close prevents further refresh callbacks.
// After Close, a mutation to the model must not change the widget's cached
// row contents (we re-set them manually and confirm they stick).
func TestCloseUnsubscribes(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []string{testVoiceAlpha})
	w := New(m)
	w.Close()

	// Stamp a sentinel into the Name cell. If the Subscribe callback is
	// still wired, the next mutation will overwrite it.
	w.table.GetCell(1, 1).SetText("SENTINEL")
	patches, err := voiceedit.BuildNamePatch("RENAMED")
	if err != nil {
		t.Fatalf("BuildNamePatch: %v", err)
	}
	for _, p := range patches {
		if err := m.ApplyVoicePatch(0, p); err != nil {
			t.Fatalf("ApplyVoicePatch: %v", err)
		}
	}
	if got := w.table.GetCell(1, 1).Text; got != "SENTINEL" {
		t.Errorf("after Close, refresh fired anyway: cell = %q, want SENTINEL", got)
	}
}
