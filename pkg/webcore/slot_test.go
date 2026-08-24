package webcore

import (
	"bytes"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
)

// unpackSlot extracts one voice slot from the session's dump as a
// standalone .fzv, through the CLI's own unpack path.
func unpackSlot(t *testing.T, s *Session, slot int) []byte {
	t.Helper()
	voices, slots, err := voiceunpack.UnpackDataFromBytes(dumpBytes(t, s))
	if err != nil {
		t.Fatalf("UnpackDataFromBytes: %v", err)
	}
	for i, sl := range slots {
		if sl == slot {
			return voices[i]
		}
	}
	t.Fatalf("slot %d not found in the unpacked dump", slot)
	return nil
}

// slotFileParity applies a slot op and the equivalent loose-file patch
// list, then pins the unpacked slot byte for byte against the file
// result. Unpack rewrites wave pointers identically on both sides, so
// any divergence is a real semantic difference.
func slotFileParity(t *testing.T, s *Session, slot int, doSlot func() *Error, filePatches func(fzv []byte) []voiceedit.Edit) {
	t.Helper()
	want := unpackSlot(t, s, slot)
	if err := voiceedit.ApplyToFZVBytes(want, filePatches(want)); err != nil {
		t.Fatalf("ApplyToFZVBytes: %v", err)
	}
	if cerr := doSlot(); cerr != nil {
		t.Fatalf("slot op: %v", cerr)
	}
	got := unpackSlot(t, s, slot)
	if !bytes.Equal(got, want) {
		t.Error("slot edit differs from the loose-file edit on the unpacked voice")
	}
}

func TestSlotParamNumberMatchesFileEdit(t *testing.T) {
	s := twoVoiceSession(t)
	slotFileParity(t, s, 1,
		func() *Error { _, cerr := s.SetSlotParamNumber(1, fieldCutoff, 90); return cerr },
		func(fzv []byte) []voiceedit.Edit {
			patches, err := numberPatches(fieldCutoff, 90, fzv)
			if err != nil {
				t.Fatal(err)
			}
			return patches
		})
}

func TestSlotParamOptionMatchesFileEdit(t *testing.T) {
	s := twoVoiceSession(t)
	slotFileParity(t, s, 0,
		func() *Error { _, cerr := s.SetSlotParamOption(0, fieldPlaybackMode, "reverse"); return cerr },
		func(fzv []byte) []voiceedit.Edit {
			patches, err := optionPatches(fieldPlaybackMode, "reverse", fzv)
			if err != nil {
				t.Fatal(err)
			}
			return patches
		})
}

// A slot key-range edit lands in the voice header and fans out to
// every referencing bank site, so the hardware actually hears it.
func TestSlotKeyRangeFansOutToBanks(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.SetSlotParamNumber(0, fieldKeyLow, 40); cerr != nil {
		t.Fatalf("SetSlotParamNumber: %v", cerr)
	}

	// The bank byte moved: the area snapshot reads bank bytes.
	area := instrument(t, s).Banks[0].Areas[0]
	if area.KeyLow != 40 {
		t.Errorf("area keyLow = %d, want 40 (bank fan-out)", area.KeyLow)
	}

	// The voice header moved too.
	vp, err := fzvinfo.ParseBytes(unpackSlot(t, s, 0))
	if err != nil {
		t.Fatal(err)
	}
	if vp.KeyLow != 40 {
		t.Errorf("voice header keyLow = %d, want 40", vp.KeyLow)
	}
}

// Loop cells inside a dump hold absolute addresses; the boundary
// speaks voice-relative frames. Unpack rebases to standalone form, so
// reading 100..900 back proves the write rebased correctly.
func TestSlotLoopRebasesAbsoluteAddresses(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.SetSlotLoop(1, 0, 100, 900); cerr != nil {
		t.Fatalf("SetSlotLoop: %v", cerr)
	}
	vp, err := fzvinfo.ParseBytes(unpackSlot(t, s, 1))
	if err != nil {
		t.Fatal(err)
	}
	if vp.AllLoops[0].Start != 100 || vp.AllLoops[0].End != 900 {
		t.Errorf("loop 0 = %d..%d, want 100..900", vp.AllLoops[0].Start, vp.AllLoops[0].End)
	}

	// Out-of-range frames clamp to the slot's own frame count, not the
	// dump's audio area.
	if _, cerr := s.SetSlotLoop(1, 1, 0, 10_000_000); cerr != nil {
		t.Fatalf("SetSlotLoop clamp: %v", cerr)
	}
	vp, err = fzvinfo.ParseBytes(unpackSlot(t, s, 1))
	if err != nil {
		t.Fatal(err)
	}
	if int(vp.AllLoops[1].End) != int(vp.Samples) {
		t.Errorf("clamped loop end = %d, want the slot frame count %d", vp.AllLoops[1].End, vp.Samples)
	}
}

