package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/sfzconvert"
	"github.com/philipcunningham/fizzle/pkg/studio/audio"
	"github.com/philipcunningham/fizzle/pkg/studio/loader"
	"github.com/philipcunningham/fizzle/pkg/studio/nav"
	"github.com/philipcunningham/fizzle/pkg/studio/spaces/layout"
	"github.com/philipcunningham/fizzle/pkg/studio/spaces/pool"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/minimap"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
)

const (
	qaNameTwo    = "TWO"
	qaEmptyLabel = "(empty)"
)

// bankNameAt reads the trimmed bank name stored in bank bankIdx's sector.
func bankNameAt(data []byte, bankIdx int) string {
	off := bankIdx*disk.SectorSize + disk.BankNameOffset
	if off+disk.VoiceNameFieldSize > len(data) {
		return ""
	}
	return strings.TrimRight(strings.Trim(string(data[off:off+disk.VoiceNameFieldSize]), "\x00"), " ")
}

// voiceNameAtSlot reads the trimmed voice name stored in the voice
// header at the given absolute voice-area slot.
func voiceNameAtSlot(data []byte, voiceAreaStart, slot int) string {
	off := disk.VoiceSlotOffset(voiceAreaStart, slot) + disk.VoiceNameOffset
	if off+disk.VoiceNameFieldSize > len(data) {
		return ""
	}
	raw := data[off : off+disk.VoiceNameFieldSize]
	return strings.TrimRight(strings.Trim(string(raw), "\x00"), " ")
}

// findDivergentArea returns an area in bank 0 of a loaded fixture whose
// vp[] pointer slot differs from its cumulative list-position slot
// (bank 0: position == areaIdx), with DISTINCT voice names at the two
// slots. This is exactly the configuration where a position-indexed
// operation (the F-QA-15 bug) edits the wrong voice. Fails loudly if
// the fixture has no such area, so the regression guard can never go
// vacuous.
func findDivergentArea(t *testing.T, st journeyState) (areaIdx, ptrSlot, posSlot int) {
	t.Helper()
	data := st.a.containerModel.Bytes()
	voiceAreaStart := st.a.containerInfo.BankCount * disk.SectorSize
	bstep := int(binary.LittleEndian.Uint16(data[disk.BankVoiceCountOffset:]))
	for a := 0; a < bstep; a++ {
		ps, ok := disk.BankVPLookup(data, 0, a)
		if !ok {
			continue
		}
		pos := a // bank 0: cumulative list position == areaIdx
		if ps == pos {
			continue
		}
		ptrName := voiceNameAtSlot(data, voiceAreaStart, ps)
		posName := voiceNameAtSlot(data, voiceAreaStart, pos)
		if ptrName != "" && posName != "" && ptrName != posName {
			return a, ps, pos
		}
	}
	t.Fatalf("fixture has no area whose vp[] pointer diverges from its list position with distinct voices; the F-QA-15 regression guard would be vacuous")
	return -1, -1, -1
}

// TestRename_TargetsVoicePointerNotListPosition pins F-QA-15: renaming
// an area's voice must follow the area's vp[] pointer (the voice the
// list displays), not the cumulative list-position slot. On a disk
// whose voice-table order differs from area order, the position-indexed
// rename silently renamed a DIFFERENT voice while the status message
// confirmed the intended area.
func TestRename_TargetsVoicePointerNotListPosition(t *testing.T) {
	st := newJourneyWithFixture(t, "synthetic/TECHNO.img")
	areaIdx, ptrSlot, posSlot := findDivergentArea(t, st)
	voiceAreaStart := st.a.containerInfo.BankCount * disk.SectorSize

	before := st.a.containerModel.Bytes()
	posNameBefore := voiceNameAtSlot(before, voiceAreaStart, posSlot)

	const newName = "ZZQATEST"
	st.a.renameActive = true
	st.a.renameBank = false
	st.a.renameTarget = pickerTarget{BankIdx: 0, AreaIdx: areaIdx}
	st.a.renameBuffer = newName
	m, _ := st.a.commitRename()
	st.a, _ = m.(App)

	after := st.a.containerModel.Bytes()
	if got := voiceNameAtSlot(after, voiceAreaStart, ptrSlot); got != newName {
		t.Errorf("rename wrote to the wrong slot: vp[] target slot %d = %q, want %q", ptrSlot, got, newName)
	}
	if got := voiceNameAtSlot(after, voiceAreaStart, posSlot); got != posNameBefore {
		t.Errorf("rename corrupted the list-position slot %d: now %q, was %q (position-indexing bug)", posSlot, got, posNameBefore)
	}
}

