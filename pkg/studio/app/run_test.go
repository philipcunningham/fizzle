package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveWorkspaceDir_Empty pins: empty directory resolves to
// cwd. Preserves the "fizzle studio" (no args) invocation landing
// on the cwd as workspace.
func TestResolveWorkspaceDir_Empty(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	ws, err := resolveWorkspaceDir("")
	if err != nil {
		t.Fatalf("resolveWorkspaceDir(): %v", err)
	}
	if ws != wd {
		t.Errorf("workspaceDir = %q, want %q", ws, wd)
	}
}

// TestResolveWorkspaceDir_Directory pins: an existing directory
// resolves to itself.
func TestResolveWorkspaceDir_Directory(t *testing.T) {
	dir := t.TempDir()
	ws, err := resolveWorkspaceDir(dir)
	if err != nil {
		t.Fatalf("resolveWorkspaceDir(%s): %v", dir, err)
	}
	if ws != dir {
		t.Errorf("workspaceDir = %q, want %q", ws, dir)
	}
}

// TestResolveWorkspaceDir_File pins: a file path is rejected with
// a clear error pointing the user at the parent directory. studio
// is workspace-oriented; opening a single file is done via the
// Workspace browser, not via the CLI.
func TestResolveWorkspaceDir_File(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "Piano.fzf")
	if err := os.WriteFile(file, []byte("fake fzf bytes"), 0o644); err != nil { //nolint:gosec // G306: test fixture under t.TempDir()
		t.Fatalf("seed: %v", err)
	}
	_, err := resolveWorkspaceDir(file)
	if err == nil {
		t.Fatalf("resolveWorkspaceDir(%s) returned nil error; expected one for a file path", file)
	}
}

// TestResolveWorkspaceDir_NonExistent pins: a path that doesn't
// exist returns an error rather than silently creating it.
func TestResolveWorkspaceDir_NonExistent(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")
	_, err := resolveWorkspaceDir(missing)
	if err == nil {
		t.Errorf("resolveWorkspaceDir(%s) returned nil error; expected one", missing)
	}
}
