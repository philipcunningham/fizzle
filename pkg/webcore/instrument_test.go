package webcore

import (
	"bytes"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/fzfeffects"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
)

// Names of the two fixture voices used across the area tests.
const (
	voiceLow  = "LOW"
	voiceHigh = "HIGH"
)

// assembledSession builds a real instrument (a full dump assembled by
// the CLI's own builder) on a fresh disk and opens it.
func assembledSession(t *testing.T, names []string, groups []voicebuild.Keygroup) *Session {
	t.Helper()
	voices := make([][]byte, len(names))
	for i, name := range names {
		samples := make([]int16, 2000+i*100)
		for j := range samples {
			samples[j] = int16(j % 157)
		}
		voices[i] = voiceimport.Encode(samples, 1, name, 0, voiceimport.NoLoop())
	}
	fzf, err := voicebuild.AssembleWithKeygroups(voices, groups)
	if err != nil {
		t.Fatalf("AssembleWithKeygroups: %v", err)
	}
	blank, err := diskformat.BuildImage("INST")
	if err != nil {
		t.Fatalf("BuildImage: %v", err)
	}
	img, err := disk.ReadImage(bytes.NewReader(blank))
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	if err := diskadd.AddToImage(img, fzf, 0); err != nil {
		t.Fatalf("AddToImage: %v", err)
	}
	s := NewSession()
	if _, cerr := s.OpenImage(img.Bytes()); cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	return s
}

func twoVoiceSession(t *testing.T) *Session {
	t.Helper()
	return assembledSession(t, []string{voiceLow, voiceHigh}, []voicebuild.Keygroup{
		voicebuild.NewKeygroup(36, 59, 48),
		voicebuild.NewKeygroup(60, 96, 72),
	})
}

func instrument(t *testing.T, s *Session) *InstrumentSnapshot {
	t.Helper()
	snap := s.Snapshot()
	if snap.Disk == nil || snap.Disk.Instrument == nil {
		t.Fatal("snapshot has no instrument")
	}
	return snap.Disk.Instrument
}

func TestInstrumentSnapshotFromAssembledDump(t *testing.T) {
	s := twoVoiceSession(t)
	inst := instrument(t, s)

	if len(inst.Banks) == 0 {
		t.Fatal("no banks")
	}
	bank := inst.Banks[0]
	if len(bank.Areas) != 2 {
		t.Fatalf("areas = %d, want 2", len(bank.Areas))
	}
	a := bank.Areas[0]
	if a.VoiceName != voiceLow || a.KeyLow != 36 || a.KeyHigh != 59 || a.Root != 48 {
		t.Fatalf("area 0 = %+v, want LOW 36..59 root 48", a)
	}
	if a.VelLow < 0 || a.VelHigh > 127 || a.VelLow > a.VelHigh {
		t.Fatalf("area 0 velocity = %d..%d", a.VelLow, a.VelHigh)
	}
	if a.MidiChannel < 1 || a.MidiChannel > 16 {
		t.Fatalf("midi channel = %d, want display 1..16", a.MidiChannel)
	}

	if len(inst.Voices) != 2 {
		t.Fatalf("voices = %d, want 2", len(inst.Voices))
	}
	for _, v := range inst.Voices {
		if !v.Referenced {
			t.Fatalf("voice %+v should be referenced", v)
		}
	}
}