// TestExtractToPool_FullVoiceCorrectSlot pins F-QA-8 (and the extract
// half of F-QA-15): sending a voice from a Layout area to the pool must
// store the COMPLETE voice (header + audio) for the area's actual
// pointer slot, identical to what area-export produces. Before the fix
// it copied only the 256-byte header of the wrong (list-position) slot,
// yielding a stub fzv info rejects.
func TestExtractToPool_FullVoiceCorrectSlot(t *testing.T) {
	st := newJourneyWithFixture(t, "synthetic/TECHNO.img")
	areaIdx, _, _ := findDivergentArea(t, st)

	// The area-export path is the known-correct full-FZV producer.
	exApp := st.a.exportAreaToWorkspace(0, areaIdx)
	_ = exApp
	files, err := filepath.Glob(filepath.Join(st.dir, "*.fzv"))
	if err != nil || len(files) != 1 {
		t.Fatalf("expected one exported .fzv as the reference, got %v (err %v)", files, err)
	}
	want, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read reference export: %v", err)
	}
	if len(want) <= disk.SectorSize {
		t.Fatalf("reference export is header-only (%d bytes); fixture/setup problem", len(want))
	}

	st.a = st.a.handleLayoutIntent(layout.Intent{
		Kind: layout.IntentExtractToPool, BankIdx: 0, AreaIdx: areaIdx,
	})
	entry := st.a.pool.Selected()
	if entry == nil {
		t.Fatal("extract-to-pool added no pool entry")
	}
	if len(entry.Bytes) <= disk.SectorSize {
		t.Errorf("pool entry is header-only (%d bytes): audio dropped on extract", len(entry.Bytes))
	}
	if !bytes.Equal(entry.Bytes, want) {
		t.Errorf("pool entry (%d bytes) differs from the correct area-export (%d bytes): wrong slot and/or missing audio",
			len(entry.Bytes), len(want))
	}
}

// TestExport_MultiDiskHalfRefuses pins F-QA-17: exporting a voice from
// disk 1 of a 2-disk split must refuse with the same multi-disk guard
// audition uses, instead of silently writing an incomplete .fzv whose
// audio tail lives on disk 2.
func TestExport_MultiDiskHalfRefuses(t *testing.T) {
	sfz := filepath.Join("..", "..", "..", "testdata", "synthetic", "JUNGLISM.sfz")
	dir := t.TempDir()
	prefix := filepath.Join(dir, "JUNGLISM")
	if err := sfzconvert.ConvertMultiDisk(context.Background(), sfz, prefix, 36000); err != nil {
		t.Fatalf("ConvertMultiDisk: %v", err)
	}

	audio.InstallNoopForTest(t)
	fc := newFakeClock()
	a := New(dir)
	a.tick = fc.Tick
	a.toast.SetClock(fc.Tick)
	a.status.SetClock(fc.Tick)
	m, info, err := loader.LoadContainer(prefix + "-1.img")
	if err != nil {
		t.Fatalf("LoadContainer(disk 1): %v", err)
	}
	a.containerModel = m
	a.containerInfo = info
	a.layout.SetContainer(m, info)
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})
	a.current = minimap.Layout
	a.layout.Apply(nav.Confirm)

	a = a.exportAreaToWorkspace(0, 0)
	got := stripANSI(a.status.View())
	if !strings.Contains(got, "2-disk") && !strings.Contains(got, "multi-disk") {
		t.Errorf("expected multi-disk guard on export, got: %q", got)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.fzv"))
	if len(files) > 0 {
		t.Errorf("export wrote %v despite the multi-disk guard", files)
	}
}

