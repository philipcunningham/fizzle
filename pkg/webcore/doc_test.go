package webcore

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
	"github.com/philipcunningham/fizzle/pkg/voiceextract"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

func TestRenameDisk(t *testing.T) {
	s := twoVoiceSession(t)
	snap, cerr := s.RenameDisk("Fresh Name")
	if cerr != nil {
		t.Fatalf("RenameDisk: %v", cerr)
	}
	if snap.Disk.Label != "Fresh Name" {
		t.Errorf("label = %q, want Fresh Name", snap.Disk.Label)
	}
	img, err := disk.ReadImage(bytes.NewReader(mustExport(t, s)))
	if err != nil {
		t.Fatal(err)
	}
	if img.Label() != "Fresh Name" {
		t.Errorf("exported label = %q", img.Label())
	}
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatal(cerr)
	}
	if got := s.Snapshot().Disk.Label; got != "INST" {
		t.Errorf("after undo, label = %q, want INST", got)
	}

	for _, bad := range []string{"", "THIRTEEN CHAR", "café"} {
		if _, cerr := s.RenameDisk(bad); cerr == nil {
			t.Errorf("expected an error for label %q", bad)
		}
	}
}

func TestDeleteFile(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("DEL"); cerr != nil {
		t.Fatal(cerr)
	}
	if _, cerr := s.ImportWAV("gone.wav", wavBytes(t, 600), 18000, ChannelMix); cerr != nil {
		t.Fatal(cerr)
	}
	if got := len(s.Snapshot().Disk.Files); got != 1 {
		t.Fatalf("files = %d, want 1", got)
	}
	snap, cerr := s.DeleteFile("GONE")
	if cerr != nil {
		t.Fatalf("DeleteFile: %v", cerr)
	}
	if got := len(snap.Disk.Files); got != 0 {
		t.Errorf("files = %d after delete, want 0", got)
	}
	if _, cerr := s.DeleteFile("GONE"); cerr == nil || cerr.Code != codeNotFound {
		t.Fatalf("expected not-found, got %v", cerr)
	}
}

func TestDeleteFullDumpDropsInstrument(t *testing.T) {
	s := twoVoiceSession(t)
	snap, cerr := s.DeleteFile(disk.FullDumpName)
	if cerr != nil {
		t.Fatalf("DeleteFile: %v", cerr)
	}
	if snap.Disk.Instrument != nil {
		t.Error("instrument should be gone with its dump")
	}

	// On a split pair the second disk goes too, and undo restores it.
	split, _ := splitSession(t)
	if _, cerr := split.DeleteFile(disk.FullDumpName); cerr != nil {
		t.Fatalf("DeleteFile on split: %v", cerr)
	}
	if got := split.Snapshot().Disk.Disks; got != 1 {
		t.Errorf("disks = %d after deleting a split dump, want 1", got)
	}
	if _, cerr := split.Undo(); cerr != nil {
		t.Fatal(cerr)
	}
	if got := split.Snapshot().Disk.Disks; got != 2 {
		t.Errorf("disks = %d after undo, want 2", got)
	}
}

func TestExtractFile(t *testing.T) {
	s := twoVoiceSession(t)
	data, cerr := s.ExtractFile(disk.FullDumpName)
	if cerr != nil {
		t.Fatalf("ExtractFile: %v", cerr)
	}
	if !bytes.Equal(data, dumpBytes(t, s)) {
		t.Error("extracted dump differs from the disk's payload")
	}

	// A split pair's dump extracts stitched: the whole instrument.
	split, _ := splitSession(t)
	stitched, cerr := split.ExtractFile(disk.FullDumpName)
	if cerr != nil {
		t.Fatalf("ExtractFile split: %v", cerr)
	}
	d1, _ := split.ExportImageAt(0)
	d2, _ := split.ExportImageAt(1)
	want := append(payload(t, d1), payload(t, d2)...)
	if !bytes.Equal(stitched, want) {
		t.Error("split extract is not the stitched dump")
	}

	if _, cerr := s.ExtractFile("ABSENT"); cerr == nil || cerr.Code != codeNotFound {
		t.Fatalf("expected not-found, got %v", cerr)
	}
}

func TestExtractVoiceSlot(t *testing.T) {
	s := twoVoiceSession(t)

	fzv, name, cerr := s.ExtractVoiceSlot(0, ExtractFZV)
	if cerr != nil {
		t.Fatalf("ExtractVoiceSlot fzv: %v", cerr)
	}
	if name != voiceLow {
		t.Errorf("name = %q, want %s", name, voiceLow)
	}
	if !bytes.Equal(fzv, unpackSlot(t, s, 0)) {
		t.Error("extracted .fzv differs from the CLI unpack of the slot")
	}

	wavData, _, cerr := s.ExtractVoiceSlot(0, ExtractWAV)
	if cerr != nil {
		t.Fatalf("ExtractVoiceSlot wav: %v", cerr)
	}
	parsed, err := wav.Read(bytes.NewReader(wavData))
	if err != nil {
		t.Fatalf("wav.Read: %v", err)
	}
	_, wantSamples, err := voiceextract.Decode(fzv)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Samples) != len(wantSamples) {
		t.Fatalf("wav carries %d samples, want %d", len(parsed.Samples), len(wantSamples))
	}
	for i := range wantSamples {
		if parsed.Samples[i] != wantSamples[i] {
			t.Fatalf("sample %d = %d, want %d", i, parsed.Samples[i], wantSamples[i])
		}
	}

	if _, _, cerr := s.ExtractVoiceSlot(0, "aiff"); cerr == nil || cerr.Code != codeInvalidValue {
		t.Fatalf("expected invalid-value for a bad format, got %v", cerr)
	}
}