func TestInstrumentSnapshotFromCorpusImage(t *testing.T) {
	s := NewSession()
	if _, cerr := s.OpenImage(fixture(t)); cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	inst := instrument(t, s)
	if inst.FileName != "FULL-DATA-FZ" {
		t.Fatalf("file = %q", inst.FileName)
	}
	if len(inst.Banks) == 0 || len(inst.Banks[0].Areas) == 0 {
		t.Fatal("corpus instrument parsed empty")
	}
	// The TECHNO dump carries a velocity split: LUNAR LO plays only
	// above velocity 95.
	found := false
	for _, b := range inst.Banks {
		for _, a := range b.Areas {
			if a.VoiceName == "LUNAR     LO" && a.VelLow == 95 && a.VelHigh == 127 {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("LUNAR LO velocity split not found in the parsed banks")
	}
}

func TestVoiceOnlyDiskHasNoInstrument(t *testing.T) {
	s, _ := importedSession(t)
	snap := s.Snapshot()
	if snap.Disk.Instrument != nil {
		t.Fatal("a loose-voice disk should carry no instrument")
	}
}

// dumpPCMBytes measures the audio area of the disk's full dump: the
// bytes past the voice area. A duplicate must not move this number.
func dumpPCMBytes(t *testing.T, s *Session) int {
	t.Helper()
	img, err := disk.ReadImage(bytes.NewReader(mustExport(t, s)))
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	fzf, err := diskget.FromImage(img, disk.FullDumpName)
	if err != nil {
		t.Fatalf("FromImage: %v", err)
	}
	hdr, err := fzutil.ParseFZFHeader(fzf)
	if err != nil {
		t.Fatalf("ParseFZFHeader: %v", err)
	}
	return len(fzf) - hdr.VoiceAreaStart - disk.VoiceAreaSectors(hdr.NVoice)*disk.SectorSize
}

func mustExport(t *testing.T, s *Session) []byte {
	t.Helper()
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	return out
}

// R19: the effects block parses and every cell of the 3 by 7 matrix
// round trips through apply, plus the bend range.
func TestEffectsRoundTripEveryCell(t *testing.T) {
	s := twoVoiceSession(t)

	inst := instrument(t, s)
	if inst.Effects == nil {
		t.Fatal("no effects block in the instrument snapshot")
	}
	if len(inst.Effects.Matrix) != 3 || len(inst.Effects.Matrix[0]) != 7 {
		t.Fatalf("matrix shape = %dx%d, want 3x7", len(inst.Effects.Matrix), len(inst.Effects.Matrix[0]))
	}

	for controller := 0; controller < 3; controller++ {
		for target := 0; target < 7; target++ {
			want := 10 + controller*7 + target
			if _, cerr := s.SetEffectCell(controller, target, want); cerr != nil {
				t.Fatalf("SetEffectCell(%d,%d): %v", controller, target, cerr)
			}
			got := instrument(t, s).Effects.Matrix[controller][target]
			if got != want {
				t.Fatalf("cell %d,%d = %d, want %d", controller, target, got, want)
			}
		}
	}

	// Out of range clamps; bad indices reject.
	if _, cerr := s.SetEffectCell(0, 0, 900); cerr != nil {
		t.Fatalf("SetEffectCell clamp: %v", cerr)
	}
	if got := instrument(t, s).Effects.Matrix[0][0]; got != 127 {
		t.Fatalf("clamped cell = %d, want 127", got)
	}
	if _, cerr := s.SetEffectCell(3, 0, 1); cerr == nil || cerr.Code != codeInvalidValue {
		t.Fatalf("controller 3: %v", cerr)
	}
	if _, cerr := s.SetEffectCell(0, 7, 1); cerr == nil || cerr.Code != codeInvalidValue {
		t.Fatalf("target 7: %v", cerr)
	}
}

func TestSetBendRange(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.SetBendRange(24); cerr != nil {
		t.Fatalf("SetBendRange: %v", cerr)
	}
	if got := instrument(t, s).Effects.BendRange; got != 24 {
		t.Fatalf("bend = %d, want 24", got)
	}
	if _, cerr := s.SetBendRange(500); cerr != nil {
		t.Fatalf("SetBendRange clamp: %v", cerr)
	}
	if got := instrument(t, s).Effects.BendRange; got != 127 {
		t.Fatalf("clamped bend = %d, want 127", got)
	}
}

// The web reads the same block the CLI edits: set through the session,
// then confirm with fzfeffects on the exported dump.
func TestEffectsAgreeWithCLI(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.SetEffectCell(0, 0, 77); cerr != nil {
		t.Fatalf("SetEffectCell: %v", cerr)
	}
	img, err := disk.ReadImage(bytes.NewReader(mustExport(t, s)))
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	fzf, err := diskget.FromImage(img, disk.FullDumpName)
	if err != nil {
		t.Fatalf("FromImage: %v", err)
	}
	params, err := fzfeffects.ParseBytes(fzf)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if params.ModLFP != 77 {
		t.Fatalf("CLI reads ModLFP = %d, want 77", params.ModLFP)
	}
}

// A duplicated area clones the voice header and shares the source's
// audio; the snapshot says so, so the UI does not charge the user
// twice for one sound.
func TestDuplicateAreaVoiceSharesAudio(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.DuplicateArea(0, 0); cerr != nil {
		t.Fatalf("DuplicateArea: %v", cerr)
	}
	inst := instrument(t, s)
	if len(inst.Voices) != 3 {
		t.Fatalf("voices = %d, want 3 (the clone)", len(inst.Voices))
	}
	if inst.Voices[0].SharesAudio || inst.Voices[1].SharesAudio {
		t.Error("the source voices own their audio")
	}
	if !inst.Voices[2].SharesAudio {
		t.Error("the clone should be marked as sharing audio")
	}
}