// TestDeleteBank_CompactsAndStaysValid pins F-QA-12 / F-QA-13: deleting
// a bank removes it and shifts later banks up immediately, keeping the
// in-memory disk valid (no "invalid bstep 0" when other banks are later
// unpacked/exported) and matching what save writes. Before the fix,
// deleting bank 0 zeroed bank-0's required bstep, so the whole disk
// failed to unpack in memory until a save quietly compacted it.
func TestDeleteBank_CompactsAndStaysValid(t *testing.T) {
	st := newJourneyWithFixture(t, "synthetic/TECHNO.img")
	if st.a.containerInfo.BankCount < 2 {
		t.Fatalf("TECHNO must have >= 2 banks for this guard; got %d", st.a.containerInfo.BankCount)
	}
	beforeCount := st.a.containerInfo.BankCount
	secondName := st.a.layout.BankName(1)
	if secondName == "" {
		t.Fatalf("expected a named second bank in TECHNO; fixture changed")
	}

	st.a = st.a.deleteBank(0)

	// Banks shift up immediately and bank 0 stays valid; the deletion is
	// realised in memory as a trailing empty bank (the slot count is only
	// reclaimed at save time, which keeps the op a fixed-length, undoable
	// patch batch).
	data := st.a.containerModel.Bytes()
	if got := bankNameAt(data, 0); got != secondName {
		t.Errorf("after deleting bank 0, new bank 0 = %q, want %q (banks shift up)", got, secondName)
	}
	lastBstep := binary.LittleEndian.Uint16(data[(beforeCount-1)*disk.SectorSize+disk.BankVoiceCountOffset:])
	if lastBstep != 0 {
		t.Errorf("trailing bank bstep = %d after delete, want 0 (vacated slot)", lastBstep)
	}
	// F-QA-12: the in-memory disk must stay parseable.
	if _, _, err := voiceunpack.UnpackDataFromBytes(data); err != nil {
		t.Errorf("in-memory disk invalid after deleting bank 0: %v", err)
	}

	// Save and reload: the trailing empty bank is reclaimed and the shift
	// persisted.
	data2, info2 := saveReloadReparse(t, st)
	if info2.BankCount != beforeCount-1 {
		t.Errorf("reloaded BankCount = %d, want %d", info2.BankCount, beforeCount-1)
	}
	if got := bankNameAt(data2, 0); got != secondName {
		t.Errorf("reloaded bank 0 = %q, want %q", got, secondName)
	}
}

// TestDeleteBank_IsUndoable pins that deleting a bank is a single
// undoable step (it was a Replace-based resize at one point, which wiped
// the undo history). Ctrl-Z must restore the original bytes exactly.
func TestDeleteBank_IsUndoable(t *testing.T) {
	st := newJourneyWithFixture(t, "synthetic/TECHNO.img")
	if st.a.containerInfo.BankCount < 2 {
		t.Fatalf("TECHNO must have >= 2 banks; got %d", st.a.containerInfo.BankCount)
	}
	before := append([]byte(nil), st.a.containerModel.Bytes()...)

	st.a = st.a.deleteBank(0)
	if !st.a.containerModel.CanUndo() {
		t.Fatal("deleting a bank must be undoable")
	}
	if err := st.a.containerModel.Undo(); err != nil {
		t.Fatalf("undo after delete-bank: %v", err)
	}
	if !bytes.Equal(st.a.containerModel.Bytes(), before) {
		t.Error("undo of delete-bank did not restore the original bytes")
	}
}

// TestAssign_NonContiguousGapsRenderEmpty pins F-QA-6: assigning a voice
// to an area past the bank's current extent (when slot 0 is occupied)
// must render the intervening gap areas as empty, not as degenerate
// copies of slot 0's voice with a zero key range. The target area gets
// the new voice; the originally-assigned area is untouched.
func TestAssign_NonContiguousGapsRenderEmpty(t *testing.T) {
	a, _ := newTestAppEmpty(t)
	a = a.assignPoolEntryToArea(&pool.Entry{Name: "ONE", Bytes: testFZV("ONE")}, 0, 0)
	// Target A12 (area 11) with only A01 filled: a 10-area gap over an
	// occupied slot 0.
	a = a.assignPoolEntryToArea(&pool.Entry{Name: qaNameTwo, Bytes: testFZV(qaNameTwo)}, 0, 11)

	if got := a.layout.VoiceName(0, 0); got != "ONE" {
		t.Errorf("area 0 voice = %q, want ONE (original assignment preserved)", got)
	}
	if got := a.layout.VoiceName(0, 11); got != qaNameTwo {
		t.Errorf("area 11 voice = %q, want TWO (lands at the chosen area)", got)
	}
	for k := 1; k <= 10; k++ {
		if got := a.layout.VoiceName(0, k); got != "" && got != qaEmptyLabel {
			t.Errorf("gap area %d = %q, want empty (must not show slot 0's voice)", k+1, got)
		}
	}

	// Persistence: the report confirmed the junk survived save+reload, so
	// verify the gaps stay empty on disk too.
	target := filepath.Join(t.TempDir(), "GAPTEST.img")
	saved, _ := a.doSaveTo(target)
	_ = saved
	m, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("reload after save: %v", err)
	}
	rl := New(t.TempDir())
	rl.layout.SetContainer(m, info)
	if got := rl.layout.VoiceName(0, 11); got != qaNameTwo {
		t.Errorf("reloaded area 11 = %q, want TWO", got)
	}
	for k := 1; k <= 10; k++ {
		if got := rl.layout.VoiceName(0, k); got != "" && got != qaEmptyLabel {
			t.Errorf("reloaded gap area %d = %q, want empty (junk persisted to disk)", k+1, got)
		}
	}
}

