package model

// Widget-contract integration tests.
//
// These tests exercise the editing flows the widget layer performs.
// They define what "correct" looks like at the model API boundary,
// so the widget code has a concrete spec to bind against.

import (
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
)

// TestVoiceNameEditViaApply demonstrates the Voice Details widget's name
// edit flow:
//  1. Widget builds patches via voiceedit.BuildNamePatch (voice-header-
//     relative offsets).
//  2. Widget translates each patch to FZF-absolute offset via
//     disk.VoiceSlotOffset(headerVoiceAreaStart, slot).
//  3. Widget calls m.Apply once per patch.
//  4. Widget reads the new name back via m.Voice(slot).Name.
//
// This works today but is leaky: the widget has to know about voice-
// area layout. A higher-level Model.ApplyVoicePatch(slot, patch)
// method would hide that detail. See gap note in the test summary.
func TestVoiceNameEditViaApply(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha, testVoiceBravo})
	m, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	patches, err := voiceedit.BuildNamePatch("RENAMED")
	if err != nil {
		t.Fatalf("BuildNamePatch: %v", err)
	}

	const slot = 0
	voiceOffset := disk.VoiceSlotOffset(m.Header().VoiceAreaStart, slot)
	for _, vp := range patches {
		// Translate voice-header-relative -> FZF-absolute.
		abs := vp
		abs.Offset = voiceOffset + vp.Offset
		if err := m.Apply(abs); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}

	v, err := m.Voice(slot)
	if err != nil {
		t.Fatalf("Voice: %v", err)
	}
	if v.Name != "RENAMED" {
		t.Errorf("voice name = %q, want %q", v.Name, "RENAMED")
	}
	if !m.IsDirty() {
		t.Errorf("model should be dirty after edit")
	}
}

// TestVoiceKeyRangeEditSyncsBankSites is the F14 / round-3 invariant
// applied to v2. When a voice's hwid/lwid/cent bytes change, every bank
// site that references the voice via vp[] must also have its
// hwid/lwid/cent arrays updated, or hardware playback ignores the edit
// (the bank sector is what the firmware consults when loading a bank).
//
// The widget calls m.ApplyVoicePatch(slot, patch), which translates
// the voice-header-relative offset to FZF-absolute AND fans out
// key-range patches to every bank site. All resulting byte writes
// land as a single undo step (see TestUndoRevertsKeyRangeAtomically).
func TestVoiceKeyRangeEditSyncsBankSites(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha, testVoiceBravo})
	m, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const slot = 0
	// 40 is deliberately chosen to differ from the test fixture's default
	// (FirstMIDINote = 36 for slot 0). Using a value that matches the
	// fixture would silently pass even if no sync happened.
	const newKeyLow uint8 = 40

	patch := voiceedit.Patch{
		Offset: disk.VoiceKeyLowOffset, // voice-header-relative
		Size:   1,
		Value:  uint16(newKeyLow),
	}
	if err := m.ApplyVoicePatch(slot, patch); err != nil {
		t.Fatalf("ApplyVoicePatch: %v", err)
	}

	voiceOffset := disk.VoiceSlotOffset(m.Header().VoiceAreaStart, slot)
	if got := m.Bytes()[voiceOffset+disk.VoiceKeyLowOffset]; got != newKeyLow {
		t.Errorf("voice-header key-low byte = %d, want %d", got, newKeyLow)
	}

	sites := fzutil.FindBankSitesForVoice(m.Bytes(), m.Header(), slot)
	if len(sites) == 0 {
		t.Fatalf("expected at least one bank site for slot %d, got 0 (test fixture issue)", slot)
	}
	for _, site := range sites {
		bank := fzutil.BankSliceAt(m.Bytes(), site.BankIdx)
		if bank == nil {
			t.Fatalf("BankSliceAt(%d) returned nil", site.BankIdx)
		}
		got := bank[disk.BankKeyLowOffset+site.SplitIdx]
		if got != newKeyLow {
			t.Errorf("bank %d site %d key-low = %d, want %d (F14 invariant violated)",
				site.BankIdx, site.SplitIdx, got, newKeyLow)
		}
	}
}

