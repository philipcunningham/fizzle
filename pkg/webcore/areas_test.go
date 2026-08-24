package webcore

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
)

// dumpGeometry is what every fizzle reader derives from a dump: the
// summed bank bstep values, the voice count the walk yields from them
// (fzutil.CountAllVoices), the byte where the audio starts, and the
// samples each named voice plays. The last three are what an area
// operation must leave alone: the voice count sizes the voice area,
// and the voice area is what puts the audio where the reader looks
// for it.
type dumpGeometry struct {
	bstepSum   int
	walked     int
	audioStart int
	audio      []byte
	// voices holds the samples each named voice plays. Real dumps carry
	// repeated and blank slot names, so a name that is not unique is
	// dropped rather than compared against the wrong slot.
	voices map[string][]byte
	// extents holds every slot's declared sample range as a pair of byte
	// offsets into the audio area, unclamped. It names the same thing
	// without relying on the names: a slot whose header was clobbered or
	// shifted declares a range no slot declared before.
	extents map[[2]int]bool
}

// readDumpGeometry reads the geometry back out of the exported image,
// so the assertions see what a reader of the written disk sees rather
// than what the session believes.
func readDumpGeometry(t *testing.T, s *Session) dumpGeometry {
	t.Helper()
	return dumpGeometryOf(t, dumpBytes(t, s))
}

// dumpGeometryOf reads the same geometry straight out of a dump's
// bytes, for the sweeps that drive the operations without a disk
// around them.
func dumpGeometryOf(t *testing.T, fzf []byte) dumpGeometry {
	return dumpGeometryUnder(t, fzf, 0)
}

// dumpGeometryUnder reads the geometry under a DIS voice count, or the
// walk when vn is 0.
func dumpGeometryUnder(t *testing.T, fzf []byte, vn int) dumpGeometry {
	t.Helper()
	hdr, err := dumpHeaderFor(fzf, vn)
	if err != nil {
		t.Fatalf("dumpHeaderFor: %v", err)
	}
	g := dumpGeometry{walked: hdr.NVoice, voices: map[string][]byte{}, extents: map[[2]int]bool{}}
	for b := 0; b < hdr.NBankSectors; b++ {
		g.bstepSum += bankBstep(fzf, b)
	}
	g.audioStart = hdr.VoiceAreaStart + disk.VoiceAreaSectors(hdr.NVoice)*disk.SectorSize
	if g.audioStart > len(fzf) {
		t.Fatalf("the voice area for %d voices runs past the %d byte dump", hdr.NVoice, len(fzf))
	}
	g.audio = bytes.Clone(fzf[g.audioStart:])
	seen := map[string]int{}
	for slot := 0; slot < hdr.NVoice; slot++ {
		off := disk.VoiceSlotOffset(hdr.VoiceAreaStart, slot)
		if off+disk.VoiceHeaderUsed > len(fzf) {
			break
		}
		h := fzf[off : off+disk.VoiceHeaderUsed]
		if binary.LittleEndian.Uint16(h[disk.VoiceLoopModeOffset:]) == disk.PlaybackModeNoSound {
			continue
		}
		name := disk.TrimPadded(h[disk.VoiceNameOffset : disk.VoiceNameOffset+disk.LabelSize])
		start := int(binary.LittleEndian.Uint32(h[disk.VoiceWaveStartOffset:])) * disk.BytesPerSample
		end := int(binary.LittleEndian.Uint32(h[disk.VoiceWaveEndOffset:])) * disk.BytesPerSample
		g.extents[[2]int{start, end}] = true
		// The samples are clamped rather than rejected: a voice whose
		// range runs past the audio area is the damage itself, and the
		// comparison against the same voice before the operation reports
		// it.
		start, end = clampInt(start, 0, len(g.audio)), clampInt(end, 0, len(g.audio))
		if start > end {
			start = end
		}
		seen[name]++
		if seen[name] > 1 {
			delete(g.voices, name)
			continue
		}
		g.voices[name] = g.audio[start:end]
	}
	return g
}

