package webcore

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

func imageHash(t *testing.T, s *Session) [32]byte {
	t.Helper()
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	return sha256.Sum256(out)
}

func TestUndoRedoRestoreByRevision(t *testing.T) {
	s, voice := importedSession(t)
	base := s.Snapshot()
	baseImage := imageHash(t, s)

	edited, cerr := s.SetParamNumber(voice, "cutoff", 90)
	if cerr != nil {
		t.Fatalf("SetParamNumber: %v", cerr)
	}
	editedImage := imageHash(t, s)

	undone, cerr := s.Undo()
	if cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	if undone.Revision <= edited.Revision {
		t.Fatalf("undo did not advance the revision: %d then %d", edited.Revision, undone.Revision)
	}
	if got := paramValue(t, s, voice, "cutoff"); got != paramFromSnapshot(t, base, voice, "cutoff") {
		t.Fatalf("undo did not restore cutoff: %v", got)
	}
	if imageHash(t, s) != baseImage {
		t.Fatal("undo did not restore the image bytes")
	}

	redone, cerr := s.Redo()
	if cerr != nil {
		t.Fatalf("Redo: %v", cerr)
	}
	if redone.Revision <= undone.Revision {
		t.Fatal("redo did not advance the revision")
	}
	if imageHash(t, s) != editedImage {
		t.Fatal("redo did not restore the edit")
	}
}

func paramFromSnapshot(t *testing.T, snap Snapshot, file, field string) any {
	t.Helper()
	for _, f := range snap.Disk.Files {
		if f.Name == file {
			return f.Params[field]
		}
	}
	t.Fatalf("file %q not in snapshot", file)
	return nil
}

func TestUndoWithNothingToUndo(t *testing.T) {
	s := NewSession()
	if _, cerr := s.Undo(); cerr == nil || cerr.Code != "nothing-to-undo" {
		t.Fatalf("cerr = %v, want nothing-to-undo", cerr)
	}
	if _, cerr := s.Redo(); cerr == nil || cerr.Code != "nothing-to-redo" {
		t.Fatalf("cerr = %v, want nothing-to-redo", cerr)
	}
}

func TestNewEditClearsRedo(t *testing.T) {
	s, voice := importedSession(t)
	if _, cerr := s.SetParamNumber(voice, "cutoff", 90); cerr != nil {
		t.Fatalf("SetParamNumber: %v", cerr)
	}
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	if _, cerr := s.SetParamNumber(voice, "cutoff", 30); cerr != nil {
		t.Fatalf("SetParamNumber: %v", cerr)
	}
	if _, cerr := s.Redo(); cerr == nil || cerr.Code != "nothing-to-redo" {
		t.Fatalf("redo after a new edit = %v, want nothing-to-redo", cerr)
	}
}

// R24: a drag bracketed as begin, coalesce, commit lands as exactly
// one undo entry.
func TestGestureCoalescesToOneUndoEntry(t *testing.T) {
	s, voice := importedSession(t)
	before := paramValue(t, s, voice, "cutoff")
	baseImage := imageHash(t, s)

	s.BeginGesture()
	for v := 10; v <= 90; v += 10 {
		if _, cerr := s.SetParamNumber(voice, "cutoff", v); cerr != nil {
			t.Fatalf("SetParamNumber(%d): %v", v, cerr)
		}
	}
	s.CommitGesture()

	if got := paramValue(t, s, voice, "cutoff"); got != 90 {
		t.Fatalf("cutoff = %v after gesture, want 90", got)
	}

	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	if got := paramValue(t, s, voice, "cutoff"); got != before {
		t.Fatalf("one undo did not revert the whole gesture: %v", got)
	}
	if imageHash(t, s) != baseImage {
		t.Fatal("undo did not restore the pre-gesture image")
	}
	// One more undo steps past the import to the blank disk; the whole
	// gesture consumed exactly one entry.
	undone, cerr := s.Undo()
	if cerr != nil {
		t.Fatalf("second undo: %v", cerr)
	}
	if len(undone.Disk.Files) != 0 {
		t.Fatalf("second undo should reach the blank disk, files = %+v", undone.Disk.Files)
	}
	if _, cerr := s.Undo(); cerr == nil || cerr.Code != "nothing-to-undo" {
		t.Fatalf("third undo = %v, want nothing-to-undo", cerr)
	}
}

func TestGestureWithNoEditsAddsNoHistory(t *testing.T) {
	s, voice := importedSession(t)
	s.BeginGesture()
	s.CommitGesture()
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo of the import: %v", cerr)
	}
	_ = voice
	if _, cerr := s.Undo(); cerr == nil {
		t.Fatal("empty gesture left an undo entry")
	}
}