// TestAssign_PromotionPreservesOriginalRange pins F-QA-20: assigning a
// second voice into a wrapped single-voice disk promotes it to a full
// dump, which moves the playable key range from the voice header into
// the bank's per-area table. The original area (0) must be seeded from
// its voice header instead of collapsing to a zero (C-1..C-1, velocity
// off) range that leaves the original voice unplayable.
func TestAssign_PromotionPreservesOriginalRange(t *testing.T) {
	st := newJourneyWithFixture(t, "synthetic/HOOVER.img")
	if !st.a.containerInfo.WrappedVoice {
		t.Fatalf("HOOVER must be a wrapped single voice; fixture changed")
	}
	d := st.a.containerModel.Bytes()
	vOff := disk.VoiceSlotOffset(st.a.containerInfo.BankCount*disk.SectorSize, 0)
	wantLow := d[vOff+disk.VoiceKeyLowOffset]
	wantHigh := d[vOff+disk.VoiceKeyHighOffset]
	wantRoot := d[vOff+disk.VoiceKeyCentOffset]
	if wantLow == 0 && wantHigh == 0 {
		t.Fatalf("HOOVER voice header carries no key range; fixture changed")
	}

	st.a = st.a.assignPoolEntryToArea(&pool.Entry{Name: qaNameTwo, Bytes: testFZV(qaNameTwo)}, 0, 1)

	d2 := st.a.containerModel.Bytes()
	if d2[disk.BankKeyLowOffset] != wantLow || d2[disk.BankKeyHighOffset] != wantHigh ||
		d2[disk.BankKeyCentOffset] != wantRoot {
		t.Errorf("after promotion, area 0 range = (low %d, high %d, root %d), want (%d, %d, %d) from the voice header",
			d2[disk.BankKeyLowOffset], d2[disk.BankKeyHighOffset], d2[disk.BankKeyCentOffset],
			wantLow, wantHigh, wantRoot)
	}
	if d2[disk.BankVelHighOffset] == 0 {
		t.Errorf("after promotion, area 0 velocity high = 0 (velocity off); want a full default range")
	}
}

// TestAssign_UpdatesVoiceCountOnWrappedVoice pins the stale header
// voice-count: a wrapped single voice has no parsed Header, so adding a
// second voice left containerInfo.VoiceCount at 1 ("1 voice" in the
// header) even though the disk now holds two.
func TestAssign_UpdatesVoiceCountOnWrappedVoice(t *testing.T) {
	st := newJourneyWithFixture(t, "synthetic/HOOVER.img")
	if !st.a.containerInfo.WrappedVoice || st.a.containerInfo.VoiceCount != 1 {
		t.Fatalf("HOOVER must be a wrapped single voice with VoiceCount 1; got wrapped=%v count=%d",
			st.a.containerInfo.WrappedVoice, st.a.containerInfo.VoiceCount)
	}
	st.a = st.a.assignPoolEntryToArea(&pool.Entry{Name: qaNameTwo, Bytes: testFZV(qaNameTwo)}, 0, 1)
	if st.a.containerInfo.VoiceCount != 2 {
		t.Errorf("after adding a 2nd voice, VoiceCount = %d, want 2 (stale header count)", st.a.containerInfo.VoiceCount)
	}
}