// assertVoiceAreaInvariant pins the invariant every area operation has
// to keep. The audio area must be byte identical across the operation:
// a voice count that no longer matches the voice area the dump holds
// moves the derived audio start, and every voice then plays from the
// wrong offset. Each voice named in survivors must still decode to the
// samples it had.
//
// Where the dump arrived with the summed bstep values equal to the
// walked count, that equality has to survive too: in that state the sum
// is what stops the walk, so raising it past the last slot walks the
// reader into the audio. Real hardware dumps arrive with the sum far
// above the count, and there the walk stops on the first byte pattern
// that is not a voice slot instead.
func assertVoiceAreaInvariant(t *testing.T, before, after dumpGeometry, survivors []string) {
	t.Helper()
	if before.bstepSum == before.walked && after.bstepSum != after.walked {
		t.Errorf("summed bstep = %d but the walk yields %d voices: the walk now runs past the last slot",
			after.bstepSum, after.walked)
	}
	if !bytes.Equal(before.audio, after.audio) {
		t.Errorf("the audio area moved: it started at %d holding %d bytes, now starts at %d holding %d",
			before.audioStart, len(before.audio), after.audioStart, len(after.audio))
	}
	for _, name := range survivors {
		want, ok := before.voices[name]
		if !ok {
			t.Fatalf("voice %q was not in the dump before the operation", name)
		}
		got, ok := after.voices[name]
		if !ok {
			t.Errorf("voice %q is gone from the dump", name)
			continue
		}
		if !bytes.Equal(want, got) {
			t.Errorf("voice %q plays different audio after the operation: %d bytes then %d",
				name, len(want), len(got))
		}
	}
}

// nVoiceSession assembles an instrument of n voices, one area each,
// and returns the voice names in slot order. A voice sector holds four
// slots, so the damage a wrong voice count does depends on whether the
// count crosses a multiple of four. Each voice opens on silence, the
// way a recorded sample does: zero bytes read as an empty voice slot,
// so a count that runs past the last slot walks straight into the audio
// and drags the audio start a sector with it.
func nVoiceSession(t *testing.T, n int) (*Session, []string) {
	t.Helper()
	fzf, names := nVoiceDump(t, n)
	s := NewSession()
	if _, cerr := s.LoadFZF(fzf); cerr != nil {
		t.Fatalf("LoadFZF: %v", cerr)
	}
	return s, names
}

// nVoiceDump assembles the same instrument as raw dump bytes.
func nVoiceDump(t *testing.T, n int) ([]byte, []string) {
	t.Helper()
	names := make([]string, n)
	groups := make([]voicebuild.Keygroup, n)
	voices := make([][]byte, n)
	for i := range names {
		names[i] = fmt.Sprintf("V%02d", i)
		note := uint8(36 + i) // #nosec G115 -- small test values
		groups[i] = voicebuild.NewKeygroup(note, note, note)
		samples := make([]int16, 2000+i*100)
		for j := 256; j < len(samples); j++ {
			samples[j] = int16(1 + j%157)
		}
		voices[i] = voiceimport.Encode(samples, 1, names[i], 0, voiceimport.NoLoop())
	}
	fzf, err := voicebuild.AssembleWithKeygroups(voices, groups)
	if err != nil {
		t.Fatalf("AssembleWithKeygroups: %v", err)
	}
	return fzf, names
}

// Adding an area must not move the audio. The area count and the voice
// count come out of the same bank bytes, so an area that arrives
// without room for its slot walks the reader into the audio area.
func TestAddAreaKeepsTheAudioWhereItIs(t *testing.T) {
	for _, n := range []int{4, 5, 8} {
		t.Run(fmt.Sprintf("%d voices", n), func(t *testing.T) {
			s, names := nVoiceSession(t, n)
			before := readDumpGeometry(t, s)

			if _, cerr := s.AddArea(0, 1); cerr != nil {
				t.Fatalf("AddArea: %v", cerr)
			}

			assertVoiceAreaInvariant(t, before, readDumpGeometry(t, s), names)
			areas := instrument(t, s).Banks[0].Areas
			if len(areas) != n+1 {
				t.Fatalf("areas = %d, want %d", len(areas), n+1)
			}
			if got := areas[n].VoiceSlot; got != 1 {
				t.Errorf("the added area points at slot %d, want 1", got)
			}
			if got := len(instrument(t, s).Voices); got != n {
				t.Errorf("voices = %d, want the same %d: an area is not a voice", got, n)
			}
		})
	}
}

