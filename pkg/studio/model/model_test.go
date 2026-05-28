package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
)

const (
	testVoiceAlpha = "ALPHA"
	testVoiceBravo = "BRAVO"
)

func newTestFZF(t *testing.T, names []string) string {
	t.Helper()
	_, p := fzfbuilder.MakeTestFZF(t, names)
	return p
}

func TestNewLoadsFZF(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha, testVoiceBravo})

	m, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if m.Path() != p {
		t.Errorf("Path = %q, want %q", m.Path(), p)
	}
	hdr := m.Header()
	if hdr == nil {
		t.Fatalf("Header is nil")
	}
	if hdr.NVoice != 2 {
		t.Errorf("NVoice = %d, want 2", hdr.NVoice)
	}
	if m.IsDirty() {
		t.Errorf("fresh model should not be dirty")
	}
	if got, want := len(m.Bytes()), 2*disk.SectorSize+disk.SectorSize; got < want {
		t.Errorf("Bytes length %d looks too short", got)
	}
}

func TestNewRejectsMissingFile(t *testing.T) {
	t.Parallel()
	_, err := New(filepath.Join(t.TempDir(), "nope.fzf"))
	if err == nil {
		t.Fatalf("New: want error for missing file")
	}
}

func TestApplyPatchMutatesBytesAndDirties(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha})
	m, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Bank 0 name: 12 bytes at BankNameOffset.
	patches, err := buildBankNamePatchAt(0, "NEW-BANK")
	if err != nil {
		t.Fatalf("buildBankNamePatchAt: %v", err)
	}
	for _, pp := range patches {
		if err := m.Apply(pp); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}
	if !m.IsDirty() {
		t.Errorf("model should be dirty after Apply")
	}
	if got := m.BankName(0); got != "NEW-BANK" {
		t.Errorf("BankName(0) = %q, want NEW-BANK", got)
	}
}

func TestApplyOutOfBoundsErrors(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha})
	m, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = m.Apply(voiceedit.Patch{Offset: 1 << 30, Size: 1, Value: 0})
	if err == nil {
		t.Errorf("Apply out-of-bounds: want error")
	}
}

func TestUndoRedoRoundtrip(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha})
	m, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	before := append([]byte(nil), m.Bytes()...)

	// Apply a single 1-byte patch we can verify by hand.
	off := disk.BankNameOffset
	origByte := before[off]
	newByte := origByte ^ 0x01
	if err := m.Apply(voiceedit.Patch{Offset: off, Size: 1, Value: uint16(newByte)}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if m.Bytes()[off] != newByte {
		t.Errorf("byte not applied: got %x want %x", m.Bytes()[off], newByte)
	}
	if !m.CanUndo() || m.CanRedo() {
		t.Errorf("CanUndo=%v CanRedo=%v, want true,false", m.CanUndo(), m.CanRedo())
	}

	if err := m.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if m.Bytes()[off] != origByte {
		t.Errorf("byte not restored: got %x want %x", m.Bytes()[off], origByte)
	}
	if m.CanUndo() || !m.CanRedo() {
		t.Errorf("after Undo CanUndo=%v CanRedo=%v, want false,true", m.CanUndo(), m.CanRedo())
	}
	if m.IsDirty() {
		t.Errorf("after Undo back to baseline, IsDirty should be false")
	}

	if err := m.Redo(); err != nil {
		t.Fatalf("Redo: %v", err)
	}
	if m.Bytes()[off] != newByte {
		t.Errorf("byte not re-applied: got %x want %x", m.Bytes()[off], newByte)
	}
	if !m.IsDirty() {
		t.Errorf("after Redo, IsDirty should be true")
	}
}

func TestApplyClearsRedoStack(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha})
	m, _ := New(p)
	off := disk.BankNameOffset
	_ = m.Apply(voiceedit.Patch{Offset: off, Size: 1, Value: 0xAA})
	_ = m.Undo()
	if !m.CanRedo() {
		t.Fatalf("CanRedo after Undo: want true")
	}
	_ = m.Apply(voiceedit.Patch{Offset: off, Size: 1, Value: 0xBB})
	if m.CanRedo() {
		t.Errorf("CanRedo after fresh Apply: want false")
	}
}