func TestSlotPeaks(t *testing.T) {
	s := twoVoiceSession(t)
	peaks, cerr := s.SlotPeaks(1, 0, 2100, 64)
	if cerr != nil {
		t.Fatalf("SlotPeaks: %v", cerr)
	}
	if len(peaks) != 128 {
		t.Fatalf("peaks = %d values, want 128", len(peaks))
	}
	_, samples, err := voiceextract.Decode(unpackSlot(t, s, 1))
	if err != nil {
		t.Fatal(err)
	}
	want := bucketPeaks(samples, 0, 2100, 64)
	for i := range want {
		if peaks[i] != want[i] {
			t.Fatalf("peak %d = %d, want %d", i, peaks[i], want[i])
		}
	}
	if _, cerr := s.SlotPeaks(1, 0, 100, 0); cerr == nil {
		t.Error("expected an error for zero buckets")
	}
	if _, cerr := s.SlotPeaks(9, 0, 100, 8); cerr == nil {
		t.Error("expected an error for a bad slot")
	}
}

// R4: a new empty instrument, and the first voice fills its
// placeholder instead of appending past it.
func TestNewInstrumentAndFirstFill(t *testing.T) {
	s := NewSession()
	snap, cerr := s.NewInstrument("MY SET")
	if cerr != nil {
		t.Fatalf("NewInstrument: %v", cerr)
	}
	inst := snap.Disk.Instrument
	if inst == nil {
		t.Fatal("no instrument after NewInstrument")
	}
	if inst.Banks[0].Name != "MY SET" {
		t.Errorf("bank name = %q, want MY SET", inst.Banks[0].Name)
	}
	if len(inst.Voices) != 0 {
		t.Fatalf("voices = %d, want 0 (the placeholder stays hidden)", len(inst.Voices))
	}
	if len(inst.Banks[0].Areas) != 1 {
		t.Fatalf("areas = %d, want the placeholder area", len(inst.Banks[0].Areas))
	}

	// The first voice fills slot 0 and inherits the placeholder area.
	if _, cerr := s.AddVoice(testFZV(t, "FIRST", 1500)); cerr != nil {
		t.Fatalf("AddVoice: %v", cerr)
	}
	inst = instrument(t, s)
	if len(inst.Voices) != 1 || inst.Voices[0].Slot != 0 || inst.Voices[0].Name != "FIRST" {
		t.Fatalf("voices = %+v, want FIRST at slot 0", inst.Voices)
	}
	if !inst.Voices[0].Referenced {
		t.Error("the filled slot should be referenced by the placeholder area")
	}
	if got := len(inst.Banks[0].Areas); got != 1 {
		t.Errorf("areas = %d, want 1 (no double mapping)", got)
	}
	if aud, cerr := s.AuditionSlot(0); cerr != nil || len(aud.PCM) != 1500 {
		t.Fatalf("AuditionSlot after fill: %v (%d frames)", cerr, len(aud.PCM))
	}

	if _, cerr := s.NewInstrument("AGAIN"); cerr == nil || cerr.Code != "instrument-exists" {
		t.Fatalf("expected instrument-exists, got %v", cerr)
	}
}

// The enriched snapshot: every slot carries params and detail, with
// loop addresses voice-relative.
func TestInstrumentVoicesCarryParamsAndDetail(t *testing.T) {
	s := twoVoiceSession(t)
	inst := instrument(t, s)

	v0 := inst.Voices[0]
	if v0.Params == nil || v0.Voice == nil {
		t.Fatal("slot 0 carries no params or detail")
	}
	if got := v0.Voice.Frames; got != 2000 {
		t.Errorf("slot 0 frames = %d, want 2000", got)
	}
	if got := inst.Voices[1].Voice.Frames; got != 2100 {
		t.Errorf("slot 1 frames = %d, want 2100", got)
	}
	if got, ok := v0.Params[fieldRootKey].(int); !ok || got < 0 || got > 127 {
		t.Errorf("slot 0 rootKey param = %v", v0.Params[fieldRootKey])
	}

	// A slot loop edit reads back voice-relative.
	if _, cerr := s.SetSlotLoop(1, 0, 100, 900); cerr != nil {
		t.Fatal(cerr)
	}
	loop := instrument(t, s).Voices[1].Voice.Loops[0]
	if loop.Start != 100 || loop.End != 900 {
		t.Errorf("slot 1 loop 0 = %d..%d, want 100..900 (rebased)", loop.Start, loop.End)
	}
}

// The enrichment stays cheap: headers only, no PCM. 100 parses of a
// 64-voice dump must land well inside the interactive budget.
func TestInstrumentFromSixtyFourVoicesIsCheap(t *testing.T) {
	names := make([]string, 64)
	groups := make([]voicebuild.Keygroup, 64)
	for i := range names {
		names[i] = fmt.Sprintf("V%02d", i)
		note := uint8(30 + i) // #nosec G115 -- small test values
		groups[i] = voicebuild.NewKeygroup(note, note, note)
	}
	s := assembledSession(t, names, groups)
	fzf := dumpBytes(t, s)

	start := time.Now()
	for range 100 {
		if _, err := instrumentFrom(disk.FullDumpName, fzf, 0); err != nil {
			t.Fatal(err)
		}
	}
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Errorf("100 enriched parses of 64 voices took %v; the snapshot path is too hot", elapsed)
	}
	t.Logf("100 enriched 64-voice parses: %v", elapsed)
}

// NewInstrument holds names to the same printable ASCII rule every
// rename path enforces.
func TestNewInstrumentRefusesUnprintableName(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("NAME"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	if _, cerr := s.NewInstrument("h\x01i"); cerr == nil {
		t.Fatal("control byte accepted in an instrument name")
	}
}
