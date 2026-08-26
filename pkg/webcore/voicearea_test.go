package webcore

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

// The regressions below all come from the same rule. Nothing stores a
// dump's voice count: every fizzle reader walks the voice area bounded
// by the summed bank bstep values and stops at the first byte pattern
// that is not a voice slot. The count sizes the voice area, and the
// voice area's end is where the audio starts, so any operation that
// moves a bstep has to move the voice area with it. The sum is an
// upper bound, not an equality: where areas share voices through vp[]
// it runs above the walked count and the walk ends on the audio's own
// bytes instead.

// sharedVoiceDump is a shareware dump whose summed bsteps (5) run above
// its walked voice count (3), the state the equality guard exists for.
const sharedVoiceDump = "CASIO097.FZF"

// sharedVoiceSession opens the shared-voice dump as a document.
func sharedVoiceSession(t *testing.T) *Session {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "corpus",
		"casio-fz-1-shareware-library-fzf-format", sharedVoiceDump))
	if os.IsNotExist(err) {
		t.Skip("full hardware corpus is not installed")
	}
	if err != nil {
		t.Fatalf("read %s: %v", sharedVoiceDump, err)
	}
	s := NewSession()
	if _, cerr := s.LoadFZF(data); cerr != nil {
		t.Fatalf("LoadFZF: %v", cerr)
	}
	g := readDumpGeometry(t, s)
	if g.bstepSum <= g.walked {
		t.Fatalf("%s summed bstep = %d and walks %d voices; this test needs a dump that shares voices",
			sharedVoiceDump, g.bstepSum, g.walked)
	}
	return s
}

// fzbPlaying builds a one bank dump whose areas play the given voice
// slots, so a test can hand AddBank a bank lifted from a larger
// instrument.
func fzbPlaying(t *testing.T, slots ...int) []byte {
	t.Helper()
	names := make([]string, len(slots))
	for i := range names {
		names[i] = fmt.Sprintf("B%02d", i)
	}
	fzb := testFZB(t, names...)
	for i, slot := range slots {
		binary.LittleEndian.PutUint16(fzb[disk.BankVoiceNumOffset+i*disk.VPEntrySize:], uint16(slot)) // #nosec G115 -- small test values
	}
	return fzb
}

// assertNoDanglingAreas checks every area plays a voice slot the dump
// still holds. A vp[] entry past the walked count addresses bytes the
// reader treats as audio.
func assertNoDanglingAreas(t *testing.T, s *Session) {
	t.Helper()
	inst := instrument(t, s)
	count := readDumpGeometry(t, s).walked
	for b, bank := range inst.Banks {
		for a, area := range bank.Areas {
			if area.VoiceSlot >= count {
				t.Errorf("bank %d area %d plays slot %d of %d", b, a, area.VoiceSlot, count)
			}
		}
	}
}

// assertDocumentUntouched checks a refused operation left the document
// exactly as it was. A refusal is a fine answer to an operation the
// format cannot express; silently writing a wrong image is not.
func assertDocumentUntouched(t *testing.T, s *Session, before []byte) {
	t.Helper()
	if !bytes.Equal(mustExport(t, s), before) {
		t.Error("the refused operation must not touch the image")
	}
}

// Replacing a bank with one holding fewer areas lowers the summed
// bstep values, so the voice area has to give its sectors back with
// them. Without that the walked count collapses, the audio start moves
// back a sector, and the other banks' vp[] entries dangle past the new
// count. Here the slots the shrink would take are four named voices,
// so the replacement is refused rather than made.
func TestAddBankReplacingALargerBankRefusesToDropVoices(t *testing.T) {
	s, names := nVoiceSession(t, 5)
	// A second bank whose two areas play the two slots past the five
	// this instrument arrived with.
	if _, cerr := s.AddBank(fzbPlaying(t, 5, 6), 1); cerr != nil {
		t.Fatalf("AddBank: %v", cerr)
	}
	before := readDumpGeometry(t, s)
	beforeImage := mustExport(t, s)

	// Replace bank 0's five areas with one.
	_, cerr := s.AddBank(fzbPlaying(t, 0), 0)
	if cerr == nil {
		assertVoiceAreaInvariant(t, before, readDumpGeometry(t, s), names)
		assertNoDanglingAreas(t, s)
		t.Fatal("replacing a five area bank with a one area bank cannot keep five voices")
	}
	if cerr.Code != codeSpareVoice {
		t.Errorf("cerr = %v, want %s", cerr, codeSpareVoice)
	}
	assertDocumentUntouched(t, s, beforeImage)
}

// The same shrink, where the slots it takes are the silent
// placeholders a bank's extra areas reserved. Nothing is lost, so the
// voice area gives them back and the geometry stays in step.
func TestAddBankReplacingALargerBankGivesTheSlotsBack(t *testing.T) {
	s, names := nVoiceSession(t, 8)
	// Five more areas, all playing voices the instrument already holds,
	// so the five slots they reserve stay empty.
	if _, cerr := s.AddBank(fzbPlaying(t, 0, 1, 2, 3, 4), 1); cerr != nil {
		t.Fatalf("AddBank: %v", cerr)
	}
	before := readDumpGeometry(t, s)
	if before.walked != 13 {
		t.Fatalf("walked = %d, want 13: the five extra areas each reserve a slot", before.walked)
	}

	if _, cerr := s.AddBank(fzbPlaying(t, 0), 1); cerr != nil {
		t.Fatalf("AddBank replace: %v", cerr)
	}

	after := readDumpGeometry(t, s)
	if after.walked != 9 || after.bstepSum != 9 {
		t.Errorf("summed bstep = %d and the walk yields %d voices, want 9 of each", after.bstepSum, after.walked)
	}
	if after.audioStart != before.audioStart-disk.SectorSize {
		t.Errorf("audio starts at %d, want a sector back from %d", after.audioStart, before.audioStart)
	}
	assertVoiceAreaInvariant(t, before, after, names)
	assertNoDanglingAreas(t, s)
}