// Deleting an area must not move the audio either. Deleting the highest
// voice's area on a five voice instrument otherwise leaves every
// survivor unpacking different audio.
func TestDeleteAreaKeepsTheAudioWhereItIs(t *testing.T) {
	for _, n := range []int{4, 5, 8} {
		t.Run(fmt.Sprintf("%d voices", n), func(t *testing.T) {
			s, names := nVoiceSession(t, n)
			before := readDumpGeometry(t, s)

			if _, cerr := s.DeleteArea(0, n-1); cerr != nil {
				t.Fatalf("DeleteArea: %v", cerr)
			}

			assertVoiceAreaInvariant(t, before, readDumpGeometry(t, s), names[:n-1])
			areas := instrument(t, s).Banks[0].Areas
			if len(areas) != n-1 {
				t.Fatalf("areas = %d, want %d", len(areas), n-1)
			}
			for i, a := range areas {
				if a.VoiceName != names[i] {
					t.Errorf("area %d plays %q, want %q", i, a.VoiceName, names[i])
				}
			}
		})
	}
}

// Adding an area and deleting it again leaves the dump exactly as it
// was, byte for byte: the slot the area claimed goes back, the voice
// area shrinks to the sector it grew from, and no byte of the audio
// ever moved. The image around it can differ, because a file that
// grows and shrinks again comes back on a different sector chain.
func TestAddThenDeleteAreaRoundTrips(t *testing.T) {
	s, _ := nVoiceSession(t, 4)
	before := dumpBytes(t, s)

	if _, cerr := s.AddArea(0, 1); cerr != nil {
		t.Fatalf("AddArea: %v", cerr)
	}
	areas := instrument(t, s).Banks[0].Areas
	if _, cerr := s.DeleteArea(0, len(areas)-1); cerr != nil {
		t.Fatalf("DeleteArea: %v", cerr)
	}

	if after := dumpBytes(t, s); !bytes.Equal(after, before) {
		t.Errorf("adding an area and deleting it again did not restore the dump: %d bytes then %d",
			len(before), len(after))
	}
}

// Deleting an area from the middle of the voice list keeps every other
// voice on its own area: the slot the area gave up comes out of the
// voice area, so the vp[] entries above it have to renumber with it.
func TestDeleteAreaRenumbersTheSurvivingAreas(t *testing.T) {
	s, names := nVoiceSession(t, 5)
	before := readDumpGeometry(t, s)

	if _, cerr := s.DeleteArea(0, 0); cerr != nil {
		t.Fatalf("DeleteArea: %v", cerr)
	}

	assertVoiceAreaInvariant(t, before, readDumpGeometry(t, s), names[1:])
	areas := instrument(t, s).Banks[0].Areas
	if len(areas) != 4 {
		t.Fatalf("areas = %d, want 4", len(areas))
	}
	for i, a := range areas {
		if a.VoiceName != names[i+1] {
			t.Errorf("area %d plays %q, want %q", i, a.VoiceName, names[i+1])
		}
	}
}

// R13: mapping an unreferenced voice gives it an area without
// disturbing the audio, and the voice is referenced afterwards.
func TestMapVoiceKeepsTheAudioWhereItIs(t *testing.T) {
	for _, n := range []int{4, 5, 8} {
		t.Run(fmt.Sprintf("%d voices", n), func(t *testing.T) {
			s, names := nVoiceSession(t, n)
			// Point the last area at the first voice, which leaves the
			// last voice referenced by nothing: the state R13 names.
			if _, cerr := s.SetAreaField(0, n-1, "voiceSlot", 0); cerr != nil {
				t.Fatalf("SetAreaField: %v", cerr)
			}
			before := readDumpGeometry(t, s)

			if _, cerr := s.MapVoice(n - 1); cerr != nil {
				t.Fatalf("MapVoice: %v", cerr)
			}

			assertVoiceAreaInvariant(t, before, readDumpGeometry(t, s), names)
			inst := instrument(t, s)
			if len(inst.Banks[0].Areas) != n+1 {
				t.Fatalf("areas = %d, want %d", len(inst.Banks[0].Areas), n+1)
			}
			mapped := inst.Banks[0].Areas[n]
			if mapped.VoiceSlot != n-1 || mapped.VoiceName != names[n-1] {
				t.Fatalf("mapped area = %+v, want slot %d (%s)", mapped, n-1, names[n-1])
			}
			for _, v := range inst.Voices {
				if !v.Referenced {
					t.Errorf("voice %q is still unreferenced after mapping it", v.Name)
				}
			}
		})
	}
}

