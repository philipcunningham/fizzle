package globaleffect

import (
	"strconv"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
)

func newTestModel(t *testing.T) *model.Model {
	t.Helper()
	_, path := fzfbuilder.MakeTestFZF(t, []string{"ALPHA"})
	m, err := model.New(path)
	if err != nil {
		t.Fatalf("model.New: %v", err)
	}
	return m
}

func TestNewBuildsPrimitive(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	w := New(m)
	defer w.Close()
	if w.Primitive() == nil {
		t.Fatalf("Primitive: nil")
	}
}

func TestBendInputReflectsByte(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.Bytes()[disk.BankEffectOffset+disk.EffectBendOffset] = 42

	w := New(m)
	defer w.Close()

	if got := w.bendIF.GetText(); got != "42" {
		t.Errorf("bendIF: got %q, want %q", got, "42")
	}
}

func TestModWheelLFOPitchReflectsByte(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.Bytes()[disk.BankEffectOffset+disk.EffectModLFPOffset] = 17

	w := New(m)
	defer w.Close()

	if got := w.cells[0][0].GetText(); got != "17" {
		t.Errorf("Mod x LFO Pitch cell: got %q, want %q", got, "17")
	}
}

func TestCommitByteWritesBend(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	w := New(m)
	defer w.Close()

	w.commitByte(disk.EffectBendOffset, 99)

	if got := m.Bytes()[disk.BankEffectOffset+disk.EffectBendOffset]; got != 99 {
		t.Errorf("bend byte: got %d, want 99", got)
	}
}

func TestCommitByteWritesModLFP(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	w := New(m)
	defer w.Close()

	w.commitByte(disk.EffectModLFPOffset, 64)

	if got := m.Bytes()[disk.BankEffectOffset+disk.EffectModLFPOffset]; got != 64 {
		t.Errorf("mod_lfp byte: got %d, want 64", got)
	}
}

func TestRefreshOnExternalApply(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	w := New(m)
	defer w.Close()

	// External Apply: should fire the subscriber and refresh the cell.
	if err := m.Apply(voiceedit.Patch{
		Offset: disk.BankEffectOffset + disk.EffectAftDCQOffset,
		Size:   1,
		Value:  120,
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got := w.cells[2][6].GetText(); got != strconv.Itoa(120) {
		t.Errorf("Aft x DCF Q cell: got %q, want %q", got, "120")
	}
}

func TestCellOffsetMatrixCovers21Cells(t *testing.T) {
	t.Parallel()
	seen := map[int]bool{}
	for r := 0; r < 3; r++ {
		for c := 0; c < 7; c++ {
			off := cellOffsets[r][c]
			if seen[off] {
				t.Errorf("duplicate offset %d at [%d][%d]", off, r, c)
			}
			seen[off] = true
		}
	}
	if len(seen) != 21 {
		t.Errorf("offset count: got %d, want 21", len(seen))
	}
}

func TestCloseUnsubscribesIdempotent(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	w := New(m)
	w.Close()
	w.Close() // second Close must not panic
}
