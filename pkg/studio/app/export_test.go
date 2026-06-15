package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestExportOverwriteGuard_ExistingFile pins N-05: exporting to a path
// that already exists does NOT write immediately; it opens a confirm
// modal first, since a filesystem overwrite is outside the undo stack.
func TestExportOverwriteGuard_ExistingFile(t *testing.T) {
	a, _ := newTestAppEmpty(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "HRPSC.fzv")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil { //nolint:gosec // G306: t.TempDir fixture
		t.Fatal(err)
	}

	wrote := false
	a = a.exportWithOverwriteGuard(target, func() error { wrote = true; return nil })

	if wrote {
		t.Fatal("export wrote over an existing file without confirmation")
	}
	if a.confirm == nil || !a.confirm.IsOpen() {
		t.Fatal("export of an existing path should open the overwrite confirm modal")
	}
	// The original file is untouched while the prompt is up.
	if b, _ := os.ReadFile(target); string(b) != "original" {
		t.Errorf("existing file changed before confirmation: %q", b)
	}
}

// TestExportOverwriteGuard_NewFile pins the happy path: a non-existent
// target is written directly with a success status and no prompt.
func TestExportOverwriteGuard_NewFile(t *testing.T) {
	a, _ := newTestAppEmpty(t)
	target := filepath.Join(t.TempDir(), "NEW.fzv")

	wrote := false
	a = a.exportWithOverwriteGuard(target, func() error { wrote = true; return nil })

	if !wrote {
		t.Fatal("export to a new path should write immediately")
	}
	if a.confirm != nil && a.confirm.IsOpen() {
		t.Error("export to a new path should not prompt")
	}
}

// TestExportOverwriteGuard_ConfirmWrites pins the confirm path of N-05:
// once the user picks "Overwrite", the write actually runs.
func TestExportOverwriteGuard_ConfirmWrites(t *testing.T) {
	a, _ := newTestAppEmpty(t)
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})
	target := filepath.Join(t.TempDir(), "DUP.fzv")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil { //nolint:gosec // G306: t.TempDir fixture
		t.Fatal(err)
	}

	wrote := false
	a = a.exportWithOverwriteGuard(target, func() error { wrote = true; return nil })
	if !a.confirm.IsOpen() {
		t.Fatal("expected overwrite confirm to be open")
	}
	// Options are [Cancel, Overwrite]; move focus to Overwrite, confirm.
	m, _ := a.Update(keyPress(testKeyRight))
	a, _ = m.(App)
	// Enter on the focused "Overwrite" option resolves the confirm,
	// which runs the write as a side effect (sets wrote via the closure).
	a.Update(keyPress(testKeyEnter)) //nolint:errcheck // tea.Model/Cmd, no error to check; we assert via the side effect

	if !wrote {
		t.Error("confirming Overwrite did not run the write")
	}
}
