package webcore

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/model"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
	"github.com/philipcunningham/fizzle/pkg/voiceextract"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
)

// testFZV encodes an in-memory voice through the CLI's importer.
func testFZV(t *testing.T, name string, n int) []byte {
	t.Helper()
	samples := make([]int16, n)
	for i := range samples {
		samples[i] = int16(i % 173)
	}
	return voiceimport.Encode(samples, 1, name, 0, voiceimport.NoLoop())
}

// testFZF assembles a full dump from named voices with default ranges.
func testFZF(t *testing.T, names ...string) []byte {
	t.Helper()
	voices := make([][]byte, len(names))
	groups := make([]voicebuild.Keygroup, len(names))
	for i, name := range names {
		voices[i] = testFZV(t, name, 1500+i*100)
		lo := uint8(36 + i*12) // #nosec G115 -- small test values
		groups[i] = voicebuild.NewKeygroup(lo, lo+11, lo+5)
	}
	fzf, err := voicebuild.AssembleWithKeygroups(voices, groups)
	if err != nil {
		t.Fatalf("AssembleWithKeygroups: %v", err)
	}
	return fzf
}

// voiceNames flattens the instrument voice list for assertions.
func voiceNames(inst *InstrumentSnapshot) []string {
	names := make([]string, len(inst.Voices))
	for i, v := range inst.Voices {
		names[i] = v.Name
	}
	return names
}

// R7 matrix, .fzv row, "no disk open": a new disk and an instrument
// containing the voice.
func TestAddVoiceNoDiskCreatesDiskAndInstrument(t *testing.T) {
	s := NewSession()
	fzv := testFZV(t, "SOLO", 2000)

	snap, cerr := s.AddVoice(fzv)
	if cerr != nil {
		t.Fatalf("AddVoice: %v", cerr)
	}
	if snap.Disk == nil || snap.Disk.Label != defaultLabel {
		t.Fatalf("expected a new %s disk, got %+v", defaultLabel, snap.Disk)
	}
	inst := instrument(t, s)
	if len(inst.Voices) != 1 || inst.Voices[0].Name != "SOLO" {
		t.Fatalf("voices = %v, want [SOLO]", voiceNames(inst))
	}
	if !inst.Voices[0].Referenced {
		t.Error("the assembled instrument should map its voice")
	}

	// The voice's audio survives placement.
	wantRate, wantPCM, err := voiceextract.Decode(fzv)
	if err != nil {
		t.Fatal(err)
	}
	aud, aerr := s.AuditionSlot(0)
	if aerr != nil {
		t.Fatalf("AuditionSlot: %v", aerr)
	}
	if aud.SampleRate != int(wantRate) || len(aud.PCM) != len(wantPCM) {
		t.Fatalf("slot audio = %d frames at %d Hz, want %d at %d", len(aud.PCM), aud.SampleRate, len(wantPCM), wantRate)
	}
}

// R7 matrix, .fzv row, "instrument open": joins the voice list, mapped
// to a fresh area so the next parse still finds the voice.
func TestAddVoiceJoinsVoiceList(t *testing.T) {
	s := twoVoiceSession(t)
	pcmBefore := dumpPCMBytes(t, s)
	areasBefore := len(instrument(t, s).Banks[0].Areas)
	fzv := testFZV(t, "EXTRA", 1800)

	if _, cerr := s.AddVoice(fzv); cerr != nil {
		t.Fatalf("AddVoice: %v", cerr)
	}
	inst := instrument(t, s)
	if len(inst.Voices) != 3 {
		t.Fatalf("voices = %v, want 3 entries", voiceNames(inst))
	}
	added := inst.Voices[2]
	if added.Name != "EXTRA" || !added.Referenced {
		t.Fatalf("added voice = %+v, want referenced EXTRA", added)
	}
	areas := inst.Banks[0].Areas
	if len(areas) != areasBefore+1 {
		t.Fatalf("areas = %d, want %d", len(areas), areasBefore+1)
	}
	joined := areas[len(areas)-1]
	if joined.VoiceSlot != added.Slot || joined.VoiceName != "EXTRA" {
		t.Fatalf("joined area = %+v, want slot %d EXTRA", joined, added.Slot)
	}
	if joined.VelLow != 1 || joined.VelHigh != 127 {
		t.Fatalf("joined area velocity = %d..%d, want 1..127", joined.VelLow, joined.VelHigh)
	}

	_, wantPCM, err := voiceextract.Decode(fzv)
	if err != nil {
		t.Fatal(err)
	}
	grown := dumpPCMBytes(t, s) - pcmBefore
	if grown != disk.PadToSector(len(wantPCM)*disk.BytesPerSample) {
		t.Fatalf("PCM grew by %d bytes, want %d", grown, disk.PadToSector(len(wantPCM)*disk.BytesPerSample))
	}

	// The slot's audio decodes frame for frame like the .fzv.
	aud, aerr := s.AuditionSlot(added.Slot)
	if aerr != nil {
		t.Fatalf("AuditionSlot: %v", aerr)
	}
	if len(aud.PCM) != len(wantPCM) {
		t.Fatalf("slot decodes %d frames, want %d", len(aud.PCM), len(wantPCM))
	}
	for i := range wantPCM {
		if aud.PCM[i] != wantPCM[i] {
			t.Fatalf("frame %d = %d, want %d", i, aud.PCM[i], wantPCM[i])
		}
	}

	// Undo unwinds the whole join as one step.
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	inst = instrument(t, s)
	if got := len(inst.Voices); got != 2 {
		t.Fatalf("after undo, voices = %d, want 2", got)
	}
	if got := len(inst.Banks[0].Areas); got != areasBefore {
		t.Fatalf("after undo, areas = %d, want %d", got, areasBefore)
	}
}