func TestHistoryIsCapped(t *testing.T) {
	// R24 is a MUST: undo and redo cover every mutating operation, at
	// least 100 deep.
	if historyCap < 100 {
		t.Fatalf("historyCap = %d, want at least the 100 deep R24 requires", historyCap)
	}
	s, voice := importedSession(t)
	for i := 0; i < historyCap+10; i++ {
		if _, cerr := s.SetParamNumber(voice, "cutoff", i%128); cerr != nil {
			t.Fatalf("SetParamNumber: %v", cerr)
		}
	}
	undos := 0
	for {
		if _, cerr := s.Undo(); cerr != nil {
			break
		}
		undos++
	}
	if undos != historyCap {
		t.Fatalf("undo depth = %d, want the cap %d", undos, historyCap)
	}
}

func TestHistoryByteBudgetPreservesRequiredDepth(t *testing.T) {
	state := documentState{image: make([]byte, disk.ImageSize), image2: make([]byte, disk.ImageSize)}
	s := &Session{}
	for range historyCap {
		s.pushHistory(state)
	}
	if got := len(s.past); got < historyMinDepth || got >= historyCap {
		t.Fatalf("split-pair history depth = %d, want budgeted depth in [%d,%d)", got, historyMinDepth, historyCap)
	}
	if got := historyBytes(s.past); got > historyByteCap {
		t.Fatalf("budgeted history = %d bytes, want at most %d", got, historyByteCap)
	}
}

func TestSnapshotCarriesUndoAvailability(t *testing.T) {
	s, voice := importedSession(t)
	snap := s.Snapshot()
	if !snap.CanUndo || snap.CanRedo {
		t.Fatalf("after import: canUndo=%v canRedo=%v, want true/false", snap.CanUndo, snap.CanRedo)
	}
	if _, cerr := s.SetParamNumber(voice, "cutoff", 5); cerr != nil {
		t.Fatalf("SetParamNumber: %v", cerr)
	}
	undone, cerr := s.Undo()
	if cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	if !undone.CanRedo {
		t.Fatal("after undo, canRedo should be true")
	}
}

// An undo interleaved into an open gesture must not invert the
// timeline: the gesture closes first, so its entry lands before the
// history moves.
func TestUndoDuringGestureKeepsTheTimeline(t *testing.T) {
	s, file := importedSession(t)
	before := paramValue(t, s, file, fieldCutoff)

	s.BeginGesture()
	if _, cerr := s.SetParamNumber(file, fieldCutoff, 40); cerr != nil {
		t.Fatal(cerr)
	}
	// Mid-drag undo: the gesture's entry lands, then the undo moves.
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo mid-gesture: %v", cerr)
	}
	if got := paramValue(t, s, file, fieldCutoff); got != before {
		t.Fatalf("cutoff = %v after undo, want the pre-gesture %v", got, before)
	}
	// A late commit must not push a stale image on top.
	s.CommitGesture()
	if got := paramValue(t, s, file, fieldCutoff); got != before {
		t.Fatalf("cutoff = %v after the late commit, want %v", got, before)
	}
	// Redo still reaches the edit, in the right direction.
	if _, cerr := s.Redo(); cerr != nil {
		t.Fatalf("Redo: %v", cerr)
	}
	if got := paramValue(t, s, file, fieldCutoff); got != 40 {
		t.Errorf("cutoff = %v after redo, want 40", got)
	}
}

// The drag continues after a mid-drag undo: the pointer never came up,
// so the rest of it is still one gesture and lands one entry. Without
// the reopened bracket every movement after the undo pushes its own
// entry, and one drag leaves a dozen of them.
func TestDragAfterAMidDragUndoStillCoalesces(t *testing.T) {
	s, file := importedSession(t)

	s.BeginGesture()
	if _, cerr := s.SetParamNumber(file, fieldCutoff, 40); cerr != nil {
		t.Fatal(cerr)
	}
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo mid-gesture: %v", cerr)
	}

	// The finger is still down: three more movements, then the release.
	depth := len(s.past)
	for _, v := range []int{41, 42, 43} {
		if _, cerr := s.SetParamNumber(file, fieldCutoff, v); cerr != nil {
			t.Fatal(cerr)
		}
	}
	s.CommitGesture()

	if got := len(s.past) - depth; got != 1 {
		t.Fatalf("the rest of the drag landed %d history entries, want 1", got)
	}
	if got := paramValue(t, s, file, fieldCutoff); got != 43 {
		t.Fatalf("cutoff = %v, want the drag's last value 43", got)
	}
	// One undo takes the whole remainder back.
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatal(cerr)
	}
	if got := paramValue(t, s, file, fieldCutoff); got == 43 {
		t.Fatal("undo after the drag left the drag's value in place")
	}
}

// A gesture that changes nothing lands no history entry, so the UI can
// tell an empty press apart from an edit.
func TestEmptyGestureLandsNothing(t *testing.T) {
	s, _ := importedSession(t)
	before := mustExport(t, s)
	s.BeginGesture()
	if landed := s.CommitGesture(); landed {
		t.Error("an empty gesture should land no entry")
	}
	// Undo still reaches the state before the import, not a phantom
	// entry the empty gesture left behind.
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	if bytes.Equal(mustExport(t, s), before) {
		t.Error("the empty gesture pushed a history entry")
	}
}