// The same on a real hardware dump. There the areas outnumber the
// voices, because several key splits share a voice through vp[], so
// the summed bsteps sit far above the count and the walk ends on the
// audio's own byte pattern rather than on the bound. An area operation
// must leave that alone too, without growing the voice area for a slot
// the count never reaches.
func TestAreaOpsOnAHardwareDumpLeaveTheAudioAlone(t *testing.T) {
	s := NewSession()
	if _, cerr := s.OpenImage(fixture(t)); cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	before := readDumpGeometry(t, s)
	if before.bstepSum <= before.walked {
		t.Fatalf("fixture summed bstep = %d and walks %d voices; this test needs a dump that shares voices",
			before.bstepSum, before.walked)
	}
	names := make([]string, 0, len(before.voices))
	for name := range before.voices {
		names = append(names, name)
	}

	if _, cerr := s.AddArea(0, 1); cerr != nil {
		t.Fatalf("AddArea: %v", cerr)
	}
	added := readDumpGeometry(t, s)
	assertVoiceAreaInvariant(t, before, added, names)
	if added.walked != before.walked {
		t.Errorf("voice count = %d after adding an area, want the same %d", added.walked, before.walked)
	}

	areas := instrument(t, s).Banks[0].Areas
	if _, cerr := s.DeleteArea(0, len(areas)-1); cerr != nil {
		t.Fatalf("DeleteArea: %v", cerr)
	}
	assertVoiceAreaInvariant(t, before, readDumpGeometry(t, s), names)

	// The two operations that take a voice slot outright rather than
	// reserving an empty one have to work here too: both grow the voice
	// area, so the walk still has to end where the operation put the
	// audio. The clone carries the source's name, so the survivors are
	// checked by sample range rather than by name.
	if _, cerr := s.DuplicateArea(0, 0); cerr != nil {
		t.Fatalf("DuplicateArea: %v", cerr)
	}
	assertAudioHeld(t, "DuplicateArea", before, readDumpGeometry(t, s))

	joined := readDumpGeometry(t, s)
	if _, cerr := s.AddVoice(testFZV(t, "JOINED", 1500)); cerr != nil {
		t.Fatalf("AddVoice: %v", cerr)
	}
	after := readDumpGeometry(t, s)
	if !bytes.Equal(joined.audio, after.audio[:len(joined.audio)]) {
		t.Errorf("joining a voice moved the audio already on the disk: it started at %d, now starts at %d",
			joined.audioStart, after.audioStart)
	}
}

// A mapped area has to be audible and pitched from its own voice. A
// zero gchn routes to no generator, which the sampler plays silently,
// and a zero root pitches every note from C-1.
func TestMappedAreaIsAudibleAndPitchedFromItsVoice(t *testing.T) {
	s, names := nVoiceSession(t, 4)
	// nVoiceSession keys voice i at note 36+i, root included.
	const rootOfSlot2, rootOfSlot3 = 38, 39

	if _, cerr := s.AddArea(0, 2); cerr != nil {
		t.Fatalf("AddArea: %v", cerr)
	}
	added := instrument(t, s).Banks[0].Areas[4]
	if added.Output != int(disk.PolyphonicAudioOut) {
		t.Errorf("added area output = %d (%q), want %d (all generators)",
			added.Output, added.OutputLabel, disk.PolyphonicAudioOut)
	}
	if added.Root != rootOfSlot2 {
		t.Errorf("added area root = %d, want the voice's own %d", added.Root, rootOfSlot2)
	}

	// The same for the R13 map path, on a voice nothing references.
	if _, cerr := s.SetAreaField(0, 3, "voiceSlot", 0); cerr != nil {
		t.Fatalf("SetAreaField: %v", cerr)
	}
	if _, cerr := s.MapVoice(3); cerr != nil {
		t.Fatalf("MapVoice: %v", cerr)
	}
	areas := instrument(t, s).Banks[0].Areas
	mapped := areas[len(areas)-1]
	if mapped.VoiceName != names[3] {
		t.Fatalf("mapped area = %+v, want %s", mapped, names[3])
	}
	if mapped.Output != int(disk.PolyphonicAudioOut) {
		t.Errorf("mapped area output = %d (%q), want %d (all generators)",
			mapped.Output, mapped.OutputLabel, disk.PolyphonicAudioOut)
	}
	if mapped.Root != rootOfSlot3 {
		t.Errorf("mapped area root = %d, want the voice's own %d", mapped.Root, rootOfSlot3)
	}
}