// TestUndoRevertsKeyRangeAtomically verifies that the multi-byte
// fan-out performed by ApplyVoicePatch for a key-range edit collapses
// into a single undo step: one Ctrl+Z reverts the voice-header byte
// AND every synced bank-site byte. Without this, undoing a key-range
// edit would unwind one bank site at a time (bizarre UX) or worse
// leave the voice and bank arrays inconsistent.
func TestUndoRevertsKeyRangeAtomically(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha, testVoiceBravo})
	m, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const slot = 0
	pristine := append([]byte(nil), m.Bytes()...)

	patch := voiceedit.Patch{
		Offset: disk.VoiceKeyLowOffset,
		Size:   1,
		Value:  42,
	}
	if err := m.ApplyVoicePatch(slot, patch); err != nil {
		t.Fatalf("ApplyVoicePatch: %v", err)
	}

	// One undo step should suffice regardless of how many bank sites
	// got fanned out to.
	if err := m.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if m.CanUndo() {
		t.Errorf("undo stack should be empty after one Undo of the batched edit")
	}
	if m.IsDirty() {
		t.Errorf("model should not be dirty after undoing to baseline")
	}

	// Spot-check: every byte in the voice header and every bank
	// sector should match pristine.
	for i := 0; i < len(pristine); i++ {
		if m.Bytes()[i] != pristine[i] {
			t.Fatalf("byte %d differs from pristine after undo: got 0x%02x, want 0x%02x", i, m.Bytes()[i], pristine[i])
		}
	}
}

// TestBankAreaVolumeEdit is the bvol edit flow for the Bank tab. The
// widget writes one byte at the bank-sector offset for the area's
// bvol[] entry. No bank-site fan-out (bvol is per-bank, not per-voice).
//
// Should pass on the current foundation.
func TestBankAreaVolumeEdit(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha, testVoiceBravo})
	m, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const bankIdx = 0
	const areaIdx = 0
	const newVol uint8 = 17

	patch := voiceedit.Patch{
		Offset: bankIdx*disk.SectorSize + disk.BankVolumeOffset + areaIdx,
		Size:   1,
		Value:  uint16(newVol),
	}
	if err := m.Apply(patch); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	bank := fzutil.BankSliceAt(m.Bytes(), bankIdx)
	if bank == nil {
		t.Fatalf("BankSliceAt(0) returned nil")
	}
	if got := bank[disk.BankVolumeOffset+areaIdx]; got != newVol {
		t.Errorf("bank-area bvol = %d, want %d", got, newVol)
	}
}

// TestBankRename exercises Model.SetBankName, the only structural
// operation the studio exposes.
func TestBankRename(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha, testVoiceBravo})
	m, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := m.SetBankName(0, "MYBANK"); err != nil {
		t.Fatalf("SetBankName: %v", err)
	}
	got := m.BankName(0)
	want := "MYBANK"
	if got != want {
		t.Errorf("BankName = %q, want %q", got, want)
	}
	if !m.IsDirty() {
		t.Errorf("model should be dirty after rename")
	}
}

// TestUndoRevertsMultipleEdits verifies that undo unwinds each edit
// one at a time, in reverse order, back to the pristine state. This
// is the spec §3.3 "granularity is one commit" invariant.
//
// Should pass on the current foundation.
func TestUndoRevertsMultipleEdits(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha, testVoiceBravo})
	m, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pristine := append([]byte(nil), m.Bytes()...)

	// Three independent edits.
	if err := m.Apply(voiceedit.Patch{Offset: 0, Size: 1, Value: 0xAA}); err != nil {
		t.Fatalf("Apply 1: %v", err)
	}
	if err := m.Apply(voiceedit.Patch{Offset: 1, Size: 1, Value: 0xBB}); err != nil {
		t.Fatalf("Apply 2: %v", err)
	}
	if err := m.Apply(voiceedit.Patch{Offset: 2, Size: 1, Value: 0xCC}); err != nil {
		t.Fatalf("Apply 3: %v", err)
	}

	for i := 0; i < 3; i++ {
		if !m.CanUndo() {
			t.Fatalf("CanUndo false at i=%d", i)
		}
		if err := m.Undo(); err != nil {
			t.Fatalf("Undo at i=%d: %v", i, err)
		}
	}

	if m.IsDirty() {
		t.Errorf("model should not be dirty after undoing to baseline")
	}
	for i, want := range pristine[:8] {
		if got := m.Bytes()[i]; got != want {
			t.Errorf("byte %d: got 0x%02x, want 0x%02x", i, got, want)
		}
	}
}

// TestSaveClearsUndoAndDirty: after save, the undo stack is cleared and
// the model is no longer dirty.
//
// Should pass on the current foundation (already covered by
// TestSaveFZFClearsUndoAndPersists in model_test.go, but reproduced
// here as part of the widget-contract spec).
func TestSaveClearsUndoAndDirty(t *testing.T) {
	t.Parallel()
	p := newTestFZF(t, []string{testVoiceAlpha, testVoiceBravo})
	m, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := m.Apply(voiceedit.Patch{Offset: 0, Size: 1, Value: 0xAA}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !m.IsDirty() {
		t.Fatalf("expected dirty after edit")
	}
	if err := m.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if m.IsDirty() {
		t.Errorf("dirty after Save")
	}
	if m.CanUndo() {
		t.Errorf("CanUndo true after Save (undo stack should be cleared)")
	}
}
