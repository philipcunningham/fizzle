package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/nav"
)

// TestWorkspace_ShowsSizeAndFlagsInvalidDisk pins F-04: each file row
// carries a size, and an .img whose length is not a valid disk image
// size is flagged so junk and real disks are distinguishable without
// opening them.
func TestWorkspace_ShowsSizeAndFlagsInvalidDisk(t *testing.T) {
	dir := t.TempDir()
	// A valid-size disk and a too-small (junk) disk.
	if err := os.WriteFile(filepath.Join(dir, "good.img"), make([]byte, disk.ImageSize), 0o644); err != nil { //nolint:gosec // G703: t.TempDir fixture
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "junk.img"), make([]byte, 200), 0o644); err != nil { //nolint:gosec // G703: t.TempDir fixture
		t.Fatal(err)
	}

	m := New(dir)
	rendered := m.View(140, 40)

	// A formatted size appears (the valid disk is 1.2 MB).
	if !strings.Contains(rendered, "MB") && !strings.Contains(rendered, "KB") {
		t.Errorf("workspace listing shows no file sizes:\n%s", rendered)
	}
	// The junk .img is flagged as an invalid size.
	if !strings.Contains(rendered, sizeFlag) {
		t.Errorf("invalid-size .img is not flagged with %q:\n%s", sizeFlag, rendered)
	}
}

// writeFixture creates an empty file at path with permissive permissions
// suitable for a test temp directory. gosec G703 is suppressed because
// the file lives under t.TempDir().
func writeFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under t.TempDir()
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

// findCursorFor scans the model's entries for the row matching name and
// places the cursor there. Fails the test when name is absent.
func findCursorFor(t *testing.T, m *Model, name string) {
	t.Helper()
	for i, e := range m.entries {
		if e.Name == name {
			m.cursor = i
			return
		}
	}
	t.Fatalf("entry %q not found in entries=%v", name, m.entries)
}

// TestFileKindDispatch covers the file-kind dispatch table for
// Apply(nav.Confirm). Each visible kind maps to the expected Intent;
// KindDir descends (no Intent); KindUnknown rows are filtered out at
// reload time so they never reach Apply.
func TestFileKindDispatch(t *testing.T) {
	dir := t.TempDir()

	// Seed one file per supported kind plus a subdirectory. Unknown
	// extensions are filtered at reload, so they cannot be confirmed;
	// that branch is exercised separately below.
	subDir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	writeFixture(t, filepath.Join(dir, "disk.img"))
	writeFixture(t, filepath.Join(dir, "dump.fzf"))
	writeFixture(t, filepath.Join(dir, "voice.fzv"))
	writeFixture(t, filepath.Join(dir, "sample.wav"))

	cases := []struct {
		name       string
		entry      string
		wantIntent IntentKind
	}{
		{name: "disk emits OpenContainer", entry: "disk.img", wantIntent: IntentOpenContainer},
		{name: "dump emits OpenContainer", entry: "dump.fzf", wantIntent: IntentOpenContainer},
		{name: "voice emits AddVoiceToPool", entry: "voice.fzv", wantIntent: IntentAddVoiceToPool},
		{name: "sample emits AddSampleToPool", entry: "sample.wav", wantIntent: IntentAddSampleToPool},
		{name: "dir emits no Intent", entry: "subdir", wantIntent: IntentNone},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New(dir)
			findCursorFor(t, &m, tc.entry)
			_, intent := m.Apply(nav.Confirm)
			if intent.Kind != tc.wantIntent {
				t.Errorf("Apply(Confirm) on %q intent.Kind = %v, want %v",
					tc.entry, intent.Kind, tc.wantIntent)
			}
			// Files (non-directory kinds) must report their full path.
			if tc.wantIntent != IntentNone {
				wantPath := filepath.Join(dir, tc.entry)
				if intent.Path != wantPath {
					t.Errorf("intent.Path = %q, want %q", intent.Path, wantPath)
				}
			}
		})
	}
}

// TestFileKindDispatch_UnknownFiltered confirms files with unsupported
// extensions never reach Apply: reload removes them from m.entries so
// the cursor cannot land on a KindUnknown row in practice.
func TestFileKindDispatch_UnknownFiltered(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, filepath.Join(dir, "readme.txt"))
	writeFixture(t, filepath.Join(dir, "notes.md"))

	m := New(dir)
	if len(m.entries) != 0 {
		t.Errorf("unknown extensions should be filtered; got entries=%v", m.entries)
	}
	// Apply(Confirm) on an empty listing is a no-op.
	status, intent := m.Apply(nav.Confirm)
	if status != "" || intent.Kind != IntentNone {
		t.Errorf("Apply(Confirm) on empty listing returned status=%q intent=%v",
			status, intent)
	}
}

// TestDirectoryDrill confirms Enter on a directory row changes cwd to
// that subdirectory and resets the cursor.
func TestDirectoryDrill(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "child")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}
	writeFixture(t, filepath.Join(sub, "inner.img"))

	m := New(root)
	findCursorFor(t, &m, "child")

	_, intent := m.Apply(nav.Confirm)
	if intent.Kind != IntentNone {
		t.Errorf("descend should emit no Intent, got %v", intent)
	}
	if m.CurrentDirectory() != sub {
		t.Errorf("cwd = %q, want %q", m.CurrentDirectory(), sub)
	}
	if m.Cursor() != 0 {
		t.Errorf("cursor after descend = %d, want 0", m.Cursor())
	}
	// The inner file should now be visible in the listing.
	if len(m.entries) != 1 || m.entries[0].Name != "inner.img" {
		t.Errorf("after descend entries = %v, want [inner.img]", m.entries)
	}
}