func TestSetAreaFieldRoundTripsAndClamps(t *testing.T) {
	s := twoVoiceSession(t)

	if _, cerr := s.SetAreaField(0, 0, "keyLow", 40); cerr != nil {
		t.Fatalf("SetAreaField: %v", cerr)
	}
	if got := instrument(t, s).Banks[0].Areas[0].KeyLow; got != 40 {
		t.Fatalf("keyLow = %d, want 40", got)
	}

	if _, cerr := s.SetAreaField(0, 0, fieldAreaVelHigh, 900); cerr != nil {
		t.Fatalf("SetAreaField: %v", cerr)
	}
	if got := instrument(t, s).Banks[0].Areas[0].VelHigh; got != 127 {
		t.Fatalf("velHigh = %d, want 127", got)
	}

	// MIDI channel speaks the display scale (1..16).
	if _, cerr := s.SetAreaField(0, 1, "midiChannel", 16); cerr != nil {
		t.Fatalf("SetAreaField: %v", cerr)
	}
	if got := instrument(t, s).Banks[0].Areas[1].MidiChannel; got != 16 {
		t.Fatalf("midiChannel = %d, want 16", got)
	}

	// Re-pointing an area at another voice slot updates the name too.
	if _, cerr := s.SetAreaField(0, 0, "voiceSlot", 1); cerr != nil {
		t.Fatalf("SetAreaField: %v", cerr)
	}
	if got := instrument(t, s).Banks[0].Areas[0]; got.VoiceSlot != 1 || got.VoiceName != voiceHigh {
		t.Fatalf("area 0 = %+v, want slot 1 HIGH", got)
	}

	if _, cerr := s.SetAreaField(0, 0, "warp", 1); cerr == nil || cerr.Code != "invalid-field" {
		t.Fatalf("unknown field: %v", cerr)
	}
	if _, cerr := s.SetAreaField(0, 9, "keyLow", 1); cerr == nil || cerr.Code != codeInvalidValue {
		t.Fatalf("area out of range: %v", cerr)
	}
	if _, cerr := s.SetAreaField(0, 0, "voiceSlot", 99); cerr == nil || cerr.Code != codeInvalidValue {
		t.Fatalf("slot out of range: %v", cerr)
	}
}

func TestRenameBank(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.RenameBank(0, "DRUMS"); cerr != nil {
		t.Fatalf("RenameBank: %v", cerr)
	}
	if got := instrument(t, s).Banks[0].Name; got != "DRUMS" {
		t.Fatalf("name = %q, want DRUMS", got)
	}
	if _, cerr := s.RenameBank(0, "THIRTEEN CHRS"); cerr == nil || cerr.Code != codeInvalidValue || cerr.Message != "bank name must be 1 to 12 characters" {
		t.Fatalf("over-long name: %v", cerr)
	}
	if _, cerr := s.RenameBank(0, "café"); cerr == nil || cerr.Code != codeInvalidValue || cerr.Message != "bank name contains non-ASCII character \"é\"" {
		t.Fatalf("non-ASCII name: %v", cerr)
	}
	if _, cerr := s.RenameBank(99, "NAME"); cerr == nil || cerr.Code != codeInvalidValue || cerr.Message != "bank 99 out of range" {
		t.Fatalf("bad bank: %v", cerr)
	}
}