// Growing past a voice sector boundary keeps every earlier voice
// intact; the grow-at-boundary behaviour.
func TestAddVoiceGrowsVoiceArea(t *testing.T) {
	s := twoVoiceSession(t)
	for i := range 4 {
		name := []string{"EX1", "EX2", "EX3", "EX4"}[i]
		if _, cerr := s.AddVoice(testFZV(t, name, 1200)); cerr != nil {
			t.Fatalf("AddVoice %s: %v", name, cerr)
		}
	}
	inst := instrument(t, s)
	if len(inst.Voices) != 6 {
		t.Fatalf("voices = %v, want 6", voiceNames(inst))
	}
	// Slot 5 sits in the second voice sector; both it and slot 0 decode.
	for _, slot := range []int{0, 5} {
		if _, aerr := s.AuditionSlot(slot); aerr != nil {
			t.Fatalf("AuditionSlot %d: %v", slot, aerr)
		}
	}
}

// R7 matrix, .wav row, "instrument open": convert and join the voice
// list (the import deferred from slice 5).
func TestImportWAVToInstrumentGrows(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.ImportWAVToInstrument("kick drum.wav", wavBytes(t, 900), 18000, ChannelMix); cerr != nil {
		t.Fatalf("ImportWAVToInstrument: %v", cerr)
	}
	inst := instrument(t, s)
	if len(inst.Voices) != 3 || inst.Voices[2].Name != "KICK DRUM" {
		t.Fatalf("voices = %v, want KICK DRUM appended", voiceNames(inst))
	}
}

// R7 matrix, .wav row, "no disk open": disk, instrument, and voice all
// come into being.
func TestImportWAVToInstrumentNoDisk(t *testing.T) {
	s := NewSession()
	if _, cerr := s.ImportWAVToInstrument("snare.wav", wavBytes(t, 700), 18000, ChannelMix); cerr != nil {
		t.Fatalf("ImportWAVToInstrument: %v", cerr)
	}
	inst := instrument(t, s)
	if len(inst.Voices) != 1 || inst.Voices[0].Name != "SNARE" {
		t.Fatalf("voices = %v, want [SNARE]", voiceNames(inst))
	}
}

// A first import roots at MIDI 60 (C4) in both the voice header and
// the bank area, so playing C4 reproduces the recorded pitch.
func TestImportWAVToInstrumentRootsAtC4(t *testing.T) {
	s := NewSession()
	snap, cerr := s.ImportWAVToInstrument("piano.wav", wavBytes(t, 500), 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("ImportWAVToInstrument: %v", cerr)
	}
	inst := snap.Disk.Instrument
	if inst == nil {
		t.Fatal("no instrument in snapshot")
	}
	if got := inst.Voices[0].Params["rootKey"]; got != 60 {
		t.Errorf("voice rootKey = %v, want 60", got)
	}
	if got := inst.Banks[0].Areas[0].Root; got != 60 {
		t.Errorf("area root = %d, want 60", got)
	}
}