// TestDirectoryAscent confirms nav.Cancel (and nav.NavLeft) ascend to
// the parent directory, and that ascent is bounded by the workspace
// root.
func TestDirectoryAscent(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "child")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}
	writeFixture(t, filepath.Join(sub, "inner.img"))

	m := New(root)
	findCursorFor(t, &m, "child")
	if _, intent := m.Apply(nav.Confirm); intent.Kind != IntentNone {
		t.Fatalf("descend should not emit an intent, got %v", intent)
	}
	if m.CurrentDirectory() != sub {
		t.Fatalf("setup: cwd = %q, want %q", m.CurrentDirectory(), sub)
	}

	t.Run("Cancel ascends to parent", func(t *testing.T) {
		status, intent := m.Apply(nav.Cancel)
		if status != "" {
			t.Errorf("ascend status = %q, want empty", status)
		}
		if intent.Kind != IntentNone {
			t.Errorf("ascend should not emit an intent, got %v", intent)
		}
		if m.CurrentDirectory() != root {
			t.Errorf("cwd after ascend = %q, want %q", m.CurrentDirectory(), root)
		}
	})

	t.Run("Cancel at root surfaces hint and is a no-op", func(t *testing.T) {
		status, intent := m.Apply(nav.Cancel)
		if status == "" {
			t.Errorf("status at root = empty, want a hint message")
		}
		if intent.Kind != IntentNone {
			t.Errorf("intent at root = %v, want none", intent)
		}
		if m.CurrentDirectory() != root {
			t.Errorf("cwd after blocked ascend = %q, want %q",
				m.CurrentDirectory(), root)
		}
	})

	t.Run("NavLeft also ascends", func(t *testing.T) {
		// Descend again, then NavLeft should symmetrically ascend.
		findCursorFor(t, &m, "child")
		if _, intent := m.Apply(nav.Confirm); intent.Kind != IntentNone {
			t.Fatalf("re-descend should not emit an intent, got %v", intent)
		}
		if m.CurrentDirectory() != sub {
			t.Fatalf("setup: cwd = %q, want %q", m.CurrentDirectory(), sub)
		}
		if _, intent := m.Apply(nav.NavLeft); intent.Kind != IntentNone {
			t.Errorf("NavLeft ascend should not emit an intent, got %v", intent)
		}
		if m.CurrentDirectory() != root {
			t.Errorf("cwd after NavLeft = %q, want %q",
				m.CurrentDirectory(), root)
		}
	})
}

// TestCursorClamping pins NavUp at row 0 and NavDown at the last row
// as no-ops.
func TestCursorClamping(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, filepath.Join(dir, "a.img"))
	writeFixture(t, filepath.Join(dir, "b.img"))
	writeFixture(t, filepath.Join(dir, "c.img"))

	m := New(dir)
	if len(m.entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(m.entries))
	}

	t.Run("NavUp at row 0 stays at 0", func(t *testing.T) {
		m.cursor = 0
		m.Apply(nav.NavUp)
		if m.Cursor() != 0 {
			t.Errorf("cursor after NavUp at row 0 = %d, want 0", m.Cursor())
		}
	})

	t.Run("NavDown at last row stays at last row", func(t *testing.T) {
		last := len(m.entries) - 1
		m.cursor = last
		m.Apply(nav.NavDown)
		if m.Cursor() != last {
			t.Errorf("cursor after NavDown at row %d = %d, want %d",
				last, m.Cursor(), last)
		}
	})

	t.Run("NavDown advances within bounds", func(t *testing.T) {
		m.cursor = 0
		m.Apply(nav.NavDown)
		if m.Cursor() != 1 {
			t.Errorf("cursor after NavDown from 0 = %d, want 1", m.Cursor())
		}
	})

	t.Run("NavUp retreats within bounds", func(t *testing.T) {
		m.cursor = 2
		m.Apply(nav.NavUp)
		if m.Cursor() != 1 {
			t.Errorf("cursor after NavUp from 2 = %d, want 1", m.Cursor())
		}
	})
}

// TestReloadShowsNewFile pins that calling reload (via Refresh) after a
// file appears on disk surfaces the new file in the listing.
func TestReloadShowsNewFile(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, filepath.Join(dir, "first.img"))

	m := New(dir)
	if len(m.entries) != 1 || m.entries[0].Name != "first.img" {
		t.Fatalf("initial entries = %v, want [first.img]", m.entries)
	}

	// Drop a second supported file into the directory after construction.
	writeFixture(t, filepath.Join(dir, "second.fzv"))

	m.Refresh()

	if len(m.entries) != 2 {
		t.Fatalf("entries after Refresh = %v, want 2 items", m.entries)
	}
	names := map[string]bool{}
	for _, e := range m.entries {
		names[e.Name] = true
	}
	if !names["first.img"] || !names["second.fzv"] {
		t.Errorf("after Refresh names = %v, want first.img + second.fzv", names)
	}
}
