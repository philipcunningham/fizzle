package layout

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/studio/loader"
	"github.com/philipcunningham/fizzle/pkg/studio/nav"
)

func bindFromPiano(t testing.TB) *Model {
	t.Helper()
	src := filepath.Join("..", "..", "..", "..", "testdata", "corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-a-piano", "Piano.fzf")
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("missing Piano.fzf: %v", err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "Piano.fzf")
	if err := os.WriteFile(target, raw, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed fixture: %v", err)
	}
	m, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("LoadContainer: %v", err)
	}
	lm := New()
	lm.SetContainer(m, info)
	return &lm
}

// TestLayoutNav_BankCursorClampsAtZero pins NavUp at Bank 0 is a
// no-op. We don't wrap around; that would be confusing for an
// 8-bank list.
func TestLayoutNav_BankCursorClampsAtZero(t *testing.T) {
	lm := bindFromPiano(t)
	for i := 0; i < 5; i++ {
		lm.Apply(nav.NavUp)
	}
	if lm.bankCursor != 0 {
		t.Errorf("bankCursor after 5x NavUp from 0 = %d, want 0", lm.bankCursor)
	}
}

// TestLayoutNav_BankCursorClampsAtMaxBanks pins NavDown doesn't
// run off the end of the all-8-banks-visible bank list.
func TestLayoutNav_BankCursorClampsAtMaxBanks(t *testing.T) {
	lm := bindFromPiano(t)
	for i := 0; i < 20; i++ {
		lm.Apply(nav.NavDown)
	}
	const maxBanks = 8
	if lm.bankCursor != maxBanks-1 {
		t.Errorf("bankCursor after 20x NavDown = %d, want %d", lm.bankCursor, maxBanks-1)
	}
}

// TestLayoutNav_DrillIntoBankShowsAreaList pins Enter on a bank
// flips to the area list with cursor at Area 0.
func TestLayoutNav_DrillIntoBankShowsAreaList(t *testing.T) {
	lm := bindFromPiano(t)
	if !lm.InBankList() {
		t.Fatal("should start in bank list")
	}
	lm.Apply(nav.Confirm) // Enter
	if lm.InBankList() {
		t.Errorf("Enter did not flip into area list")
	}
	if lm.areaCursor != 0 {
		t.Errorf("areaCursor after drill-in = %d, want 0", lm.areaCursor)
	}
}

// TestLayoutNav_CancelInAreaListReturnsToBankList pins Esc in the
// area list bubbles back up to the bank list.
func TestLayoutNav_CancelInAreaListReturnsToBankList(t *testing.T) {
	lm := bindFromPiano(t)
	lm.Apply(nav.Confirm) // drill in
	if lm.InBankList() {
		t.Fatal("setup failed: should have drilled in")
	}
	lm.Apply(nav.Cancel) // Esc
	if !lm.InBankList() {
		t.Errorf("Esc did not return to bank list")
	}
}

// TestLayoutNav_DeleteOnEmptyBankReturnsStatus pins that pressing
// Del on a bank past BankCount yields a status message (not an
// Intent), since there's nothing to delete.
func TestLayoutNav_DeleteOnEmptyBankReturnsStatus(t *testing.T) {
	lm := bindFromPiano(t)
	// Walk the cursor past info.BankCount.
	for i := 0; i < 8; i++ {
		lm.Apply(nav.NavDown)
	}
	if lm.bankCursor < lm.info.BankCount {
		t.Skipf("cursor=%d not past BankCount=%d; nothing to test", lm.bankCursor, lm.info.BankCount)
	}
	msg, intent := lm.Apply(nav.Delete)
	if intent.Kind != IntentNone {
		t.Errorf("Delete on empty bank emitted intent %v; want IntentNone", intent.Kind)
	}
	if msg == "" {
		t.Errorf("Delete on empty bank yielded no status message")
	}
}

// TestLayoutNav_DeleteInBankListOnMaterialisedBankEmitsIntent
// pins that Del on a populated bank emits IntentDeleteBank for the
// App to handle (which shows the confirmation modal).
func TestLayoutNav_DeleteInBankListOnMaterialisedBankEmitsIntent(t *testing.T) {
	lm := bindFromPiano(t)
	// bankCursor is at 0 by default, which is populated.
	_, intent := lm.Apply(nav.Delete)
	if intent.Kind != IntentDeleteBank {
		t.Errorf("Delete on materialised bank emitted %v; want IntentDeleteBank", intent.Kind)
	}
	if intent.BankIdx != 0 {
		t.Errorf("intent.BankIdx = %d, want 0", intent.BankIdx)
	}
}

// TestLayoutNav_AreaCursorClampsAtZero pins NavUp in the area list
// at Area 0 stays at 0.
func TestLayoutNav_AreaCursorClampsAtZero(t *testing.T) {
	lm := bindFromPiano(t)
	lm.Apply(nav.Confirm) // drill in
	for i := 0; i < 5; i++ {
		lm.Apply(nav.NavUp)
	}
	if lm.areaCursor != 0 {
		t.Errorf("areaCursor after NavUp from 0 = %d, want 0", lm.areaCursor)
	}
}
