package model

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestApplySingleByteEdit(t *testing.T) {
	m := FromBytes("", []byte{0x00, 0x01, 0x02, 0x03})
	err := m.Apply(Patch{Offset: 1, Old: []byte{0x01}, New: []byte{0xAA}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []byte{0x00, 0xAA, 0x02, 0x03}
	if !bytes.Equal(m.Bytes(), want) {
		t.Errorf("Bytes() = %v, want %v", m.Bytes(), want)
	}
	if !m.Dirty() {
		t.Errorf("Dirty() = false, want true")
	}
	if !m.CanUndo() || m.CanRedo() {
		t.Errorf("CanUndo=%v CanRedo=%v, want true,false", m.CanUndo(), m.CanRedo())
	}
}

func TestApplyRejectsPreImageMismatch(t *testing.T) {
	m := FromBytes("", []byte{0x00, 0x01, 0x02})
	err := m.Apply(Patch{Offset: 1, Old: []byte{0xFF}, New: []byte{0xAA}})
	if !errors.Is(err, ErrPatchPreImageMismatch) {
		t.Errorf("Apply error = %v, want ErrPatchPreImageMismatch", err)
	}
	if m.Dirty() {
		t.Errorf("Dirty() = true after rejected patch, want false")
	}
}

func TestApplyRejectsOutOfBounds(t *testing.T) {
	m := FromBytes("", []byte{0x00, 0x01, 0x02})
	err := m.Apply(Patch{Offset: 5, Old: []byte{0x00}, New: []byte{0xAA}})
	if !errors.Is(err, ErrPatchOutOfBounds) {
		t.Errorf("Apply error = %v, want ErrPatchOutOfBounds", err)
	}
}

func TestApplyRejectsLenMismatch(t *testing.T) {
	m := FromBytes("", []byte{0x00, 0x01, 0x02})
	err := m.Apply(Patch{Offset: 0, Old: []byte{0x00}, New: []byte{0xAA, 0xBB}})
	if !errors.Is(err, ErrPatchLenMismatch) {
		t.Errorf("Apply error = %v, want ErrPatchLenMismatch", err)
	}
}

func TestApplyBatchAtomicity(t *testing.T) {
	m := FromBytes("", []byte{0x00, 0x01, 0x02, 0x03})
	// Second patch is invalid (bad pre-image). Whole batch must fail
	// without mutating the model.
	err := m.ApplyBatch([]Patch{
		{Offset: 0, Old: []byte{0x00}, New: []byte{0xAA}},
		{Offset: 2, Old: []byte{0xFF}, New: []byte{0xBB}},
	})
	if !errors.Is(err, ErrPatchPreImageMismatch) {
		t.Errorf("ApplyBatch error = %v, want ErrPatchPreImageMismatch", err)
	}
	want := []byte{0x00, 0x01, 0x02, 0x03}
	if !bytes.Equal(m.Bytes(), want) {
		t.Errorf("Bytes() = %v, want unchanged %v", m.Bytes(), want)
	}
}

func TestUndoRedoRoundtrip(t *testing.T) {
	m := FromBytes("", []byte{0x00, 0x01, 0x02})
	if err := m.Apply(Patch{Offset: 0, Old: []byte{0x00}, New: []byte{0xAA}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := m.Apply(Patch{Offset: 1, Old: []byte{0x01}, New: []byte{0xBB}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	wantAfterApply := []byte{0xAA, 0xBB, 0x02}
	if !bytes.Equal(m.Bytes(), wantAfterApply) {
		t.Fatalf("after two applies = %v, want %v", m.Bytes(), wantAfterApply)
	}
	if err := m.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	wantAfterUndo := []byte{0xAA, 0x01, 0x02}
	if !bytes.Equal(m.Bytes(), wantAfterUndo) {
		t.Fatalf("after undo = %v, want %v", m.Bytes(), wantAfterUndo)
	}
	if err := m.Redo(); err != nil {
		t.Fatalf("Redo: %v", err)
	}
	if !bytes.Equal(m.Bytes(), wantAfterApply) {
		t.Fatalf("after redo = %v, want %v", m.Bytes(), wantAfterApply)
	}
}

func TestApplyClearsRedo(t *testing.T) {
	m := FromBytes("", []byte{0x00, 0x01})
	_ = m.Apply(Patch{Offset: 0, Old: []byte{0x00}, New: []byte{0xAA}})
	_ = m.Undo()
	if !m.CanRedo() {
		t.Fatalf("CanRedo = false after Undo, want true")
	}
	_ = m.Apply(Patch{Offset: 1, Old: []byte{0x01}, New: []byte{0xBB}})
	if m.CanRedo() {
		t.Errorf("CanRedo = true after new Apply, want false")
	}
}

func TestBatchedUndoIsSingleStep(t *testing.T) {
	m := FromBytes("", []byte{0x00, 0x01, 0x02})
	err := m.ApplyBatch([]Patch{
		{Offset: 0, Old: []byte{0x00}, New: []byte{0xAA}},
		{Offset: 1, Old: []byte{0x01}, New: []byte{0xBB}},
	})
	if err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}
	if err := m.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if m.CanUndo() {
		t.Errorf("CanUndo = true after single Undo of batch, want false")
	}
	want := []byte{0x00, 0x01, 0x02}
	if !bytes.Equal(m.Bytes(), want) {
		t.Errorf("after batch+undo = %v, want %v", m.Bytes(), want)
	}
}

func TestSaveClearsStacksAndDirty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.img")
	m := FromBytes(path, []byte{0x00, 0x01, 0x02})
	_ = m.Apply(Patch{Offset: 0, Old: []byte{0x00}, New: []byte{0xAA}})
	if !m.Dirty() {
		t.Fatalf("expected dirty before save")
	}
	if err := m.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if m.Dirty() || m.CanUndo() || m.CanRedo() {
		t.Errorf("after save: dirty=%v canUndo=%v canRedo=%v, want all false",
			m.Dirty(), m.CanUndo(), m.CanRedo())
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := []byte{0xAA, 0x01, 0x02}
	if !bytes.Equal(written, want) {
		t.Errorf("file = %v, want %v", written, want)
	}
}

func TestSaveAsUpdatesPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.img")
	m := FromBytes("", []byte{0x00, 0x01, 0x02})
	if got := m.Path(); got != "" {
		t.Fatalf("Path() = %q, want empty", got)
	}
	if err := m.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := m.Path(); got != path {
		t.Errorf("Path() after Save = %q, want %q", got, path)
	}
}

func TestUndoOnEmptyStackIsError(t *testing.T) {
	m := FromBytes("", []byte{0x00})
	if err := m.Undo(); !errors.Is(err, ErrNothingToUndo) {
		t.Errorf("Undo on empty stack = %v, want ErrNothingToUndo", err)
	}
}

func TestRedoOnEmptyStackIsError(t *testing.T) {
	m := FromBytes("", []byte{0x00})
	if err := m.Redo(); !errors.Is(err, ErrNothingToRedo) {
		t.Errorf("Redo on empty stack = %v, want ErrNothingToRedo", err)
	}
}
