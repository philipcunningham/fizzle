package webcore

// Undo and redo restore the parse mode the state was recorded under.

import (
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
)

// Undo and redo restore the mode the state was recorded under, not a
// re-derivation from bytes an edit had already moved.
func TestUndoRedoKeepDISMode(t *testing.T) {
	t.Parallel()
	s, _ := openBanklessDisk(t)

	if _, cerr := s.AddArea(0, 0); cerr != nil {
		t.Fatalf("AddArea: %v", cerr)
	}
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	if _, cerr := s.Redo(); cerr != nil {
		t.Fatalf("Redo: %v", cerr)
	}
	snap, cerr := s.DeleteArea(0, 1)
	if cerr != nil {
		t.Fatalf("DeleteArea: %v", cerr)
	}
	if got := len(snap.Disk.Instrument.Voices); got != fzfbuilder.BanklessDumpVoices {
		t.Fatalf("voices after undo redo delete = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
	}

	// The other door: deleting the dump and undoing it restores the
	// document with its mode.
	if _, cerr := s.DeleteFile(disk.FullDumpName); cerr != nil {
		t.Fatalf("DeleteFile: %v", cerr)
	}
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	snap, cerr = s.DeleteArea(1, 0)
	if cerr != nil {
		t.Fatalf("DeleteArea after undo: %v", cerr)
	}
	if got := len(snap.Disk.Instrument.Voices); got != fzfbuilder.BanklessDumpVoices {
		t.Fatalf("voices after dump-delete undo = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
	}
}

// Undoing a wholesale load restores the replaced document's mode.
func TestUndoOfLoadRestoresDISMode(t *testing.T) {
	t.Parallel()
	s, _ := openBanklessDisk(t)

	fzf := testFZF(t, "PLAIN1", "PLAIN2")
	if _, cerr := s.LoadFZF(fzf); cerr != nil {
		t.Fatalf("LoadFZF: %v", cerr)
	}
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	snap, cerr := s.DeleteArea(1, 0)
	if cerr != nil {
		t.Fatalf("DeleteArea: %v", cerr)
	}
	if got := len(snap.Disk.Instrument.Voices); got != fzfbuilder.BanklessDumpVoices {
		t.Fatalf("voices after load undo = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
	}
}

// The recorded mode must be restored, not re-derived: an undo to a
// state whose bytes walk differently keeps the mode it was saved
// under, through repeated undo and redo.
func TestHistoryRestoresRecordedMode(t *testing.T) {
	t.Parallel()

	t.Run("undo to a mid-edit state", func(t *testing.T) {
		t.Parallel()
		s, _ := openBanklessDisk(t)
		for range 2 {
			if _, cerr := s.AddArea(0, 0); cerr != nil {
				t.Fatalf("AddArea: %v", cerr)
			}
		}
		if _, cerr := s.Undo(); cerr != nil {
			t.Fatalf("Undo: %v", cerr)
		}
		snap, cerr := s.DeleteArea(1, 0)
		if cerr != nil {
			t.Fatalf("DeleteArea: %v", cerr)
		}
		if got := len(snap.Disk.Instrument.Voices); got != fzfbuilder.BanklessDumpVoices {
			t.Fatalf("voices = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
		}
	})

	t.Run("undo redo undo", func(t *testing.T) {
		t.Parallel()
		s, _ := openBanklessDisk(t)
		if _, cerr := s.AddArea(0, 0); cerr != nil {
			t.Fatalf("AddArea: %v", cerr)
		}
		for _, step := range []func() (Snapshot, *Error){s.Undo, s.Redo, s.Undo} {
			if _, cerr := step(); cerr != nil {
				t.Fatalf("history step: %v", cerr)
			}
		}
		snap, cerr := s.DeleteArea(1, 0)
		if cerr != nil {
			t.Fatalf("DeleteArea: %v", cerr)
		}
		if got := len(snap.Disk.Instrument.Voices); got != fzfbuilder.BanklessDumpVoices {
			t.Fatalf("voices = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
		}
	})
}