// The rate is fixed when a sample is taken. The FZ panel offers no way
// to change a loaded voice's rate, so neither does fizzle. The value
// still reads out through VoiceDetail; it just isn't editable.
func TestSampleRateIsNotAnEditableSchemaField(t *testing.T) {
	if _, ok := schemaField(fieldSampleRate); ok {
		t.Fatal("the schema still carries a sample rate field")
	}

	s := twoVoiceSession(t)
	before := mustExport(t, s)
	if _, cerr := s.SetSlotParamOption(0, fieldSampleRate, "18000"); cerr == nil {
		t.Fatal("expected a refusal for a field the schema no longer carries")
	}
	if !bytes.Equal(mustExport(t, s), before) {
		t.Error("the refused rate changed the image")
	}

	// The readout survives even though the control is gone.
	if got := instrument(t, s).Voices[0].Voice.SampleRate; got == 0 {
		t.Error("the voice detail no longer reports a sample rate")
	}
}

// R14's generation start and end. The cells hold absolute sample
// addresses into the shared audio area, so the setter rebases the way
// SetSlotLoop does, and clamps to the slot's own frame count rather
// than to a schema range no static declaration could carry.
func TestSlotGenerationRebasesAndClamps(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.SetSlotGeneration(1, 100, 900); cerr != nil {
		t.Fatalf("SetSlotGeneration: %v", cerr)
	}
	vp, err := fzvinfo.ParseBytes(unpackSlot(t, s, 1))
	if err != nil {
		t.Fatal(err)
	}
	if vp.GenStart != 100 || vp.GenEnd != 900 {
		t.Errorf("generation = %d..%d, want 100..900", vp.GenStart, vp.GenEnd)
	}
	detail := instrument(t, s).Voices[1].Voice
	if detail.GenStart != 100 || detail.GenEnd != 900 {
		t.Errorf("snapshot generation = %d..%d, want 100..900", detail.GenStart, detail.GenEnd)
	}

	// Clamped to the slot's frames, not to the dump's audio area: a
	// gened past waved would play another voice's samples.
	if _, cerr := s.SetSlotGeneration(1, -50, 10_000_000); cerr != nil {
		t.Fatalf("SetSlotGeneration clamp: %v", cerr)
	}
	vp, err = fzvinfo.ParseBytes(unpackSlot(t, s, 1))
	if err != nil {
		t.Fatal(err)
	}
	if vp.GenStart != 0 || vp.GenEnd != vp.Samples {
		t.Errorf("clamped generation = %d..%d, want 0..%d", vp.GenStart, vp.GenEnd, vp.Samples)
	}

	// An end below the start clamps up rather than inverting the pair.
	if _, cerr := s.SetSlotGeneration(1, 500, 10); cerr != nil {
		t.Fatalf("SetSlotGeneration inverted: %v", cerr)
	}
	vp, err = fzvinfo.ParseBytes(unpackSlot(t, s, 1))
	if err != nil {
		t.Fatal(err)
	}
	if vp.GenStart != 500 || vp.GenEnd < vp.GenStart {
		t.Errorf("generation = %d..%d, want a start of 500 and an end no lower", vp.GenStart, vp.GenEnd)
	}

	if _, cerr := s.SetSlotGeneration(9, 0, 10); cerr == nil || cerr.Code != codeInvalidValue {
		t.Fatalf("expected invalid-value for a bad slot, got %v", cerr)
	}
}

func TestSlotLoopAttrAndSelect(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.SetSlotLoopAttr(0, 2, 512, 700); cerr != nil {
		t.Fatalf("SetSlotLoopAttr: %v", cerr)
	}
	if _, cerr := s.SetSlotLoopSelect(0, 2, 8); cerr != nil {
		t.Fatalf("SetSlotLoopSelect: %v", cerr)
	}
	vp, err := fzvinfo.ParseBytes(unpackSlot(t, s, 0))
	if err != nil {
		t.Fatal(err)
	}
	if vp.AllLoops[2].XF != 512 || vp.AllLoops[2].Tm != 700 {
		t.Errorf("loop 2 attrs = XF %d Tm %d, want 512 700", vp.AllLoops[2].XF, vp.AllLoops[2].Tm)
	}
	if int(vp.LoopSustain) != 2 || int(vp.LoopRelease) != disk.NoSustainLoop {
		t.Errorf("loop select = %d/%d, want 2/%d", vp.LoopSustain, vp.LoopRelease, disk.NoSustainLoop)
	}
}