func TestSwapAreasReorders(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.SwapAreas(0, 0, 1); cerr != nil {
		t.Fatalf("SwapAreas: %v", cerr)
	}
	inst := instrument(t, s)
	if inst.Banks[0].Areas[0].VoiceName != voiceHigh || inst.Banks[0].Areas[1].VoiceName != voiceLow {
		t.Fatalf("areas = %+v, want HIGH then LOW", inst.Banks[0].Areas)
	}
}

func TestSwapAreaWithItselfIsAccepted(t *testing.T) {
	s := twoVoiceSession(t)
	beforeImage := mustExport(t, s)

	_, cerr := s.SwapAreas(0, 0, 0)
	if cerr != nil {
		t.Fatalf("SwapAreas: %v", cerr)
	}
	if !bytes.Equal(mustExport(t, s), beforeImage) {
		t.Fatal("self swap changed the image")
	}
}

// R11 delete: the area goes and its voice goes with it, because the
// format sizes the voice area from the banks' area counts and has
// nowhere to keep a voice no area references. The voice that stays
// keeps its own audio, and undo brings both back.
func TestDeleteAreaGivesUpItsVoice(t *testing.T) {
	s := twoVoiceSession(t)
	before := readDumpGeometry(t, s)

	if _, cerr := s.DeleteArea(0, 0); cerr != nil {
		t.Fatalf("DeleteArea: %v", cerr)
	}
	assertVoiceAreaInvariant(t, before, readDumpGeometry(t, s), []string{voiceHigh})

	inst := instrument(t, s)
	if len(inst.Banks[0].Areas) != 1 || inst.Banks[0].Areas[0].VoiceName != voiceHigh {
		t.Fatalf("areas after delete = %+v", inst.Banks[0].Areas)
	}
	if got := voiceNames(inst); len(got) != 1 || got[0] != voiceHigh {
		t.Fatalf("voices after delete = %v, want [%s]", got, voiceHigh)
	}

	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	inst = instrument(t, s)
	if got := voiceNames(inst); len(got) != 2 || got[0] != voiceLow {
		t.Fatalf("voices after undo = %v, want both back", got)
	}
	after := readDumpGeometry(t, s)
	assertVoiceAreaInvariant(t, before, after, []string{voiceLow, voiceHigh})
}

// R13: the map path lands a playable area with the container's full
// key range and velocity window.
func TestMapVoiceLandsPlayableDefaults(t *testing.T) {
	s := twoVoiceSession(t)
	// Point HIGH's area at LOW, which leaves HIGH referenced by nothing.
	if _, cerr := s.SetAreaField(0, 1, "voiceSlot", 0); cerr != nil {
		t.Fatalf("SetAreaField: %v", cerr)
	}
	if _, cerr := s.MapVoice(1); cerr != nil {
		t.Fatalf("MapVoice: %v", cerr)
	}
	inst := instrument(t, s)
	if len(inst.Banks[0].Areas) != 3 {
		t.Fatalf("areas after map = %d, want 3", len(inst.Banks[0].Areas))
	}
	mapped := inst.Banks[0].Areas[2]
	if mapped.VoiceName != voiceHigh {
		t.Fatalf("mapped area = %+v, want %s", mapped, voiceHigh)
	}
	if mapped.KeyHigh != 127 || mapped.VelLow != 1 || mapped.VelHigh != 127 {
		t.Fatalf("mapped area defaults = %+v", mapped)
	}
	if mapped.Output == 0 {
		t.Errorf("mapped area routes to no output: %+v", mapped)
	}
	if mapped.Root == 0 {
		t.Errorf("mapped area has root 0, so every note pitches from C-1: %+v", mapped)
	}
	for _, v := range inst.Voices {
		if !v.Referenced {
			t.Fatalf("voice %+v still unreferenced after map", v)
		}
	}
}

// A bank with no areas drops out of the dump on the next parse, taking
// every later bank with it, so the last area of a bank does not delete.
func TestDeleteAreaRefusesABanksLastArea(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.DeleteArea(0, 1); cerr != nil {
		t.Fatalf("DeleteArea: %v", cerr)
	}
	before := mustExport(t, s)

	_, cerr := s.DeleteArea(0, 0)
	if cerr == nil || cerr.Code != codeLastArea {
		t.Fatalf("cerr = %v, want %s", cerr, codeLastArea)
	}
	if !bytes.Equal(mustExport(t, s), before) {
		t.Error("the refused delete must not touch the image")
	}
}

