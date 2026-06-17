// Package model is the studio in-memory representation of an
// in-focus container (.img or .fzf). It owns the container's byte
// representation, the undo and redo stacks, the dirty flag, and the
// current file path. Edits commit through Apply or ApplyBatch; Undo
// and Redo move between snapshots; Save writes to disk and clears
// both stacks.
package model

import (
	"bytes"
	"errors"
	"fmt"
	"os"
)

// Patch records a single byte-range mutation.
//
// Offset is the absolute byte offset into Model.bytes. Old is the
// expected pre-image at that offset (Apply rejects the patch if the
// current bytes don't match). New is the post-image; it must be the
// same length as Old.
type Patch struct {
	Offset int
	Old    []byte
	New    []byte
}

// Model holds the in-focus container and its history.
type Model struct {
	path  string
	bytes []byte
	undo  [][]Patch
	redo  [][]Patch
	dirty bool
}

// FromBytes wraps an existing byte slice as a Model with the given
// path. The model takes ownership of the slice; callers should not
// mutate the slice after handing it over.
func FromBytes(path string, data []byte) *Model {
	return &Model{path: path, bytes: data}
}

// Path returns the container's file path. Empty string for untitled.
func (m *Model) Path() string { return m.path }

// SetPath updates the file path (used by Save-As to rename an
// untitled container).
func (m *Model) SetPath(p string) { m.path = p }

// Bytes returns the current byte representation. The returned slice
// aliases the model's internal bytes; callers must not mutate it.
func (m *Model) Bytes() []byte { return m.bytes }

// Len returns the byte length of the container.
func (m *Model) Len() int { return len(m.bytes) }

// Dirty reports whether the container has unsaved edits.
func (m *Model) Dirty() bool { return m.dirty }

// CanUndo reports whether the undo stack has at least one entry.
func (m *Model) CanUndo() bool { return len(m.undo) > 0 }

// CanRedo reports whether the redo stack has at least one entry.
func (m *Model) CanRedo() bool { return len(m.redo) > 0 }

// Apply applies a single patch. Returns ErrPatchOutOfBounds when the
// patch's range exceeds the container, ErrPatchPreImageMismatch when
// Old does not match the current bytes, and ErrPatchLenMismatch when
// Old and New have different lengths.
func (m *Model) Apply(p Patch) error {
	return m.ApplyBatch([]Patch{p})
}

// ApplyBatch applies every patch in order atomically: either all
// apply and a single undo step is pushed, or none apply and the
// model is unchanged.
func (m *Model) ApplyBatch(ps []Patch) error {
	if len(ps) == 0 {
		return nil
	}
	if err := m.validateBatch(ps); err != nil {
		return err
	}
	for _, p := range ps {
		copy(m.bytes[p.Offset:p.Offset+len(p.New)], p.New)
	}
	m.undo = append(m.undo, ps)
	m.redo = nil
	m.dirty = true
	return nil
}

// Undo reverses the most recent batch on the undo stack and pushes it
// onto the redo stack. Returns ErrNothingToUndo when the stack is
// empty.
func (m *Model) Undo() error {
	if len(m.undo) == 0 {
		return ErrNothingToUndo
	}
	top := m.undo[len(m.undo)-1]
	m.undo = m.undo[:len(m.undo)-1]
	// Reverse the patches in reverse order: each patch's Old is written
	// back to where its New had landed.
	for i := len(top) - 1; i >= 0; i-- {
		p := top[i]
		copy(m.bytes[p.Offset:p.Offset+len(p.Old)], p.Old)
	}
	m.redo = append(m.redo, top)
	m.dirty = true
	return nil
}

// Redo re-applies the most recent batch on the redo stack and pushes
// it back onto the undo stack. Returns ErrNothingToRedo when the
// stack is empty.
func (m *Model) Redo() error {
	if len(m.redo) == 0 {
		return ErrNothingToRedo
	}
	top := m.redo[len(m.redo)-1]
	m.redo = m.redo[:len(m.redo)-1]
	for _, p := range top {
		copy(m.bytes[p.Offset:p.Offset+len(p.New)], p.New)
	}
	m.undo = append(m.undo, top)
	m.dirty = true
	return nil
}

// ClearHistory empties the undo/redo stacks and clears the dirty
// flag. Used by callers that wrote the buffer to disk via a path
// the Model itself doesn't know about (e.g., the App's .img-aware
// save path that has to splice m.bytes into a disk image first).
func (m *Model) ClearHistory() {
	m.undo = nil
	m.redo = nil
	m.dirty = false
}

// ClearRedo drops the redo stack without touching the undo stack or the
// dirty flag. Used when an in-place edit is cancelled by replaying Undo
// for each of its live steps: those reverted batches land on the redo
// stack, and a cancelled edit must not be re-applied by a later Redo.
func (m *Model) ClearRedo() { m.redo = nil }

// Replace swaps the model's bytes for an entirely new buffer. Used
// when an operation cannot be expressed as a fixed-size patch (e.g.,
// growing the container to append PCM for a newly assigned voice).
// Clears the undo and redo stacks because patch offsets no longer
// map onto the new buffer, and marks the model dirty.
func (m *Model) Replace(newBytes []byte) {
	m.bytes = newBytes
	m.undo = nil
	m.redo = nil
	m.dirty = true
}

// Save writes the current bytes to the given path atomically (writes
// to path+".tmp" then renames). On success clears both stacks and
// the dirty flag, and updates the model's path. Returns an error if
// the write fails.
func (m *Model) Save(path string) error {
	if path == "" {
		return errors.New("model: Save: path must not be empty")
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, m.bytes, 0o644); err != nil {
		return fmt.Errorf("model: writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("model: renaming %s to %s: %w", tmp, path, err)
	}
	m.path = path
	m.undo = nil
	m.redo = nil
	m.dirty = false
	return nil
}

// validateBatch checks every patch before any mutation lands.
func (m *Model) validateBatch(ps []Patch) error {
	for _, p := range ps {
		if len(p.Old) != len(p.New) {
			return fmt.Errorf("%w: offset %d, old %d, new %d",
				ErrPatchLenMismatch, p.Offset, len(p.Old), len(p.New))
		}
		end := p.Offset + len(p.Old)
		if p.Offset < 0 || end > len(m.bytes) {
			return fmt.Errorf("%w: offset %d, len %d, container %d",
				ErrPatchOutOfBounds, p.Offset, len(p.Old), len(m.bytes))
		}
		if !bytes.Equal(m.bytes[p.Offset:end], p.Old) {
			return fmt.Errorf("%w: offset %d", ErrPatchPreImageMismatch, p.Offset)
		}
	}
	return nil
}

// Sentinel errors returned by Apply / ApplyBatch / Undo / Redo.
var (
	ErrPatchOutOfBounds      = errors.New("model: patch out of bounds")
	ErrPatchPreImageMismatch = errors.New("model: patch pre-image mismatch")
	ErrPatchLenMismatch      = errors.New("model: patch old and new lengths differ")
	ErrNothingToUndo         = errors.New("model: nothing to undo")
	ErrNothingToRedo         = errors.New("model: nothing to redo")
)