func TestImportWAVToInstrumentRejectsBadChannel(t *testing.T) {
	s := twoVoiceSession(t)
	_, cerr := s.ImportWAVToInstrument("x.wav", wavBytes(t, 100), 18000, "both")
	if cerr == nil || cerr.Code != "invalid-channel" {
		t.Fatalf("expected invalid-channel, got %v", cerr)
	}
}

// R7 matrix, .fzf row, "no disk open": a new disk with the dump as its
// instrument, byte identical on the disk.
func TestLoadFZFNoDisk(t *testing.T) {
	s := NewSession()
	fzf := testFZF(t, "ALPHA", "BETA")

	if _, cerr := s.LoadFZF(fzf); cerr != nil {
		t.Fatalf("LoadFZF: %v", cerr)
	}
	inst := instrument(t, s)
	if got := voiceNames(inst); len(got) != 2 || got[0] != "ALPHA" || got[1] != "BETA" {
		t.Fatalf("voices = %v, want [ALPHA BETA]", got)
	}
	if got := dumpBytes(t, s); len(got) != len(fzf) {
		t.Fatalf("dump on disk is %d bytes, want %d", len(got), len(fzf))
	}
}

// R7 matrix, .fzf row, "instrument open": replaces the instrument, and
// undo brings the old one back.
func TestLoadFZFReplacesInstrument(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.LoadFZF(testFZF(t, "FRESH")); cerr != nil {
		t.Fatalf("LoadFZF: %v", cerr)
	}
	if got := voiceNames(instrument(t, s)); len(got) != 1 || got[0] != "FRESH" {
		t.Fatalf("voices = %v, want [FRESH]", got)
	}
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	if got := len(instrument(t, s).Voices); got != 2 {
		t.Fatalf("after undo, voices = %d, want 2", got)
	}
}

func TestLoadFZFRejectsGarbage(t *testing.T) {
	s := NewSession()
	_, cerr := s.LoadFZF([]byte("not a dump"))
	if cerr == nil || cerr.Code != "invalid-fzf" {
		t.Fatalf("expected invalid-fzf, got %v", cerr)
	}
	if s.Snapshot().Disk != nil {
		t.Error("a rejected load must not create a disk")
	}
}

// testFZB slices a bank dump (bank sector plus voice headers, no
// audio) out of an assembled full dump, the .fzb shape fzbinfo reads.
func testFZB(t *testing.T, names ...string) []byte {
	t.Helper()
	fzf := testFZF(t, names...)
	hdr, err := fzutil.ParseFZFHeader(fzf)
	if err != nil {
		t.Fatal(err)
	}
	end := hdr.VoiceAreaStart + disk.VoiceAreaSectors(hdr.NVoice)*disk.SectorSize
	return fzf[:end]
}

// R7 matrix, .fzb row, "no disk open": the bank dump itself becomes
// the instrument (mapping and voice headers; audio arrives later).
func TestAddBankNoDisk(t *testing.T) {
	s := NewSession()
	fzb := testFZB(t, "ONE", "TWO")

	if _, cerr := s.AddBank(fzb, 0); cerr != nil {
		t.Fatalf("AddBank: %v", cerr)
	}
	inst := instrument(t, s)
	if got := voiceNames(inst); len(got) != 2 || got[0] != "ONE" {
		t.Fatalf("voices = %v, want [ONE TWO]", got)
	}
	if len(inst.Banks) != 1 || len(inst.Banks[0].Areas) != 2 {
		t.Fatalf("banks = %+v, want 1 bank with 2 areas", inst.Banks)
	}
}

// R7 matrix, .fzb row, "instrument open": joins as a bank at the given
// slot; voice slots keep their meaning by index.
func TestAddBankJoinsAtSlot(t *testing.T) {
	s := twoVoiceSession(t)
	fzb := testFZB(t, "REMAP")

	// Appending at the first unused index grows the banks.
	if _, cerr := s.AddBank(fzb, 1); cerr != nil {
		t.Fatalf("AddBank append: %v", cerr)
	}
	inst := instrument(t, s)
	if len(inst.Banks) != 2 {
		t.Fatalf("banks = %d, want 2", len(inst.Banks))
	}
	// The joined bank's area 0 points at voice slot 0 by index, which
	// resolves to this instrument's own first voice.
	area := inst.Banks[1].Areas[0]
	if area.VoiceSlot != 0 || area.VoiceName != voiceLow {
		t.Fatalf("joined area = %+v, want slot 0 (%s)", area, voiceLow)
	}
	if got := len(inst.Voices); got != 2 {
		t.Fatalf("voices = %d, want 2", got)
	}

	// Replacing bank 0 swaps the mapping in place.
	if _, cerr := s.AddBank(fzb, 0); cerr != nil {
		t.Fatalf("AddBank replace: %v", cerr)
	}
	inst = instrument(t, s)
	if len(inst.Banks) != 2 || len(inst.Banks[0].Areas) != 1 {
		t.Fatalf("banks = %+v, want replaced bank 0 with 1 area", inst.Banks)
	}

	// A slot past the first unused index is rejected.
	if _, cerr := s.AddBank(fzb, 5); cerr == nil || cerr.Code != codeInvalidValue {
		t.Fatalf("expected invalid-value for a skipping slot, got %v", cerr)
	}
}

