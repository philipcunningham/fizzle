package sound

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/loader"
	"github.com/philipcunningham/fizzle/pkg/studio/nav"
)

// bindFromPiano materialises a Sound model bound to a real voice in
// the Piano.fzf fixture. Tests in this file share the helper so the
// nav assertions read short.
func bindFromPiano(t testing.TB) (*Model, func()) {
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
	voiceAreaStart := info.BankCount * disk.SectorSize
	sm := New()
	sm.Bind(m, info.BankCount, voiceAreaStart, info.AudioAreaStart, 0, 0)
	if !sm.HasVoice() {
		t.Fatalf("expected a voice bound for Bank 0 Area 0; got HasVoice=false")
	}
	return &sm, func() {}
}

// TestSoundNav_LeftRightClampsAtRowBounds pins the within-row
// navigation: Right past the last column stays at the last column;
// Left past column 0 stays at column 0.
func TestSoundNav_LeftRightClampsAtRowBounds(t *testing.T) {
	sm, cleanup := bindFromPiano(t)
	defer cleanup()

	// Row starts at rowDCA with col 1 after Bind. Walk right past
	// the end of the row.
	for i := 0; i < 100; i++ {
		sm.Apply(nav.NavRight)
	}
	if got, want := sm.col, cellCount(sm.row)-1; got != want {
		t.Errorf("NavRight saturated at col=%d; want %d (cellCount(%s)-1)", got, want, sm.row)
	}
	// Walk left past the start.
	for i := 0; i < 100; i++ {
		sm.Apply(nav.NavLeft)
	}
	if got, want := sm.col, 0; got != want {
		t.Errorf("NavLeft saturated at col=%d; want %d", got, want)
	}
}

// TestSoundNav_UpDownClampsRow pins that NavUp at the top row and
// NavDown at the bottom row are no-ops (row stays at the boundary).
func TestSoundNav_UpDownClampsRow(t *testing.T) {
	sm, cleanup := bindFromPiano(t)
	defer cleanup()

	// Walk to the bottom.
	for i := 0; i < int(numRows)*3; i++ {
		sm.Apply(nav.NavDown)
	}
	if got, want := sm.row, numRows-1; got != want {
		t.Errorf("NavDown saturated at row=%d; want %d", got, want)
	}
	// Walk to the top.
	for i := 0; i < int(numRows)*3; i++ {
		sm.Apply(nav.NavUp)
	}
	if got, want := sm.row, row(0); got != want {
		t.Errorf("NavUp saturated at row=%d; want %d", got, want)
	}
}

// TestSoundNav_UpDownClampsColumnToNewRow pins the column-clamp on
// row transition: if the user is on a high column in a wide row,
// then steps Down into a narrower row, col must clamp to the new
// row's cellCount-1 instead of indexing past the end.
//
// This is the behaviour the test plan called out as
// "column-preserve-across-rows": preserve when valid, clamp when
// not.
func TestSoundNav_UpDownClampsColumnToNewRow(t *testing.T) {
	sm, cleanup := bindFromPiano(t)
	defer cleanup()

	// Find a row pair where the source row is wider than the
	// destination row. rowLFO has 3 cells; rowDCF has 12. Walk to
	// the widest row (DCF), then jump into LFO.
	for sm.row != rowDCF {
		sm.Apply(nav.NavDown)
		if sm.row == numRows-1 && sm.row != rowDCF {
			break
		}
	}
	// Push col to the right edge of DCF.
	for i := 0; i < cellCount(rowDCF)+2; i++ {
		sm.Apply(nav.NavRight)
	}
	if sm.col != cellCount(rowDCF)-1 {
		t.Fatalf("expected col=%d before transition; got %d", cellCount(rowDCF)-1, sm.col)
	}
	// Step Down into rowLFO.
	for sm.row != rowLFO {
		sm.Apply(nav.NavDown)
		if sm.row == numRows-1 {
			break
		}
	}
	if sm.row != rowLFO {
		t.Fatalf("could not reach rowLFO; landed at row=%s", sm.row)
	}
	want := cellCount(rowLFO) - 1
	if sm.col != want {
		t.Errorf("col after transition into narrower row = %d; want clamp to %d", sm.col, want)
	}
}
