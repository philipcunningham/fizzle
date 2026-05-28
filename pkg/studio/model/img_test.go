package model

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

// TestNewLoadsImg verifies that .img sources are auto-extracted and the
// resulting model carries the FZF bytes from the embedded full-dump entry.
// Uses the committed BRASS.img test fixture.
func TestNewLoadsImg(t *testing.T) {
	t.Parallel()
	m, err := New("../../../testdata/synthetic/BRASS.img")
	if err != nil {
		t.Fatalf("New(BRASS.img): %v", err)
	}
	if m.Header() == nil {
		t.Fatalf("Header is nil after loading .img")
	}
	if m.Header().NVoice == 0 {
		t.Errorf("loaded .img reports zero voices")
	}
	if len(m.Bytes()) < disk.SectorSize {
		t.Errorf("Bytes too short: %d", len(m.Bytes()))
	}
}

// TestSaveToImgPersists copies a fixture to a writable temp dir, mutates a
// bank name through the model, calls Save, then re-loads to verify the
// change persisted to the .img.
func TestSaveToImgPersists(t *testing.T) {
	t.Parallel()
	src := "../../../testdata/synthetic/BRASS.img"
	srcData, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read src: %v", err)
	}
	tmp := filepath.Join(t.TempDir(), "BRASS.img")
	if err := os.WriteFile(tmp, srcData, 0644); err != nil { //nolint:gosec // G703: tmp is t.TempDir()-derived, not user-tainted
		t.Fatalf("copy src: %v", err)
	}

	m, err := New(tmp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := m.SetBankName(0, "ZZZZZZ"); err != nil {
		t.Fatalf("SetBankName: %v", err)
	}
	if !m.IsDirty() {
		t.Fatalf("expected dirty after SetBankName")
	}
	if err := m.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if m.IsDirty() {
		t.Errorf("expected not-dirty after Save")
	}
	// Re-open and verify the bank name persisted.
	m2, err := New(tmp)
	if err != nil {
		t.Fatalf("New again: %v", err)
	}
	if got := m2.BankName(0); got != "ZZZZZZ" {
		t.Errorf("BankName(0) on re-load = %q, want ZZZZZZ", got)
	}
}