func TestAreaEditsUndo(t *testing.T) {
	s := twoVoiceSession(t)
	before := instrument(t, s).Banks[0].Areas[0]

	if _, cerr := s.DeleteArea(0, 0); cerr != nil {
		t.Fatalf("DeleteArea: %v", cerr)
	}
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	after := instrument(t, s).Banks[0].Areas[0]
	if after != before {
		t.Fatalf("undo did not restore area 0: %+v then %+v", before, after)
	}
}

func TestAreaOpsNeedAnInstrument(t *testing.T) {
	s, _ := importedSession(t)
	if _, cerr := s.SetAreaField(0, 0, "keyLow", 1); cerr == nil || cerr.Code != "no-instrument" {
		t.Fatalf("cerr = %v, want no-instrument", cerr)
	}
}

// R11 duplicate: the velocity switch workflow. The clone points at a
// fresh voice slot whose header copies the source, so the audio is
// shared and the PCM footprint does not move.
func TestDuplicateAreaSharesAudio(t *testing.T) {
	s := twoVoiceSession(t)

	before := instrument(t, s)
	srcArea := before.Banks[0].Areas[0]
	pcmBefore := dumpPCMBytes(t, s)

	if _, cerr := s.DuplicateArea(0, 0); cerr != nil {
		t.Fatalf("DuplicateArea: %v", cerr)
	}

	inst := instrument(t, s)
	if len(inst.Banks[0].Areas) != 3 {
		t.Fatalf("areas = %d, want 3", len(inst.Banks[0].Areas))
	}
	dup := inst.Banks[0].Areas[2]
	if dup.VoiceName != srcArea.VoiceName {
		t.Fatalf("duplicate name = %q, want %q", dup.VoiceName, srcArea.VoiceName)
	}
	if dup.VoiceSlot == srcArea.VoiceSlot {
		t.Fatal("duplicate reuses the source slot; it must own a fresh one")
	}
	if dup.KeyLow != srcArea.KeyLow || dup.KeyHigh != srcArea.KeyHigh {
		t.Fatalf("duplicate range = %d..%d, want %d..%d", dup.KeyLow, dup.KeyHigh, srcArea.KeyLow, srcArea.KeyHigh)
	}
	if got := dumpPCMBytes(t, s); got != pcmBefore {
		t.Fatalf("PCM bytes moved: %d then %d", pcmBefore, got)
	}

	// The velocity switch: narrow the two layers so they never fight.
	if _, cerr := s.SetAreaField(0, 0, fieldAreaVelHigh, 64); cerr != nil {
		t.Fatalf("SetAreaField: %v", cerr)
	}
	if _, cerr := s.SetAreaField(0, 2, fieldAreaVelLow, 65); cerr != nil {
		t.Fatalf("SetAreaField: %v", cerr)
	}
	inst = instrument(t, s)
	if inst.Banks[0].Areas[0].VelHigh != 64 || inst.Banks[0].Areas[2].VelLow != 65 {
		t.Fatalf("velocity switch = %+v / %+v", inst.Banks[0].Areas[0], inst.Banks[0].Areas[2])
	}
}

func TestDuplicateAreaUndoes(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.DuplicateArea(0, 1); cerr != nil {
		t.Fatalf("DuplicateArea: %v", cerr)
	}
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	if got := len(instrument(t, s).Banks[0].Areas); got != 2 {
		t.Fatalf("areas after undo = %d, want 2", got)
	}
}