// A bank landing on a dump whose summed bsteps already run above the
// walked count must reserve no slots: clearing them turns the bytes
// that were stopping the walk into countable empty slots.
func TestAddBankOnASharedVoiceDumpKeepsTheAudioWhereItIs(t *testing.T) {
	s := sharedVoiceSession(t)
	before := readDumpGeometry(t, s)
	beforeImage := mustExport(t, s)

	if _, cerr := s.AddBank(fzbPlaying(t, 0), 2); cerr != nil {
		assertDocumentUntouched(t, s, beforeImage)
		return
	}

	after := readDumpGeometry(t, s)
	if after.walked != before.walked {
		t.Errorf("voice count = %d after adding a bank, want the same %d", after.walked, before.walked)
	}
	assertVoiceAreaInvariant(t, before, after, nil)
}

// A fresh instrument's placeholder slot carries root 0, which the guard
// on the voice's own root has to catch as well as roots above the MIDI
// range. Taken at face value it pitches every note from C-1.
func TestAddAreaOnAFreshInstrumentPitchesFromMiddleC(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewInstrument("EMPTY"); cerr != nil {
		t.Fatalf("NewInstrument: %v", cerr)
	}
	if _, cerr := s.AddArea(0, 0); cerr != nil {
		t.Fatalf("AddArea: %v", cerr)
	}
	areas := instrument(t, s).Banks[0].Areas
	if len(areas) != 2 {
		t.Fatalf("areas = %d, want 2", len(areas))
	}
	if got := areas[1].Root; got != defaultRootKey {
		t.Errorf("added area root = %d, want %d: root 0 pitches every note from C-1", got, defaultRootKey)
	}

	if _, cerr := s.MapVoice(0); cerr != nil {
		t.Fatalf("MapVoice: %v", cerr)
	}
	areas = instrument(t, s).Banks[0].Areas
	if got := areas[len(areas)-1].Root; got != defaultRootKey {
		t.Errorf("mapped area root = %d, want %d", got, defaultRootKey)
	}
}

// When the slot the deleted area gave up is still played by another
// area, the shrink must not compact the highest unreferenced slot
// instead: that is a real named voice the UI lists as unreferenced and
// R13's Map button exists to rescue.
func TestDeleteAreaDoesNotDropAnUnrelatedVoice(t *testing.T) {
	s, names := nVoiceSession(t, 4)
	// Point area 1 at slot 3, which leaves slot 1's voice referenced by
	// nothing: the state R13's Map button rescues.
	if _, cerr := s.SetAreaField(0, 1, "voiceSlot", 3); cerr != nil {
		t.Fatalf("SetAreaField: %v", cerr)
	}
	before := readDumpGeometry(t, s)
	beforeImage := mustExport(t, s)

	if _, cerr := s.DeleteArea(0, 1); cerr != nil {
		assertDocumentUntouched(t, s, beforeImage)
		if !bytes.Contains([]byte(cerr.Message), []byte(names[1])) {
			t.Errorf("the refusal must name the voice it would drop: %q", cerr.Message)
		}
		return
	}

	after := readDumpGeometry(t, s)
	if _, ok := after.voices[names[1]]; !ok {
		t.Errorf("voice %q is gone: deleting one area dropped an unrelated voice", names[1])
	}
	assertVoiceAreaInvariant(t, before, after, []string{names[0], names[2], names[3]})
}

// Duplicate clones a voice header into a fresh slot, so it moves the
// walked count and has to go through the same helper. On a dump whose
// bytes are what stop the walk, the clone lands on those bytes and the
// reader walks on into the audio.
func TestDuplicateAreaOnASharedVoiceDumpKeepsTheAudioWhereItIs(t *testing.T) {
	s := sharedVoiceSession(t)
	before := readDumpGeometry(t, s)
	beforeImage := mustExport(t, s)

	if _, cerr := s.DuplicateArea(0, 0); cerr != nil {
		assertDocumentUntouched(t, s, beforeImage)
		return
	}

	assertVoiceAreaInvariant(t, before, readDumpGeometry(t, s), nil)
}

// The same for the join path, which appends a voice header at the
// walked count and lands its PCM at the end of the audio area.
func TestAddVoiceToASharedVoiceDumpKeepsTheAudioWhereItIs(t *testing.T) {
	s := sharedVoiceSession(t)
	before := readDumpGeometry(t, s)
	beforeImage := mustExport(t, s)

	if _, cerr := s.AddVoice(testFZV(t, "JOINED", 1500)); cerr != nil {
		assertDocumentUntouched(t, s, beforeImage)
		return
	}

	after := readDumpGeometry(t, s)
	if !bytes.Equal(before.audio, after.audio[:len(before.audio)]) {
		t.Errorf("the audio area moved: it started at %d holding %d bytes, now starts at %d holding %d",
			before.audioStart, len(before.audio), after.audioStart, len(after.audio))
	}
}