// TestDiskLabel_EditPersistsWithCase pins F-QA-21: the disk label is
// loaded, editable via the `l` gesture preserving mixed case/spaces, and
// persists across save + reload (Save no longer forces an uppercased
// filename-derived label).
func TestDiskLabel_EditPersistsWithCase(t *testing.T) {
	st := newJourneyWithFixture(t, "synthetic/TECHNO.img")
	if st.a.containerInfo.DiskLabel == "" {
		t.Fatalf("expected a disk label loaded from TECHNO.img")
	}
	st.a.current = minimap.Layout

	const want = "My Kit 3"
	st.a = pump(t, st.a, tea.KeyPressMsg{Code: 'l', Text: "l"}) // open disk-label rename
	if !st.a.renameActive || !st.a.renameDiskLabel {
		t.Fatalf("`l` did not open the disk-label rename modal")
	}
	for _, r := range want {
		st.a = pump(t, st.a, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	st.a = pump(t, st.a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if st.a.containerInfo.DiskLabel != want {
		t.Errorf("after rename, DiskLabel = %q, want %q (mixed case preserved)", st.a.containerInfo.DiskLabel, want)
	}

	_, info := saveReloadReparse(t, st)
	if info.DiskLabel != want {
		t.Errorf("reloaded DiskLabel = %q, want %q (persisted with case)", info.DiskLabel, want)
	}
}

// TestOpenSound_EmptyAreaRefuses pins F-QA-3: opening the Sound editor on
// an "(empty)" area must refuse rather than binding slot 0's voice (which
// the list never showed there), so the user can't unknowingly edit an
// unrelated voice.
func TestOpenSound_EmptyAreaRefuses(t *testing.T) {
	st := newJourneyWithFixture(t, "synthetic/TECHNO.img")
	emptyArea := st.a.bankBstep(0) // first area beyond the materialised ones
	if got := st.a.layout.VoiceName(0, emptyArea); got != "" && got != qaEmptyLabel {
		t.Fatalf("area %d expected empty, shows %q (fixture changed)", emptyArea, got)
	}
	st.a.current = minimap.Layout
	st.a = st.a.handleLayoutIntent(layout.Intent{
		Kind: layout.IntentOpenSound, BankIdx: 0, AreaIdx: emptyArea,
	})
	if st.a.current == minimap.Sound {
		t.Error("opening an empty area entered the Sound editor; should refuse")
	}
	if st.a.sound.HasVoice() {
		t.Error("Sound bound a voice for an empty area (would edit an unrelated voice)")
	}
}

// TestAssign_RefusesWhenOverCapacity pins F-QA-4: an assignment whose
// sample data won't fit the disk's free space must report the shortfall
// instead of silently no-op'ing (or over-filling past floppy capacity).
func TestAssign_RefusesWhenOverCapacity(t *testing.T) {
	a, _ := newTestAppEmpty(t)
	// A voice whose PCM alone exceeds the whole usable floppy area.
	huge := voiceimport.Encode(make([]int16, disk.UsableDataSize), 0, "HUGE", 0, voiceimport.NoLoop())
	before := a.containerModel.Len()
	a = a.assignPoolEntryToArea(&pool.Entry{Name: "HUGE", Bytes: huge}, 0, 0)
	if a.containerModel.Len() != before {
		t.Errorf("over-capacity assign changed the container (%d -> %d); want a refusal no-op",
			before, a.containerModel.Len())
	}
	got := strings.ToLower(stripANSI(a.status.View()))
	if !strings.Contains(got, "free space") && !strings.Contains(got, "fit") {
		t.Errorf("expected a not-enough-space message, got %q", got)
	}
}

// TestDeleteBank_RefusesLastBank pins that deleting the only remaining
// non-empty bank is refused: an FZ full dump must keep bank 0 with a
// valid bstep, so there is no valid "disk with zero banks" to save.
func TestDeleteBank_RefusesLastBank(t *testing.T) {
	st := newJourneyWithFixture(t, "synthetic/STAB.img")
	if st.a.containerInfo.BankCount != 1 {
		t.Fatalf("STAB must be single-bank for this guard; got %d", st.a.containerInfo.BankCount)
	}
	before := append([]byte(nil), st.a.containerModel.Bytes()...)

	st.a = st.a.deleteBank(0)

	if !bytes.Equal(st.a.containerModel.Bytes(), before) {
		t.Error("deleting the only bank must be a no-op (bytes unchanged)")
	}
	got := strings.ToLower(stripANSI(st.a.status.View()))
	if !strings.Contains(got, "only bank") {
		t.Errorf("expected a refusal mentioning the only bank, got %q", got)
	}
}