func TestSlotEnvelopeMatchesFileEdit(t *testing.T) {
	s := twoVoiceSession(t)
	rates := []int{10, 20, 30, 40, 50, 60, 70, 80}
	stops := []int{99, 90, 80, 70, 60, 50, 40, 30}
	slotFileParity(t, s, 0,
		func() *Error { _, cerr := s.SetSlotEnvelope(0, "dca", 3, 6, rates, stops); return cerr },
		func(fzv []byte) []voiceedit.Edit {
			vp, err := fzvinfo.ParseBytes(fzv)
			if err != nil {
				t.Fatal(err)
			}
			var r, st [disk.EnvelopeStages]int
			copy(r[:], rates)
			copy(st[:], stops)
			patches, err := voiceedit.BuildDCAPatches(3, 6, r, st, vp.DCARates)
			if err != nil {
				t.Fatal(err)
			}
			return patches
		})
}

func TestRenameVoiceSlot(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.RenameVoiceSlot(1, "NEW NAME"); cerr != nil {
		t.Fatalf("RenameVoiceSlot: %v", cerr)
	}
	inst := instrument(t, s)
	if inst.Voices[1].Name != "NEW NAME" {
		t.Errorf("voice 1 name = %q, want NEW NAME", inst.Voices[1].Name)
	}

	if _, cerr := s.RenameVoiceSlot(1, ""); cerr == nil || cerr.Code != codeInvalidValue || cerr.Message != "voice name must be 1 to 12 characters" {
		t.Fatalf("expected invalid-value for an empty name, got %v", cerr)
	}
	if _, cerr := s.RenameVoiceSlot(1, "WAY TOO LONG NAME"); cerr == nil || cerr.Message != "voice name must be 1 to 12 characters" {
		t.Fatalf("expected stable length error for a long name, got %v", cerr)
	}
	if _, cerr := s.RenameVoiceSlot(1, "BAD☃"); cerr == nil || cerr.Message != "voice name contains non-ASCII character \"☃\"" {
		t.Fatalf("expected character-specific ASCII error, got %v", cerr)
	}
	if _, cerr := s.RenameVoiceSlot(99, "NAME"); cerr == nil || cerr.Message != "voice slot 99 out of range" {
		t.Fatalf("expected boundary slot error, got %v", cerr)
	}
}

func TestSlotOpsBoundsAndUndo(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.SetSlotParamNumber(9, fieldCutoff, 50); cerr == nil || cerr.Code != codeInvalidValue {
		t.Fatalf("expected invalid-value for a bad slot, got %v", cerr)
	}

	before := mustExport(t, s)
	if _, cerr := s.SetSlotParamNumber(0, fieldCutoff, 41); cerr != nil {
		t.Fatal(cerr)
	}
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	if !bytes.Equal(mustExport(t, s), before) {
		t.Error("undo did not restore the pre-edit image")
	}
}

// Slot edits work on a split document: the dump stitches, patches, and
// re-splits without disturbing the pair.
func TestSlotEditOnSplitDocument(t *testing.T) {
	s, _ := splitSession(t)
	if _, cerr := s.SetSlotParamNumber(2, fieldCutoff, 33); cerr != nil {
		t.Fatalf("SetSlotParamNumber on split doc: %v", cerr)
	}
	if got := s.Snapshot().Disk.Disks; got != 2 {
		t.Fatalf("disks = %d, want 2 after a slot edit", got)
	}
	vp, err := fzvinfo.ParseBytes(unpackSlotSplit(t, s, 2))
	if err != nil {
		t.Fatal(err)
	}
	if int(vp.FilterCutoff) != 33 {
		t.Errorf("cutoff = %d, want 33", vp.FilterCutoff)
	}
}

// unpackSlotSplit stitches the pair before unpacking, mirroring the
// CLI's multi-disk unpack.
func unpackSlotSplit(t *testing.T, s *Session, slot int) []byte {
	t.Helper()
	disk1, cerr := s.ExportImageAt(0)
	if cerr != nil {
		t.Fatal(cerr)
	}
	disk2, cerr := s.ExportImageAt(1)
	if cerr != nil {
		t.Fatal(cerr)
	}
	stitched := append(payload(t, disk1), payload(t, disk2)...)
	voices, slots, err := voiceunpack.UnpackDataFromBytes(stitched)
	if err != nil {
		t.Fatalf("UnpackDataFromBytes: %v", err)
	}
	for i, sl := range slots {
		if sl == slot {
			return voices[i]
		}
	}
	t.Fatalf("slot %d not found in the stitched dump", slot)
	return nil
}