func TestSaveFZFClearsUndoAndPersists(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha})
	m, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	off := disk.BankNameOffset
	_ = m.Apply(voiceedit.Patch{Offset: off, Size: 1, Value: 'Z'})
	if !m.IsDirty() {
		t.Fatalf("IsDirty: want true before save")
	}

	if err := m.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if m.IsDirty() {
		t.Errorf("IsDirty: want false after save")
	}
	if m.CanUndo() {
		t.Errorf("undo stack should be cleared after save")
	}

	on, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if on[off] != 'Z' {
		t.Errorf("on-disk byte not persisted: got 0x%02x want 'Z'", on[off])
	}
}

func TestSubscribeFiresOnApply(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha})
	m, _ := New(p)

	var calls int
	unsub := m.Subscribe(func() { calls++ })
	defer unsub()

	off := disk.BankNameOffset
	_ = m.Apply(voiceedit.Patch{Offset: off, Size: 1, Value: 'X'})
	if calls != 1 {
		t.Errorf("Subscribe: got %d calls after Apply, want 1", calls)
	}
	_ = m.Undo()
	if calls != 2 {
		t.Errorf("Subscribe: got %d calls after Undo, want 2", calls)
	}
	unsub()
	_ = m.Apply(voiceedit.Patch{Offset: off, Size: 1, Value: 'Y'})
	if calls != 2 {
		t.Errorf("Subscribe: got %d calls after unsubscribe, want still 2", calls)
	}
}

func TestSetBankName(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha})
	m, _ := New(p)
	if err := m.SetBankName(0, "MYBANK"); err != nil {
		t.Fatalf("SetBankName: %v", err)
	}
	got := m.BankName(0)
	if got != "MYBANK" {
		t.Errorf("BankName(0) = %q, want MYBANK", got)
	}
	// Undo restores the previous name.
	prev := m.BankName(0)
	_ = m.Undo()
	if m.BankName(0) == prev {
		t.Errorf("Undo did not restore bank name")
	}
}

func TestSetBankNameRejectsTooLong(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha})
	m, _ := New(p)
	tooLong := strings.Repeat("A", disk.LabelSize+1)
	if err := m.SetBankName(0, tooLong); err == nil {
		t.Errorf("SetBankName(%q) want error, got nil", tooLong)
	}
}

func TestVoiceAccessor(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha, testVoiceBravo})
	m, _ := New(p)
	v, err := m.Voice(1)
	if err != nil {
		t.Fatalf("Voice(1): %v", err)
	}
	if v.Name != testVoiceBravo {
		t.Errorf("Voice(1).Name = %q, want BRAVO", v.Name)
	}
}

func TestVoiceAccessorOutOfRange(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha})
	m, _ := New(p)
	if _, err := m.Voice(99); err == nil {
		t.Errorf("Voice(99): want error")
	}
	if _, err := m.Voice(-1); err == nil {
		t.Errorf("Voice(-1): want error")
	}
}

// buildBankNamePatchAt returns a patch that writes the 12-byte padded name
// into the bank-name field of bankIdx. Mirrors what SetBankName builds
// internally; used to drive Apply tests without depending on the method.
func buildBankNamePatchAt(bankIdx int, name string) ([]voiceedit.Patch, error) {
	if len(name) > disk.LabelSize {
		return nil, errLongName
	}
	padded := disk.PadLabel(strings.ToUpper(name))
	off := bankIdx*disk.SectorSize + disk.BankNameOffset
	return []voiceedit.Patch{{Offset: off, Bytes: padded[:]}}, nil
}

var errLongName = &nameError{}

type nameError struct{}

func (e *nameError) Error() string { return "name too long" }