// A joined bank claims areas of its own, and the areas a dump holds
// are the bound every reader walks the voice area with. A bank that
// claims more areas than the instrument has voice slots walks the
// reader past the last slot and into the audio, which moves the audio
// start out from under every voice.
func TestAddBankKeepsTheAudioWhereItIs(t *testing.T) {
	s, names := nVoiceSession(t, 2)
	before := readDumpGeometry(t, s)

	if _, cerr := s.AddBank(testFZB(t, "B0", "B1", "B2", "B3", "B4", "B5"), 1); cerr != nil {
		t.Fatalf("AddBank: %v", cerr)
	}

	assertVoiceAreaInvariant(t, before, readDumpGeometry(t, s), names)
	inst := instrument(t, s)
	if len(inst.Banks) != 2 || len(inst.Banks[1].Areas) != 6 {
		t.Fatalf("banks = %+v, want the joined bank's 6 areas", inst.Banks)
	}
	// The instrument's own voices are still the audible ones: the
	// joined bank's areas point at slots this instrument never filled.
	if got := voiceNames(inst); len(got) != 2 || got[0] != names[0] {
		t.Fatalf("voices = %v, want the instrument's own %v", got, names)
	}
}

// A bank sector holds more than the bank. Sector 0 also carries the
// instrument's effect block (R19: bend range and all 21 modulation
// cells) and the multi-disk total wave marker, neither of which the
// arriving .fzb owns. Replacing the whole sector would take a bend
// range of 99 down to the firmware default and wipe every cell.
func TestAddBankAtSlotZeroKeepsTheInstrumentsEffects(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.SetBendRange(99); cerr != nil {
		t.Fatalf("SetBendRange: %v", cerr)
	}
	if _, cerr := s.SetEffectCell(0, 0, 77); cerr != nil {
		t.Fatalf("SetEffectCell: %v", cerr)
	}

	if _, cerr := s.AddBank(testFZB(t, "REPLACE"), 0); cerr != nil {
		t.Fatalf("AddBank: %v", cerr)
	}

	eff := instrument(t, s).Effects
	if eff == nil {
		t.Fatal("the instrument carries no effect block after the bank landed")
	}
	if eff.BendRange != 99 {
		t.Errorf("bend range = %d, want the instrument's 99: the bank replaced the effect block", eff.BendRange)
	}
	if eff.Matrix[0][0] != 77 {
		t.Errorf("mod wheel to LFO pitch = %d, want 77", eff.Matrix[0][0])
	}
	// The bank itself did land: its one area replaced the two.
	if got := len(instrument(t, s).Banks[0].Areas); got != 1 {
		t.Errorf("bank 0 holds %d areas, want the arriving bank's 1", got)
	}
}

// The same sector carries the total wave marker, which says how much
// audio the instrument spans across a two disk set. Losing it makes a
// split disk 1 read as a whole document.
func TestAddBankAtSlotZeroKeepsTheTotalWaveMarker(t *testing.T) {
	fzf, _ := nVoiceDump(t, 2)
	const marker = 4242
	binary.LittleEndian.PutUint32(fzf[disk.BankTotalWaveOffset:], marker)

	out, _, cerr := patchDumpBytes(bytes.Clone(fzf), 0, func(d *dumpState) ([]model.Patch, *Error) {
		return addBankPatches(d, fzbPlaying(t, 0), 0)
	})
	if cerr != nil {
		t.Fatalf("addBankPatches: %v", cerr)
	}
	if got := binary.LittleEndian.Uint32(out[disk.BankTotalWaveOffset:]); got != marker {
		t.Errorf("total wave marker = %d, want the instrument's %d", got, marker)
	}
}

