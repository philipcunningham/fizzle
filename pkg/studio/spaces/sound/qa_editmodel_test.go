package sound

import (
	"bytes"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/studio/nav"
)

// TestSound_EscRevertsCellEdit pins F-QA-1: a single Esc reverts the
// whole cell edit (any stepped/typed changes) back to its entry value
// and exits edit mode, leaving no redo entry that could un-revert it.
func TestSound_EscRevertsCellEdit(t *testing.T) {
	sm, cleanup := bindFromPiano(t)
	defer cleanup()

	before := append([]byte(nil), sm.m.Bytes()...)

	sm.Apply(nav.Confirm) // enter edit on the default DCA cell
	if !sm.InEditMode() {
		t.Fatal("Confirm should enter edit mode")
	}

	// Step the focused field, routing like the App does (numeric fields
	// take raw keys via ConsumeNumericKey; enum fields step via Apply).
	if sm.InNumericEditMode() {
		sm.ConsumeNumericKey("up")
	} else {
		sm.Apply(nav.NavUp)
	}
	if bytes.Equal(sm.m.Bytes(), before) {
		t.Fatal("stepping should have changed the buffer")
	}

	// One Esc reverts everything and closes.
	if sm.InNumericEditMode() {
		sm.ConsumeNumericKey("esc")
	} else {
		sm.Apply(nav.Cancel)
	}
	if !bytes.Equal(sm.m.Bytes(), before) {
		t.Error("Esc did not revert the cell edit to its entry value")
	}
	if sm.InEditMode() {
		t.Error("Esc should exit edit mode in one press")
	}
	if sm.m.CanRedo() {
		t.Error("reverted edit left redo entries (Ctrl-Y would un-revert it)")
	}
}

// TestSound_StageCellsAreOneIndexed pins F-QA-26: envelope stage cells
// are labelled s1..s8 (matching the 1-based "stage N" copy/paste
// messages), not s0..s7.
func TestSound_StageCellsAreOneIndexed(t *testing.T) {
	// DCA stage cells begin at col 3; DCF at col 4.
	if got := cellLabel(rowDCA, 3); got != "[s1]" {
		t.Errorf("DCA first stage cell = %q, want [s1]", got)
	}
	if got := cellLabel(rowDCA, 10); got != "[s8]" {
		t.Errorf("DCA last stage cell = %q, want [s8]", got)
	}
	if got := cellLabel(rowDCF, 4); got != "[s1]" {
		t.Errorf("DCF first stage cell = %q, want [s1]", got)
	}
	if got := cellLabel(rowDCF, 11); got != "[s8]" {
		t.Errorf("DCF last stage cell = %q, want [s8]", got)
	}
}

// TestSound_TabMovesToNextField pins F-QA-2: Tab/Shift+Tab move between
// fields within a Sound cell (mirroring the Area editor), without
// leaving edit mode.
func TestSound_TabMovesToNextField(t *testing.T) {
	sm, cleanup := bindFromPiano(t)
	defer cleanup()

	// Position on a cell that has more than one field.
	found := false
	for r := 0; r < int(numRows) && !found; r++ {
		sm.row = row(r)
		for c := 0; c < cellCount(sm.row); c++ {
			if len(cellFields(row(r), c, sm.voiceOff)) > 1 {
				sm.col = c
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("no multi-field cell found in the fixture; cannot exercise Tab")
	}

	sm.Apply(nav.Confirm)
	if sm.fieldIdx != 0 {
		t.Fatalf("fieldIdx after entering edit = %d, want 0", sm.fieldIdx)
	}
	sm.MoveFieldInEdit(1)
	if sm.fieldIdx != 1 {
		t.Errorf("Tab: fieldIdx = %d, want 1", sm.fieldIdx)
	}
	if !sm.InEditMode() {
		t.Error("Tab should stay in edit mode")
	}
	sm.MoveFieldInEdit(-1)
	if sm.fieldIdx != 0 {
		t.Errorf("Shift+Tab: fieldIdx = %d, want 0", sm.fieldIdx)
	}
}
