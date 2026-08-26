package webcore

import (
	"bytes"

	"github.com/philipcunningham/fizzle/pkg/disk"
	documentmodel "github.com/philipcunningham/fizzle/pkg/document"
)

// History has two bounds. R24 guarantees at least 100 whole-document states,
// including split pairs. Beyond that floor, a byte budget prevents a long
// editing session on one-disk documents from growing without limit.
const (
	historyMinDepth = 100
	historyCap      = 200
	historyByteCap  = 256 * 1024 * 1024
)

func (s *Session) pushHistory(doc documentmodel.State) {
	s.past = append(s.past, doc)
	for len(s.past) > historyMinDepth && (len(s.past) > historyCap || historyBytes(s.past) > historyByteCap) {
		s.past[0] = documentmodel.State{}
		s.past = s.past[1:]
	}
}

func historyBytes(states []documentmodel.State) int {
	total := 0
	for _, state := range states {
		total += state.SizeBytes()
	}
	return total
}

// BeginGesture opens an undo bracket: edits until CommitGesture coalesce into
// one history entry.
func (s *Session) BeginGesture() {
	if !s.inGesture {
		s.inGesture = true
		s.gestureBase = nil
	}
}

// CommitGesture closes the bracket and reports whether an edit landed.
func (s *Session) CommitGesture() bool {
	if !s.inGesture {
		return false
	}
	s.inGesture = false
	if s.gestureBase != nil {
		s.pushHistory(*s.gestureBase)
		s.gestureBase = nil
		s.revision++
		return true
	}
	return false
}

func (s *Session) endGesture() {
	if !s.inGesture {
		return
	}
	s.CommitGesture()
	s.BeginGesture()
}

// Undo restores the previous state under a fresh revision.
func (s *Session) Undo() (Snapshot, *Error) {
	s.endGesture()
	if len(s.past) == 0 {
		return s.Snapshot(), errf("nothing-to-undo", "nothing to undo")
	}
	prev := s.past[len(s.past)-1]
	img, err := disk.ReadImage(bytes.NewReader(prev.Image1()))
	if err != nil {
		return s.Snapshot(), errf("invalid-image", "history entry unreadable: %v", err)
	}
	current := s.state
	snap, cerr := s.adoptState(img, prev.Image2(), prev.UsesDIS())
	if cerr != nil {
		return snap, cerr
	}
	s.past = s.past[:len(s.past)-1]
	s.future = append(s.future, current)
	return s.Snapshot(), nil
}

// Redo restores the state most recently undone.
func (s *Session) Redo() (Snapshot, *Error) {
	s.endGesture()
	if len(s.future) == 0 {
		return s.Snapshot(), errf("nothing-to-redo", "nothing to redo")
	}
	next := s.future[len(s.future)-1]
	img, err := disk.ReadImage(bytes.NewReader(next.Image1()))
	if err != nil {
		return s.Snapshot(), errf("invalid-image", "history entry unreadable: %v", err)
	}
	current := s.state
	snap, cerr := s.adoptState(img, next.Image2(), next.UsesDIS())
	if cerr != nil {
		return snap, cerr
	}
	s.future = s.future[:len(s.future)-1]
	s.pushHistory(current)
	return s.Snapshot(), nil
}
