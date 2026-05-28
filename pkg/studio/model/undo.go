package model

import "errors"

// opRecord captures one byte-range write's inverse: the absolute FZF
// offset, the pre-image bytes that were overwritten there, and the
// post-image bytes the patch wrote. preImage drives Undo (restore-state);
// postImage drives Redo (re-apply-state) without re-running the patch
// encoder.
type opRecord struct {
	offset    int
	preImage  []byte
	postImage []byte
}

// undoRecord is one logical undo step. Single-patch edits (Apply) push a
// record with one op. Batched edits (ApplyBatch / ApplyVoicePatch with
// key-range fan-out) push a record with multiple ops; Undo replays them
// in reverse order so the unwind is symmetric.
type undoRecord struct {
	ops []opRecord
}

// CanUndo reports whether the undo stack is non-empty.
func (m *Model) CanUndo() bool { return len(m.undo) > 0 }

// CanRedo reports whether the redo stack is non-empty.
func (m *Model) CanRedo() bool { return len(m.redo) > 0 }

// IsDirty reports whether the in-memory state diverges from the on-disk
// state, computed from the undo stack's save-index position. Save sets
// saveIndex to len(undo) (i.e. 0 after clearing), so a freshly-saved
// model with no further edits is not dirty.
func (m *Model) IsDirty() bool {
	return m.saveIndex != len(m.undo)
}

// Undo reverses the most recent edit step (which may have been one patch
// or a batched set), pushing the inverse onto the redo stack. Returns an
// error if the undo stack is empty.
func (m *Model) Undo() error {
	if len(m.undo) == 0 {
		return errors.New("model: undo stack empty")
	}
	rec := m.undo[len(m.undo)-1]
	m.undo = m.undo[:len(m.undo)-1]
	// Replay in reverse so overlapping writes unwind cleanly.
	for i := len(rec.ops) - 1; i >= 0; i-- {
		op := rec.ops[i]
		copy(m.bytes[op.offset:op.offset+len(op.preImage)], op.preImage)
	}
	m.redo = append(m.redo, rec)
	m.notify()
	return nil
}

// Redo re-applies the most recently undone step.
func (m *Model) Redo() error {
	if len(m.redo) == 0 {
		return errors.New("model: redo stack empty")
	}
	rec := m.redo[len(m.redo)-1]
	m.redo = m.redo[:len(m.redo)-1]
	for _, op := range rec.ops {
		copy(m.bytes[op.offset:op.offset+len(op.postImage)], op.postImage)
	}
	m.undo = append(m.undo, rec)
	m.notify()
	return nil
}
