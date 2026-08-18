package model

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSave_EmptyPathReturnsError pins the guard: Save("") errors rather
// than writing a path-derived .tmp like "" or "/.tmp", which catches a
// callsite that forgot its destination.
func TestSave_EmptyPathReturnsError(t *testing.T) {
	m := FromBytes("", []byte("hello"))
	if err := m.Save(""); err == nil {
		t.Fatal("Save(\"\") returned nil; expected error")
	}
}

// TestSave_UnwritableDir_LeavesOriginalIntact is the data-loss safety
// check. Save writes a tmp file then renames, but a read-only directory
// fails even the .tmp write, and the existing target must stay
// byte-identical with no orphan .tmp left behind.
//
// Linux and macOS only: Windows doesn't honour 0500 perms for the owning
// user the same way.
func TestSave_UnwritableDir_LeavesOriginalIntact(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("0500 directory perms not enforced for the owner on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root; chmod 0500 doesn't block writes")
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "data.bin")
	original := []byte("the file the user wrote before this Save")
	if err := os.WriteFile(target, original, 0o644); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	m := FromBytes(target, []byte("the new bytes Save wants to write"))
	// Lock the directory so .tmp creation fails.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	defer func() {
		// Restore so t.TempDir cleanup can remove it.
		_ = os.Chmod(dir, 0o755)
	}()

	err := m.Save(target)
	if err == nil {
		t.Fatal("Save to unwritable dir returned nil; expected error")
	}

	// Original file unchanged.
	got, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("read target after failed save: %v", readErr)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("Save failure mutated target:\n  got  %q\n  want %q", got, original)
	}

	// No orphan .tmp.
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("Save failure left .tmp behind (stat err=%v)", err)
	}
}

// TestSave_HappyPath_ClearsDirtyAndStacks is the inverse check: a
// successful Save clears the dirty flag and both stacks. The editor gates
// quit-confirmation on dirty, so a stale flag re-prompts after a save.
func TestSave_HappyPath_ClearsDirtyAndStacks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "data.bin")

	m := FromBytes(target, []byte("initial"))
	if err := m.Apply(Patch{Offset: 0, Old: []byte("i"), New: []byte("X")}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !m.Dirty() {
		t.Fatal("expected dirty after Apply")
	}
	if !m.CanUndo() {
		t.Fatal("expected CanUndo after Apply")
	}

	if err := m.Save(target); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if m.Dirty() {
		t.Error("Save did not clear dirty")
	}
	if m.CanUndo() {
		t.Error("Save did not clear undo stack")
	}
	if m.CanRedo() {
		t.Error("Save did not clear redo stack")
	}

	// File on disk matches m.Bytes() exactly.
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read after Save: %v", err)
	}
	if !bytes.Equal(got, m.Bytes()) {
		t.Fatalf("Save wrote bytes that disagree with model:\n  disk  %q\n  model %q",
			got, m.Bytes())
	}
}