// A bank landing anywhere but slot 0 has no instrument fields to
// preserve, and must not carry a stale effect block in from the .fzb
// either: only sector 0's block is the instrument's.
func TestAddBankPastSlotZeroLeavesTheEffectsAlone(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.SetBendRange(42); cerr != nil {
		t.Fatalf("SetBendRange: %v", cerr)
	}
	if _, cerr := s.AddBank(testFZB(t, "SECOND"), 1); cerr != nil {
		t.Fatalf("AddBank: %v", cerr)
	}
	if got := instrument(t, s).Effects.BendRange; got != 42 {
		t.Errorf("bend range = %d, want 42", got)
	}
}

// dumpBytes extracts the disk's full dump from an exported image.
func dumpBytes(t *testing.T, s *Session) []byte {
	t.Helper()
	img, err := disk.ReadImage(bytes.NewReader(mustExport(t, s)))
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	fzf, gerr := diskget.FromImage(img, disk.FullDumpName)
	if gerr != nil {
		t.Fatalf("FromImage: %v", gerr)
	}
	return fzf
}

// A joined voice must route to every generator, like the CLI builder's
// default. A zero gchn byte plays silently on hardware, so the UI
// would show a mapped voice that makes no sound.
func TestJoinedVoiceRoutesToAllOutputs(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.AddVoice(testFZV(t, "JOINED", 1500)); cerr != nil {
		t.Fatalf("AddVoice: %v", cerr)
	}
	areas := instrument(t, s).Banks[0].Areas
	joined := areas[len(areas)-1]
	if joined.Output != int(disk.PolyphonicAudioOut) {
		t.Errorf("joined area output = %d (%q), want %d (all generators)",
			joined.Output, joined.OutputLabel, disk.PolyphonicAudioOut)
	}

	// The same holds for a WAV converted into the instrument.
	if _, cerr := s.ImportWAVToInstrument("wav.wav", wavBytes(t, 800), 18000, ChannelMix); cerr != nil {
		t.Fatalf("ImportWAVToInstrument: %v", cerr)
	}
	areas = instrument(t, s).Banks[0].Areas
	if got := areas[len(areas)-1].Output; got != int(disk.PolyphonicAudioOut) {
		t.Errorf("WAV-joined area output = %d, want %d", got, disk.PolyphonicAudioOut)
	}
}

// R7's .wav and .fzv cells promise sequential mapping and J5 promises
// the next free key range. Every voice the WAV importer produces
// carries the same fixed default range, so the join takes the next free
// key rather than the incoming header (joinKeyRange).
func TestJoinedVoicesTakeTheNextFreeKey(t *testing.T) {
	s := twoVoiceSession(t) // areas at 36..59 and 60..96
	const joins = 8
	for i := range joins {
		name := fmt.Sprintf("hit%d.wav", i)
		if _, cerr := s.ImportWAVToInstrument(name, wavBytes(t, 400), 18000, ChannelMix); cerr != nil {
			t.Fatalf("ImportWAVToInstrument %s: %v", name, cerr)
		}
	}

	inst := instrument(t, s)
	areas := inst.Banks[0].Areas
	if len(areas) != 2+joins {
		t.Fatalf("areas = %d, want %d", len(areas), 2+joins)
	}
	sounding := map[int]int{}
	for i, a := range areas {
		for k := a.KeyLow; k <= a.KeyHigh; k++ {
			sounding[k]++
		}
		if i < 2 {
			continue
		}
		want := 97 + i - 2
		if a.KeyLow != want || a.KeyHigh != want {
			t.Errorf("joined area %d spans %d..%d, want the free key %d alone", i, a.KeyLow, a.KeyHigh, want)
		}
		if a.Root != want {
			t.Errorf("joined area %d has root %d, want %d so the samples sound at the pitch they were recorded at",
				i, a.Root, want)
		}
		// The voice header's own root has to agree with the area's, the
		// way the CLI builder writes them: AddArea reads the header's
		// root when a later area maps the same voice.
		if got := inst.Voices[a.VoiceSlot].Params[fieldRootKey]; got != want {
			t.Errorf("voice slot %d header root = %v, want the area's %d", a.VoiceSlot, got, want)
		}
	}
	for key, n := range sounding {
		if n > 1 {
			t.Errorf("key %d sounds %d areas at once", key, n)
		}
	}
}

