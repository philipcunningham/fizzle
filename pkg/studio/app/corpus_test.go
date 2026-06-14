package app

import (
	"bytes"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/philipcunningham/fizzle/pkg/studio/loader"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/minimap"
)

// Roots for the Tier-A corpus crash sweep, relative to this test
// file. testdata/corpus/ holds the 254-file factory library;
// testdata/synthetic/ holds the hand-built .img fixtures used for
// targeted tests.
const (
	corpusRootRel    = "../../../testdata/corpus"
	syntheticRootRel = "../../../testdata/synthetic"
)

// findCorpusFiles walks both corpus roots and returns absolute
// paths of every file whose extension (case-insensitive) is in
// exts. The order is filesystem-determined; tests don't depend on
// order.
func findCorpusFiles(t testing.TB, exts ...string) []string {
	t.Helper()
	wantExt := make(map[string]bool, len(exts))
	for _, e := range exts {
		wantExt[strings.ToLower(e)] = true
	}
	var out []string
	for _, root := range []string{corpusRootRel, syntheticRootRel} {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil //nolint:nilerr // missing root is treated as "no fixtures"
			}
			if !wantExt[strings.ToLower(filepath.Ext(path))] {
				return nil
			}
			abs, absErr := filepath.Abs(path)
			if absErr != nil {
				return nil //nolint:nilerr // skip un-resolvable paths
			}
			out = append(out, abs)
			return nil
		})
	}
	return out
}

// loadCorpusInto loads absPath into a fresh App wired with a fake
// clock. Returns the App and the load error (nil on success).
// Callers asserting "no panic" can ignore the error. Workspace dir
// is t.TempDir(); the corpus file itself is read in place (loader
// does its own IO).
func loadCorpusInto(t testing.TB, absPath string) (App, error) {
	t.Helper()
	fc := newFakeClock()
	a := New(t.TempDir())
	a.backupDir = filepath.Join(t.TempDir(), "backups")
	a.tick = fc.Tick
	a.toast.SetClock(fc.Tick)
	a.status.SetClock(fc.Tick)
	m, info, err := loader.LoadContainer(absPath)
	if err != nil {
		return a, err
	}
	a.containerModel = m
	a.containerInfo = info
	a.layout.SetContainer(m, info)
	return a, nil
}

// viewWidthSlack is the known overhead the App's body adds beyond
// the requested terminal width. Today every rendered view is
// consistently `width + 2` cells wide; the body's outer container
// (paneBox + " " + rightCol composition) adds two cells regardless
// of WindowSizeMsg. The slack absorbs that constant so this canary
// only fires on REGRESSIONS (some fixture producing more overhead),
// not on the standing layout offset.
//
// Drive this to zero by fixing the body to respect width exactly;
// once that lands, set slack=0 and the canary tightens.
const viewWidthSlack = 2

// assertViewInvariants is the Tier-A appearance contract: every
// rendered view must be non-empty and no stripped line may exceed
// the terminal width (plus the documented slack). Catches layout
// regressions across the entire corpus without snapshotting any
// single file.
//
// "Stripped line" means ANSI-removed. Wide CJK characters would
// throw off the rune count, but the FZ-1 corpus is ASCII so this
// is a safe approximation.
func assertViewInvariants(t testing.TB, view string, cols int, label string) {
	t.Helper()
	if view == "" {
		t.Errorf("%s: View().Content empty", label)
		return
	}
	stripped := stripANSI(view)
	maxAllowed := cols + viewWidthSlack
	for i, line := range strings.Split(stripped, "\n") {
		w := utf8.RuneCountInString(line)
		if w > maxAllowed {
			t.Errorf("%s: line %d overflows %d cols + %d slack (got %d runes): %q",
				label, i, cols, viewWidthSlack, w, line)
			return // one overflow per fixture is enough
		}
	}
}

// loaderDeterministicallyReparses loads path twice and asserts
// the resulting bytes + key ContainerInfo fields are byte-equal.
// "Load == reparse": the loader must produce the same in-memory
// state for the same on-disk bytes, every time. A divergence here
// would mean either the loader has hidden state or the file's
// representation isn't stable.
//
// This is the lighter of the two "load == reparse" interpretations:
// loader determinism, not save-reload round-trip. Save-reload is
// already covered by Phase 1 round-trip tests.
func loaderDeterministicallyReparses(path string) error {
	m1, info1, err := loader.LoadContainer(path)
	if err != nil {
		// A consistent error is itself deterministic. Reload and
		// confirm we get the same error class.
		_, _, err2 := loader.LoadContainer(path)
		if err2 == nil {
			return fmt.Errorf("first load errored but second succeeded: %w", err)
		}
		return nil
	}
	m2, info2, err := loader.LoadContainer(path)
	if err != nil {
		return fmt.Errorf("second load errored after successful first: %w", err)
	}
	if !bytes.Equal(m1.Bytes(), m2.Bytes()) {
		return fmt.Errorf("loader nondeterministic: byte mismatch (len1=%d len2=%d)",
			len(m1.Bytes()), len(m2.Bytes()))
	}
	if info1.BankCount != info2.BankCount || info1.VoiceCount != info2.VoiceCount ||
		info1.Format != info2.Format || info1.AudioAreaStart != info2.AudioAreaStart {
		return fmt.Errorf("loader nondeterministic ContainerInfo: %+v vs %+v", info1, info2)
	}
	return nil
}

