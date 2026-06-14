package model

import (
	"bytes"
	"testing"

	"pgregory.net/rapid"
)

// TestModel_ApplyUndoIsIdentity is the headline invariant for the
// patch/undo system: apply any batch, then Undo, and the bytes must
// equal what they were before the apply. If this ever fails, undo
// is silently lossy and every editor in studio inherits the bug.
func TestModel_ApplyUndoIsIdentity(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		bufLen := rapid.IntRange(16, 2048).Draw(rt, "bufLen")
		buf := make([]byte, bufLen)
		for i := range buf {
			buf[i] = byte(rapid.IntRange(0, 255).Draw(rt, "byte")) //nolint:gosec // G115: rapid range bounded to 0..255
		}
		m := FromBytes("", buf)
		original := append([]byte(nil), m.Bytes()...)

		nPatches := rapid.IntRange(1, 8).Draw(rt, "nPatches")
		var patches []Patch
		for i := 0; i < nPatches; i++ {
			off := rapid.IntRange(0, bufLen-1).Draw(rt, "off")
			maxLen := bufLen - off
			if maxLen > 16 {
				maxLen = 16
			}
			plen := rapid.IntRange(1, maxLen).Draw(rt, "plen")
			old := append([]byte(nil), m.Bytes()[off:off+plen]...)
			newBytes := make([]byte, plen)
			for j := range newBytes {
				newBytes[j] = byte(rapid.IntRange(0, 255).Draw(rt, "newByte")) //nolint:gosec // G115: rapid range bounded to 0..255
			}
			patches = append(patches, Patch{Offset: off, Old: old, New: newBytes})
			// Apply progressively so the next patch's Old reflects the
			// running state.
			if err := m.Apply(patches[i]); err != nil {
				rt.Fatalf("Apply patch %d: %v", i, err)
			}
		}

		// Undo each one in reverse.
		for i := nPatches - 1; i >= 0; i-- {
			if !m.CanUndo() {
				rt.Fatalf("CanUndo=false at step %d; expected %d undo entries", i, nPatches)
			}
			if err := m.Undo(); err != nil {
				rt.Fatalf("Undo at step %d: %v", i, err)
			}
		}

		if !bytes.Equal(m.Bytes(), original) {
			rt.Fatalf("apply+undo not identity:\n  got  %x\n  want %x", m.Bytes(), original)
		}
	})
}

// TestModel_ApplyUndoRedoMatchesApplied pins that Redo after Undo
// reproduces the post-apply state exactly. Catches asymmetric
// edge cases (e.g. an undo that drops the redo stack).
func TestModel_ApplyUndoRedoMatchesApplied(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		bufLen := rapid.IntRange(16, 1024).Draw(rt, "bufLen")
		buf := make([]byte, bufLen)
		for i := range buf {
			buf[i] = byte(rapid.IntRange(0, 255).Draw(rt, "seedByte")) //nolint:gosec // G115: rapid range bounded to 0..255
		}
		m := FromBytes("", buf)

		off := rapid.IntRange(0, bufLen-1).Draw(rt, "off")
		maxLen := bufLen - off
		if maxLen > 16 {
			maxLen = 16
		}
		plen := rapid.IntRange(1, maxLen).Draw(rt, "plen")

		old := append([]byte(nil), m.Bytes()[off:off+plen]...)
		newBytes := make([]byte, plen)
		for i := range newBytes {
			newBytes[i] = byte(rapid.IntRange(0, 255).Draw(rt, "newByte")) //nolint:gosec // G115: rapid range bounded to 0..255
		}

		if err := m.Apply(Patch{Offset: off, Old: old, New: newBytes}); err != nil {
			rt.Fatalf("Apply: %v", err)
		}
		applied := append([]byte(nil), m.Bytes()...)

		if err := m.Undo(); err != nil {
			rt.Fatalf("Undo: %v", err)
		}
		if !m.CanRedo() {
			rt.Fatalf("CanRedo=false after Undo")
		}
		if err := m.Redo(); err != nil {
			rt.Fatalf("Redo: %v", err)
		}
		if !bytes.Equal(m.Bytes(), applied) {
			rt.Fatalf("undo+redo did not reproduce applied state:\n  got  %x\n  want %x",
				m.Bytes(), applied)
		}
	})
}

// TestModel_BatchUndoReversesAtomically pins that ApplyBatch + Undo
// reverts the entire batch in one Undo call (not patch-by-patch).
// The editor.go field patches use ApplyBatch for multi-site edits
// (e.g. the envelope Role change touches both SUS and the stop
// level); a per-patch Undo would leave the user mid-state.
func TestModel_BatchUndoReversesAtomically(t *testing.T) {
	buf := make([]byte, 64)
	for i := range buf {
		buf[i] = byte(i)
	}
	m := FromBytes("", buf)
	original := append([]byte(nil), m.Bytes()...)

	patches := []Patch{
		{Offset: 0, Old: []byte{0}, New: []byte{0xAA}},
		{Offset: 10, Old: []byte{10}, New: []byte{0xBB}},
		{Offset: 20, Old: []byte{20}, New: []byte{0xCC}},
	}
	if err := m.ApplyBatch(patches); err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}
	// All three sites changed.
	if m.Bytes()[0] != 0xAA || m.Bytes()[10] != 0xBB || m.Bytes()[20] != 0xCC {
		t.Fatalf("ApplyBatch did not apply all sites: %x", m.Bytes()[:32])
	}

	// One Undo reverses the entire batch.
	if err := m.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if !bytes.Equal(m.Bytes(), original) {
		t.Fatalf("batched Undo did not reverse atomically:\n  got  %x\n  want %x",
			m.Bytes()[:32], original[:32])
	}
}