// A voice that carries a range of its own keeps it: an .fzv lifted out
// of a real instrument says where on the keyboard it belongs, and the
// join is not entitled to move it.
func TestJoinedVoiceWithItsOwnRangeKeepsIt(t *testing.T) {
	s := twoVoiceSession(t)
	fzv := testFZV(t, "REAL", 1500)
	patches, err := voiceedit.BuildKeyRangePatch(20, 30, 25)
	if err != nil {
		t.Fatalf("BuildKeyRangePatch: %v", err)
	}
	if err := voiceedit.ApplyToFZVBytes(fzv, patches); err != nil {
		t.Fatalf("ApplyToFZVBytes: %v", err)
	}

	if _, cerr := s.AddVoice(fzv); cerr != nil {
		t.Fatalf("AddVoice: %v", cerr)
	}
	areas := instrument(t, s).Banks[0].Areas
	joined := areas[len(areas)-1]
	if joined.KeyLow != 20 || joined.KeyHigh != 30 || joined.Root != 25 {
		t.Errorf("joined area = %d..%d root %d, want the voice's own 20..30 root 25",
			joined.KeyLow, joined.KeyHigh, joined.Root)
	}
}

// The empty instrument's placeholder area spans the whole keyboard;
// the first voice to fill its slot takes the same next free key a later
// join would, so dropping a folder of WAVs and dropping its files one
// at a time land on the same notes.
func TestPlaceholderAreaTakesTheFirstFreeKey(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewInstrument("SET"); cerr != nil {
		t.Fatal(cerr)
	}
	for i := range 3 {
		if _, cerr := s.ImportWAVToInstrument(fmt.Sprintf("h%d.wav", i), wavBytes(t, 300), 18000, ChannelMix); cerr != nil {
			t.Fatalf("ImportWAVToInstrument: %v", cerr)
		}
	}
	areas := instrument(t, s).Banks[0].Areas
	if len(areas) != 3 {
		t.Fatalf("areas = %d, want 3", len(areas))
	}
	for i, a := range areas {
		want := disk.FirstMIDINote + i
		if a.KeyLow != want || a.KeyHigh != want || a.Root != want {
			t.Errorf("area %d = %d..%d root %d, want key %d alone", i, a.KeyLow, a.KeyHigh, a.Root, want)
		}
	}
}

// The empty instrument's placeholder area spans the whole keyboard;
// the first voice to fill its slot retunes it to the voice's own range
// where the voice carries one.
func TestPlaceholderAreaRetunesToTheFilledVoice(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewInstrument("SET"); cerr != nil {
		t.Fatal(cerr)
	}
	// A voice with a narrow range of its own, as an .fzv pulled from a
	// real instrument carries.
	fzv := testFZV(t, "FILLER", 1500)
	patches, perr := voiceedit.BuildKeyRangePatch(48, 55, 50)
	if perr != nil {
		t.Fatalf("BuildKeyRangePatch: %v", perr)
	}
	if perr := voiceedit.ApplyToFZVBytes(fzv, patches); perr != nil {
		t.Fatalf("ApplyToFZVBytes: %v", perr)
	}
	vp, err := fzvinfo.ParseBytes(fzv)
	if err != nil {
		t.Fatal(err)
	}
	if _, cerr := s.AddVoice(fzv); cerr != nil {
		t.Fatalf("AddVoice: %v", cerr)
	}

	inst := instrument(t, s)
	if len(inst.Banks[0].Areas) != 1 {
		t.Fatalf("areas = %d, want the reused placeholder only", len(inst.Banks[0].Areas))
	}
	area := inst.Banks[0].Areas[0]
	if area.KeyLow != int(vp.KeyLow) || area.KeyHigh != int(vp.KeyHigh) || area.Root != int(vp.KeyCentre) {
		t.Errorf("filled area = %d..%d root %d, want the voice's %d..%d root %d",
			area.KeyLow, area.KeyHigh, area.Root, vp.KeyLow, vp.KeyHigh, vp.KeyCentre)
	}
	if area.Output != int(disk.PolyphonicAudioOut) {
		t.Errorf("placeholder area output = %d, want %d", area.Output, disk.PolyphonicAudioOut)
	}
}

// A first voice too large for one disk splits across a pair, the same
// way a join does: the assembled dump routes through replaceDump.
func TestFirstImportSplitsAcrossTwoDisks(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("SPLIT"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	snap, cerr := s.ImportWAVToInstrument("long.wav", monoRateWAV(t, 720000, 18000), 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("ImportWAVToInstrument: %v", cerr)
	}
	if snap.Disk == nil || snap.Disk.Disks != 2 {
		t.Fatalf("disks = %v, want a split pair", snap.Disk)
	}
}