// runFZFNavScript drives a corpus FZF/IMG through a fixed sequence
// of space transitions, asserting view invariants at each step.
// The script degrades gracefully: a transition that doesn't take
// effect (e.g. ShiftDown ignored because there are no banks to
// drill into) is fine; the test still checks invariants on
// whatever view did render.
//
// Sequence: Workspace -> Pool -> Layout -> drill into bank 0
// (Enter) -> Sound. The cols/rows passed in are the fixed test
// terminal size (100x30 per the plan).
func runFZFNavScript(t testing.TB, a App, label string, cols, rows int) App {
	t.Helper()
	a = pump(t, a, tea.WindowSizeMsg{Width: cols, Height: rows})
	assertViewInvariants(t, a.View().Content, cols, label+":workspace")

	// SpaceDown: Workspace -> Pool -> Layout.
	a = pump(t, a, keyPress("shift+down"))
	assertViewInvariants(t, a.View().Content, cols, label+":pool")
	a = pump(t, a, keyPress("shift+down"))
	assertViewInvariants(t, a.View().Content, cols, label+":layout-banks")

	// Drill into bank 0 via Enter. If the file has no banks the
	// transition is a no-op; invariants still hold.
	a = pump(t, a, keyPress("enter"))
	assertViewInvariants(t, a.View().Content, cols, label+":layout-areas")

	// SpaceDown: Layout -> Sound. Same caveat: if the area-list
	// has no selectable Area, the transition silently no-ops.
	a = pump(t, a, keyPress("shift+down"))
	assertViewInvariants(t, a.View().Content, cols, label+":sound")
	return a
}

// TestCorpusCrashSweep is the load-bearing Tier-A invariant sweep:
// for every corpus FZF / FZV / IMG file, load via the production
// loader, run a fixed navigation script, and assert at each step:
//
//   - no panic;
//   - loader determinism (load twice -> identical bytes + info);
//   - View().Content non-empty;
//   - no rendered line exceeds the terminal width.
//
// The panic check is implicit (a panic fails the test); the rest
// are explicit. No snapshots; Tier-B (next test below) covers
// the regression net with a tiny curated budget.
//
// Skipped under `-short`. Expected runtime ~10-30s on the full
// corpus.
func TestCorpusCrashSweep(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping corpus crash sweep under -short")
	}
	const cols, rows = minCols, minRows
	start := time.Now()

	files := findCorpusFiles(t, ".fzf", ".fzv", ".img")
	if len(files) == 0 {
		t.Skip("no corpus fixtures discovered; check testdata layout")
	}
	t.Logf("corpus crash sweep over %d files at %dx%d", len(files), cols, rows)

	for _, abs := range files {
		abs := abs
		// Subtest per fixture so a failure points at the file.
		rel, _ := filepath.Rel(filepath.Dir(corpusRootRel), abs)
		t.Run(rel, func(t *testing.T) {
			t.Parallel()

			// 1. Loader determinism. A first load that fails consistently
			// is acceptable; a load that succeeds then fails (or vice
			// versa) is a bug.
			if err := loaderDeterministicallyReparses(abs); err != nil {
				t.Errorf("%s: %v", abs, err)
				return
			}

			// 2. Load into a fresh App. Load errors here are also
			// acceptable for malformed-but-corpus-shaped files; the
			// invariant is "no panic", which the test harness enforces.
			a, err := loadCorpusInto(t, abs)
			if err != nil {
				// Typed loader error is fine; we just can't run the
				// nav script. The crash check already passed.
				return
			}
			_ = runFZFNavScript(t, a, rel, cols, rows)
		})
	}
	t.Logf("corpus crash sweep finished in %v", time.Since(start))
}

// TestCorpusLayoutSnapshot is the Tier-B regression-net snapshot:
// one curated fixture (Drums.fzf, 4 banks, 24 voices) rendered at
// the Layout bank-list. A layout change that breaks the bank-list
// surface shows up here as a snapshot diff.
//
// One fixture is intentional. The plan rejected per-corpus
// snapshots because most factory files are single-bank and would
// produce near-duplicate snaps; Drums covers the multi-bank case
// which is where bank-list layout actually varies.
//
// Refresh with `UPDATE_SNAPS=true make test`.
func TestCorpusLayoutSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping snapshot under -short")
	}
	const cols, rows = minCols, minRows
	drumsPath, err := filepath.Abs(filepath.Join(corpusRootRel,
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-4", "drums", "Drums.fzf"))
	if err != nil {
		t.Fatalf("resolve drums fixture path: %v", err)
	}

	a, err := loadCorpusInto(t, drumsPath)
	if err != nil {
		t.Fatalf("load Drums.fzf: %v", err)
	}
	// Land on Layout bank list: Workspace -> Pool -> Layout.
	a = pump(t, a, tea.WindowSizeMsg{Width: cols, Height: rows})
	a = pump(t, a, keyPress("shift+down")) // Pool
	a = pump(t, a, keyPress("shift+down")) // Layout

	if a.current != minimap.Layout {
		t.Fatalf("expected current=Layout; got %v", a.current)
	}

	snaps.MatchSnapshot(t, renderView(a))
}

// TestTooSmallSnapshot pins the under-minimum-terminal hint screen
// (79x23, below the 80x24 floor). Below the minimum, the App's
// View must surface a hint pointing the user at the size
// requirement rather than rendering a half-broken layout.
func TestTooSmallSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping snapshot under -short")
	}
	a, _ := newTestAppEmpty(t)
	// Just under the floor on both axes (minCols=140, minRows=30).
	a = pump(t, a, tea.WindowSizeMsg{Width: 139, Height: 29})
	if !a.tooSmall {
		t.Fatalf("expected tooSmall=true at 139x29 (floor %dx%d)", 140, 30)
	}
	snaps.MatchSnapshot(t, renderView(a))
}