// A duplicate takes a fresh voice slot, so a full instrument has no
// room for one. Without this guard the clone lands at slot 64, inside
// the audio area, shifting every voice's wave pointers by a sector.
func TestDuplicateAreaRefusesWhenVoicesAreFull(t *testing.T) {
	const voices = 63
	names := make([]string, voices)
	groups := make([]voicebuild.Keygroup, voices)
	for i := range names {
		names[i] = fmt.Sprintf("V%02d", i)
		note := uint8(30 + i) // #nosec G115 -- small test values
		groups[i] = voicebuild.NewKeygroup(note, note, note)
	}
	s := assembledSession(t, names, groups)
	// A second bank with room, whose one area takes the last free voice
	// slot: the per-bank area guard passes, but the instrument has no
	// free slot left for the clone to land in.
	if _, cerr := s.AddBank(testFZB(t, "SPARE"), 1); cerr != nil {
		t.Fatalf("AddBank: %v", cerr)
	}
	before := mustExport(t, s)

	_, cerr := s.DuplicateArea(1, 0)
	if cerr == nil || cerr.Code != "voice-limit" {
		t.Fatalf("expected voice-limit, got %v", cerr)
	}
	if !bytes.Equal(mustExport(t, s), before) {
		t.Error("the refused duplicate must not touch the image")
	}
	if got := len(instrument(t, s).Voices); got != voices {
		t.Errorf("voices = %d, want %d", got, voices)
	}
}

// Swapping areas moves every per-area field together, the MIDI
// receive channel included: the channel belongs to the key split the
// user is reordering.
func TestSwapAreasMovesMIDIChannel(t *testing.T) {
	s := twoVoiceSession(t)
	if _, cerr := s.SetAreaField(0, 0, "midiChannel", 5); cerr != nil {
		t.Fatalf("SetAreaField: %v", cerr)
	}
	snap, cerr := s.SwapAreas(0, 0, 1)
	if cerr != nil {
		t.Fatalf("SwapAreas: %v", cerr)
	}
	areas := snap.Disk.Instrument.Banks[0].Areas
	if areas[1].MidiChannel != 5 {
		t.Errorf("area 1 channel = %d after swap, want 5 to travel with the area", areas[1].MidiChannel)
	}
	if areas[0].MidiChannel == 5 {
		t.Errorf("area 0 kept channel 5 after the swap")
	}
}

// The panel's AREA LEVEL row and the stored bvol byte run in opposite
// directions, so the web surface speaks the panel's scale and the byte
// underneath holds its inverse.
func TestSetAreaVolumeSpeaksThePanelScale(t *testing.T) {
	s := twoVoiceSession(t)

	for _, level := range []int{0, 30, 64, 127} {
		if _, cerr := s.SetAreaField(0, 0, "volume", level); cerr != nil {
			t.Fatalf("SetAreaField(volume, %d): %v", level, cerr)
		}
		if got := instrument(t, s).Banks[0].Areas[0].Volume; got != level {
			t.Errorf("volume = %d, want %d", got, level)
		}
	}

	// Out of range values clamp on the panel's scale, not the byte's.
	if _, cerr := s.SetAreaField(0, 0, "volume", 900); cerr != nil {
		t.Fatalf("SetAreaField: %v", cerr)
	}
	if got := instrument(t, s).Banks[0].Areas[0].Volume; got != 127 {
		t.Errorf("clamped volume = %d, want 127", got)
	}
}

// A fresh voice stores bvol 0, which the panel calls its loudest. The
// web surface has to agree, or full level reads as silence.
func TestUntouchedAreaReadsAsFullLevel(t *testing.T) {
	s := twoVoiceSession(t)
	if got := instrument(t, s).Banks[0].Areas[0].Volume; got != disk.MaxAreaLevel {
		t.Errorf("an untouched area reads %d, want %d", got, disk.MaxAreaLevel)
	}
}

// The panel's MAX TOUCH and MIN TOUCH rows both floor at 001, not 000.
// Casio's owner's manual prints the slider as 127 over 001, and a
// velocity of zero silences the voice: fizzle's own SFZ importer warns
// about exactly that.
func TestVelocityRangeFloorsAtOne(t *testing.T) {
	s := twoVoiceSession(t)
	for _, field := range []string{fieldAreaVelLow, fieldAreaVelHigh} {
		if _, cerr := s.SetAreaField(0, 0, field, 0); cerr != nil {
			t.Fatalf("SetAreaField(%s, 0): %v", field, cerr)
		}
		area := instrument(t, s).Banks[0].Areas[0]
		got := area.VelLow
		if field == fieldAreaVelHigh {
			got = area.VelHigh
		}
		if got != 1 {
			t.Errorf("%s set to 0 reads %d, want 1: the panel's row floors at 001", field, got)
		}
	}
}
