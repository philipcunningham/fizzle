package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

// TestRunAutoSave_WritesSingleBakInBackupDir pins the new autosave
// contract: one `<base>.bak` per container, written to backupDir,
// overwritten on each tick (no timestamped accumulation, no
// pruning). Tests override backupDir; production uses the
// workspace dir.
func TestRunAutoSave_WritesSingleBakInBackupDir(t *testing.T) {
	workspace := t.TempDir()
	backups := t.TempDir()

	a := New(workspace)
	a.backupDir = backups

	// Replace the untitled container with one that has a non-empty
	// path and is dirty, so runAutoSave will write.
	data := make([]byte, 16)
	m := model.FromBytes("Piano.fzf", data)
	if err := m.Apply(model.Patch{
		Offset: 0,
		Old:    []byte{0x00},
		New:    []byte{0x01},
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	a.containerModel = m

	a.runAutoSave()

	// Workspace dir must be empty of any .bak file.
	wsEntries, _ := os.ReadDir(workspace)
	for _, e := range wsEntries {
		if filepath.Ext(e.Name()) == ".bak" {
			t.Errorf("autosave leaked into workspace: %s", e.Name())
		}
	}

	bakPath := filepath.Join(backups, "Piano.fzf.bak")
	if _, err := os.Stat(bakPath); err != nil {
		t.Fatalf("Piano.fzf.bak missing after autosave: %v", err)
	}
	first, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatalf("read bak: %v", err)
	}
	if !bytes.Equal(first, m.Bytes()) {
		t.Errorf("bak bytes diverge from model bytes after autosave")
	}

	// Mutate, autosave again; same path, overwritten content.
	if err := m.Apply(model.Patch{
		Offset: 1,
		Old:    []byte{0x00},
		New:    []byte{0xFF},
	}); err != nil {
		t.Fatalf("Apply 2: %v", err)
	}
	a.runAutoSave()

	bakEntries, _ := os.ReadDir(backups)
	bakCount := 0
	for _, e := range bakEntries {
		if filepath.Ext(e.Name()) == ".bak" {
			bakCount++
		}
	}
	if bakCount != 1 {
		t.Errorf(".bak count after 2x autosave = %d, want 1 (overwritten)", bakCount)
	}
	second, _ := os.ReadFile(bakPath)
	if !bytes.Equal(second, m.Bytes()) {
		t.Errorf("bak not refreshed by second autosave")
	}
	if bytes.Equal(first, second) {
		t.Errorf("bak content unchanged across two distinct autosaves")
	}
}

// TestRunAutoSave_SkipsCleanAndUntitledContainers asserts the no-op
// branches: clean containers don't write, and untitled containers
// (empty path) don't write either; autosave needs a base name to
// shadow.
func TestRunAutoSave_SkipsCleanAndUntitledContainers(t *testing.T) {
	cases := []struct {
		name  string
		m     *model.Model
		dirty bool
	}{
		{name: "clean", m: model.FromBytes("Piano.fzf", []byte{0, 0, 0}), dirty: false},
		{name: "untitled", m: nil, dirty: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backups := t.TempDir()
			a := New(t.TempDir())
			a.backupDir = backups
			if tc.m != nil {
				a.containerModel = tc.m
			}
			a.runAutoSave()
			entries, _ := os.ReadDir(backups)
			if len(entries) != 0 {
				names := make([]string, 0, len(entries))
				for _, e := range entries {
					names = append(names, e.Name())
				}
				t.Errorf("expected no .bak; got %v", names)
			}
		})
	}
}

// TestClearAutoSaveBackup_RemovesBakOnSuccessfulSave pins that a
// successful Save deletes the .bak so the next launch doesn't
// prompt for recovery against bytes the user has already committed.
func TestClearAutoSaveBackup_RemovesBakOnSuccessfulSave(t *testing.T) {
	backups := t.TempDir()
	a := New(t.TempDir())
	a.backupDir = backups

	bakPath := filepath.Join(backups, "Piano.fzf.bak")
	if err := os.WriteFile(bakPath, []byte("snapshot"), 0o644); err != nil {
		t.Fatalf("seed bak: %v", err)
	}

	a.clearAutoSaveBackup("/some/path/Piano.fzf")
	if _, err := os.Stat(bakPath); !os.IsNotExist(err) {
		t.Errorf("clearAutoSaveBackup did not remove the .bak (stat err=%v)", err)
	}
	// Idempotent: second call is a no-op (not an error).
	a.clearAutoSaveBackup("/some/path/Piano.fzf")
}

// TestFindRecoveryCandidate_NewerBakBeatsContainer pins the
// recovery selector: a .bak file newer than its named container
// is offered for recovery; an older one (or missing) is ignored.
func TestFindRecoveryCandidate_NewerBakBeatsContainer(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "Piano.fzf")
	bak := filepath.Join(dir, "Piano.fzf.bak")

	if err := os.WriteFile(src, []byte("src"), 0o644); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	// Backdate src by one minute so bak (written next) is newer.
	old := time.Now().Add(-1 * time.Minute)
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatalf("chtimes src: %v", err)
	}
	if err := os.WriteFile(bak, []byte("snapshot"), 0o644); err != nil {
		t.Fatalf("seed bak: %v", err)
	}

	if got := findRecoveryCandidate(dir, src); got != bak {
		t.Errorf("newer bak not surfaced: got %q, want %q", got, bak)
	}

	// Reverse: backdate the bak instead; no recovery offered.
	if err := os.Chtimes(src, time.Now(), time.Now()); err != nil {
		t.Fatalf("chtimes src forward: %v", err)
	}
	if err := os.Chtimes(bak, old, old); err != nil {
		t.Fatalf("chtimes bak backward: %v", err)
	}
	if got := findRecoveryCandidate(dir, src); got != "" {
		t.Errorf("older bak should be ignored; got %q", got)
	}
}
