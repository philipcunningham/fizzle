package webcore

// The session must read the DIS tail's vn, edit under it, and write
// it back.

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/model"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
)

// banklessVoiceName names the dump's fifth voice, the one in no bank.
const banklessVoiceName = "VOICE4"

// banklessDiskImage builds a disk holding the bankless-voice dump.
func banklessDiskImage(t *testing.T) []byte {
	t.Helper()
	data, err := diskformat.BuildImage("PREY")
	if err != nil {
		t.Fatal(err)
	}
	img, err := disk.ReadImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	dump := fzfbuilder.MakeBanklessVoiceDump(t)
	if err := diskadd.AddToImageWithVoiceCount(img, dump, 0, fzfbuilder.BanklessDumpVoices); err != nil {
		t.Fatal(err)
	}
	return img.Bytes()
}

func openBanklessDisk(t *testing.T) (*Session, Snapshot) {
	t.Helper()
	s := NewSession()
	snap, cerr := s.OpenImage(banklessDiskImage(t))
	if cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	return s, snap
}

func fullDumpDISTail(t *testing.T, imageData []byte) disk.DisSector {
	t.Helper()
	img, err := disk.ReadImage(bytes.NewReader(imageData))
	if err != nil {
		t.Fatal(err)
	}
	return fzfbuilder.FullDumpDISTail(t, img)
}

func TestOpenDiskWithBanklessVoice(t *testing.T) {
	t.Parallel()
	_, snap := openBanklessDisk(t)

	inst := snap.Disk.Instrument
	if inst == nil {
		t.Fatal("no instrument parsed")
	}
	if got := len(inst.Voices); got != fzfbuilder.BanklessDumpVoices {
		names := make([]string, len(inst.Voices))
		for i, v := range inst.Voices {
			names[i] = v.Name
		}
		t.Fatalf("voices = %d (%v), want %d (DIS vn must beat the bstep walk)",
			got, names, fzfbuilder.BanklessDumpVoices)
	}
	last := inst.Voices[len(inst.Voices)-1]
	if last.Name != banklessVoiceName {
		t.Errorf("last voice = %q, want %q (the bank-less voice)", last.Name, banklessVoiceName)
	}
	if last.Referenced {
		t.Error("bank-less voice reported as referenced")
	}

	// The dump holds 2 audio sectors; a walked count of 4 would size the
	// voice area one sector short and read a voice sector as audio.
	if got := snap.Disk.AudioBytes; got != 2*disk.SectorSize {
		t.Errorf("AudioBytes = %d, want %d", got, 2*disk.SectorSize)
	}
}

func TestEditKeepsDISVoiceCount(t *testing.T) {
	t.Parallel()
	s, _ := openBanklessDisk(t)

	if _, cerr := s.RenameBank(0, "RENAMED"); cerr != nil {
		t.Fatalf("RenameBank: %v", cerr)
	}

	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	dis := fullDumpDISTail(t, out)
	if got := int(dis.VoiceCount); got != fzfbuilder.BanklessDumpVoices {
		t.Errorf("DIS vn after edit = %d, want %d (bank-less voice lost on save)",
			got, fzfbuilder.BanklessDumpVoices)
	}
	dump := fzfbuilder.MakeBanklessVoiceDump(t)
	wantWn := disk.SectorsNeeded(len(dump)) - fzfbuilder.BanklessDumpBanks -
		disk.VoiceAreaSectors(fzfbuilder.BanklessDumpVoices)
	if got := int(dis.WaveCount); got != wantWn {
		t.Errorf("DIS wn after edit = %d, want %d", got, wantWn)
	}
}

func TestExtractBanklessVoiceSlot(t *testing.T) {
	t.Parallel()
	s, _ := openBanklessDisk(t)

	fzv, name, cerr := s.ExtractVoiceSlot(fzfbuilder.BanklessDumpVoices-1, ExtractFZV)
	if cerr != nil {
		t.Fatalf("ExtractVoiceSlot: %v", cerr)
	}
	if name != banklessVoiceName {
		t.Errorf("extracted name = %q, want %q", name, banklessVoiceName)
	}
	if len(fzv) == 0 {
		t.Error("extracted FZV is empty")
	}
	got := disk.TrimPadded(fzv[disk.VoiceNameOffset : disk.VoiceNameOffset+disk.LabelSize])
	if got != banklessVoiceName {
		t.Errorf("FZV header name = %q, want %q", got, banklessVoiceName)
	}
}

// A walk-mode edit stamps the parsed count, not the bstep sum.
func TestEditKeepsSharedVoiceKitDISCounts(t *testing.T) {
	t.Parallel()
	data, err := diskformat.BuildImage("KIT")
	if err != nil {
		t.Fatal(err)
	}
	img, err := disk.ReadImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	dump := fzfbuilder.MakeSharedVoiceDump(t)
	if err := diskadd.AddToImageWithVoiceCount(img, dump, 0, fzfbuilder.SharedVoiceDumpVoices); err != nil {
		t.Fatal(err)
	}

	s := NewSession()
	if _, cerr := s.OpenImage(img.Bytes()); cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	if _, cerr := s.RenameBank(0, "RENAMED"); cerr != nil {
		t.Fatalf("RenameBank: %v", cerr)
	}
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	dis := fullDumpDISTail(t, out)
	if got := int(dis.VoiceCount); got != fzfbuilder.SharedVoiceDumpVoices {
		t.Errorf("DIS vn after edit = %d, want %d (bstep sum stamped over the parsed count)",
			got, fzfbuilder.SharedVoiceDumpVoices)
	}
	wantWn := disk.SectorsNeeded(len(dump)) - 1 - disk.VoiceAreaSectors(fzfbuilder.SharedVoiceDumpVoices)
	if got := int(dis.WaveCount); got != wantWn {
		t.Errorf("DIS wn after edit = %d, want %d", got, wantWn)
	}
}

// A count-changing edit must stamp the new count.
func TestAddVoiceAdvancesDISCounts(t *testing.T) {
	t.Parallel()
	s, _ := openBanklessDisk(t)

	fzv := voiceimport.Encode(make([]int16, 1024), 1, "FRESH", 0, voiceimport.NoLoop())
	if _, cerr := s.AddVoice(fzv); cerr != nil {
		t.Fatalf("AddVoice: %v", cerr)
	}
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	dis := fullDumpDISTail(t, out)
	if got := int(dis.VoiceCount); got != fzfbuilder.BanklessDumpVoices+1 {
		t.Errorf("DIS vn after AddVoice = %d, want %d", got, fzfbuilder.BanklessDumpVoices+1)
	}
	fzf, gerr := s.ExtractFile(disk.FullDumpName)
	if gerr != nil {
		t.Fatalf("ExtractFile: %v", gerr)
	}
	wantWn := disk.SectorsNeeded(len(fzf)) - fzfbuilder.BanklessDumpBanks -
		disk.VoiceAreaSectors(fzfbuilder.BanklessDumpVoices+1)
	if got := int(dis.WaveCount); got != wantWn {
		t.Errorf("DIS wn after AddVoice = %d, want %d", got, wantWn)
	}
}

// DIS mode keeps a freed voice's slot, the way the firmware does.
func TestDeleteAreaKeepsDISCounts(t *testing.T) {
	t.Parallel()
	s, _ := openBanklessDisk(t)

	snap, cerr := s.DeleteArea(1, 0)
	if cerr != nil {
		t.Fatalf("DeleteArea: %v", cerr)
	}
	if got := len(snap.Disk.Instrument.Voices); got != fzfbuilder.BanklessDumpVoices {
		t.Errorf("voices after DeleteArea = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
	}
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	dis := fullDumpDISTail(t, out)
	if got := int(dis.VoiceCount); got != fzfbuilder.BanklessDumpVoices {
		t.Errorf("DIS vn after DeleteArea = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
	}
}

// makeBankFZB builds a .fzb whose areas play the given voice slots.
func makeBankFZB(t *testing.T, name string, slots ...int) []byte {
	t.Helper()
	voiceSectors := disk.VoiceAreaSectors(len(slots))
	fzb := make([]byte, (1+voiceSectors)*disk.SectorSize)
	binary.LittleEndian.PutUint16(fzb[disk.BankVoiceCountOffset:], uint16(len(slots))) //nolint:gosec // small test values
	padded := disk.PadLabel(name)
	copy(fzb[disk.BankNameOffset:], padded[:])
	for i, slot := range slots {
		binary.LittleEndian.PutUint16(fzb[disk.BankVoiceNumOffset+i*disk.VPEntrySize:], uint16(slot)) //nolint:gosec // small test values
		off := disk.VoiceSlotOffset(disk.SectorSize, i)
		binary.LittleEndian.PutUint16(fzb[off+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	}
	return fzb
}

// In DIS mode incoming areas are bounded by the count itself: slots
// past it are stale headers, and a smaller bank must still land.
func TestAddBankBoundsAreasByDISCount(t *testing.T) {
	t.Parallel()

	t.Run("refuses areas past the count", func(t *testing.T) {
		t.Parallel()
		s, _ := openBanklessDisk(t)
		fzb := makeBankFZB(t, "STALE BANK", 5, 6, 7)
		if _, cerr := s.AddBank(fzb, fzfbuilder.BanklessDumpBanks); cerr == nil {
			t.Fatal("expected refusal: the bank's areas play stale slots past the DIS count")
		}
	})

	t.Run("accepts a smaller bank playing an existing slot", func(t *testing.T) {
		t.Parallel()
		s, _ := openBanklessDisk(t)
		fzb := makeBankFZB(t, "SMALL BANK", fzfbuilder.BanklessDumpVoices-1)
		if _, cerr := s.AddBank(fzb, 1); cerr != nil {
			t.Fatalf("AddBank: %v", cerr)
		}
		out, cerr := s.ExportImage()
		if cerr != nil {
			t.Fatalf("ExportImage: %v", cerr)
		}
		dis := fullDumpDISTail(t, out)
		if got := int(dis.VoiceCount); got != fzfbuilder.BanklessDumpVoices {
			t.Errorf("DIS vn after bank replace = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
		}
	})
}

// A dump whose DIS vn is garbage must still open through the walk.
func TestOpenFallsBackOnCorruptDISVoiceCount(t *testing.T) {
	t.Parallel()
	imageData := banklessDiskImage(t)
	img, err := disk.ReadImage(bytes.NewReader(imageData))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := img.Directory()
	if err != nil {
		t.Fatal(err)
	}
	disOff := int(entries[0].DisSector) * disk.SectorSize
	binary.LittleEndian.PutUint16(img.Bytes()[disOff+disk.DisVoiceCountOffset:], 63)

	s := NewSession()
	snap, cerr := s.OpenImage(img.Bytes())
	if cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	if snap.Disk.Instrument == nil {
		t.Fatal("no instrument parsed after corrupt vn fallback")
	}
	if got := len(snap.Disk.Instrument.Voices); got != 4 {
		t.Errorf("voices = %d, want 4 (the walked count)", got)
	}
}

// A DIS count below the walk is an undercount: TECHNO.img says 30 of
// its 32 live voices.
func TestOpenKeepsVoicesPastLowDISCount(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../testdata/synthetic/TECHNO.img")
	if err != nil {
		t.Fatal(err)
	}
	s := NewSession()
	snap, cerr := s.OpenImage(data)
	if cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	voices := snap.Disk.Instrument.Voices
	if len(voices) != 32 {
		t.Fatalf("voices = %d, want 32 (a low DIS vn must not hide live voices)", len(voices))
	}
	if got := voices[30].Name; got != "PPGISH BASS1" {
		t.Errorf("slot 30 = %q, want PPGISH BASS1", got)
	}

	// A routine edit must not overwrite the live voices past vn.
	if _, cerr := s.DuplicateArea(0, 0); cerr != nil {
		t.Fatalf("DuplicateArea: %v", cerr)
	}
	snap = s.Snapshot()
	names := make(map[string]bool)
	for _, v := range snap.Disk.Instrument.Voices {
		names[v.Name] = true
	}
	if !names["PPGISH BASS1"] || !names["PPGISH BASS2"] {
		t.Error("PPGISH voices lost after DuplicateArea")
	}
}

// Adding an area and deleting it again must not flip the mode and
// drop the bank-less voice.
func TestAddThenDeleteAreaKeepsBanklessVoice(t *testing.T) {
	t.Parallel()
	s, _ := openBanklessDisk(t)

	if _, cerr := s.AddArea(0, 0); cerr != nil {
		t.Fatalf("AddArea: %v", cerr)
	}
	snap, cerr := s.DeleteArea(0, 1)
	if cerr != nil {
		t.Fatalf("DeleteArea: %v", cerr)
	}
	if got := len(snap.Disk.Instrument.Voices); got != fzfbuilder.BanklessDumpVoices {
		t.Errorf("voices after add+delete = %d, want %d (mode flipped mid-session)",
			got, fzfbuilder.BanklessDumpVoices)
	}
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	dis := fullDumpDISTail(t, out)
	if got := int(dis.VoiceCount); got != fzfbuilder.BanklessDumpVoices {
		t.Errorf("DIS vn after add+delete = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
	}
}

// The bank-less voice must be editable, not only listed.
func TestRenameBanklessVoiceSlot(t *testing.T) {
	t.Parallel()
	s, _ := openBanklessDisk(t)

	snap, cerr := s.RenameVoiceSlot(fzfbuilder.BanklessDumpVoices-1, "RENAMED")
	if cerr != nil {
		t.Fatalf("RenameVoiceSlot: %v", cerr)
	}
	voices := snap.Disk.Instrument.Voices
	if got := voices[len(voices)-1].Name; got != "RENAMED" {
		t.Errorf("renamed slot = %q, want RENAMED", got)
	}
}

// Extract then reload must keep the bank-less voice.
func TestExtractThenLoadKeepsBanklessVoice(t *testing.T) {
	t.Parallel()
	s, _ := openBanklessDisk(t)

	fzf, cerr := s.ExtractFile(disk.FullDumpName)
	if cerr != nil {
		t.Fatalf("ExtractFile: %v", cerr)
	}
	snap, cerr := s.LoadFZF(fzf)
	if cerr != nil {
		t.Fatalf("LoadFZF: %v", cerr)
	}
	if got := len(snap.Disk.Instrument.Voices); got != fzfbuilder.BanklessDumpVoices {
		t.Errorf("voices after extract and reload = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
	}
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	dis := fullDumpDISTail(t, out)
	if got := int(dis.VoiceCount); got != fzfbuilder.BanklessDumpVoices {
		t.Errorf("DIS vn after reload = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
	}
}

// A marker-stamped dump on a fresh disk exercises the add arm of the
// write-back.
func TestLoadMarkedDumpOnFreshDisk(t *testing.T) {
	t.Parallel()
	dump := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(dump, fzfbuilder.BanklessDumpVoices)

	s := NewSession()
	snap, cerr := s.LoadFZF(dump)
	if cerr != nil {
		t.Fatalf("LoadFZF: %v", cerr)
	}
	if got := len(snap.Disk.Instrument.Voices); got != fzfbuilder.BanklessDumpVoices {
		t.Errorf("voices = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
	}
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	dis := fullDumpDISTail(t, out)
	if got := int(dis.VoiceCount); got != fzfbuilder.BanklessDumpVoices {
		t.Errorf("DIS vn = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
	}
}

// The trusted-upward rule hands a low-tail disk to the walk, so an
// edit deliberately stamps the walked count over the firmware's tail:
// TECHNO's vn 30 becomes 32. Hiding live voices would be worse.
func TestEditRestampsLowDISCount(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../testdata/synthetic/TECHNO.img")
	if err != nil {
		t.Fatal(err)
	}
	s := NewSession()
	if _, cerr := s.OpenImage(data); cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	if _, cerr := s.RenameBank(0, "EDITED"); cerr != nil {
		t.Fatalf("RenameBank: %v", cerr)
	}
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	dis := fullDumpDISTail(t, out)
	if got := int(dis.VoiceCount); got != 32 {
		t.Errorf("DIS vn after edit = %d, want the walked 32", got)
	}
}

// Listing, audition, and edits share the session's mode: an edit that
// raises a bstep to the DIS count must not flip the listing to walk
// mode while the edit paths stay in DIS mode.
func TestListingStaysInDISModeAcrossEdits(t *testing.T) {
	t.Parallel()
	s, _ := openBanklessDisk(t)

	for range 2 {
		if _, cerr := s.AddArea(0, 0); cerr != nil {
			t.Fatalf("AddArea: %v", cerr)
		}
	}
	snap := s.Snapshot()
	if got := len(snap.Disk.Instrument.Voices); got != fzfbuilder.BanklessDumpVoices {
		names := make([]string, 0, got)
		for _, v := range snap.Disk.Instrument.Voices {
			names = append(names, v.Name)
		}
		t.Fatalf("voices after edits = %d (%v), want %d", got, names, fzfbuilder.BanklessDumpVoices)
	}
	if _, cerr := s.RenameVoiceSlot(fzfbuilder.BanklessDumpVoices-1, "STILL HERE"); cerr != nil {
		t.Fatalf("RenameVoiceSlot: %v", cerr)
	}
	if _, _, cerr := s.ExtractVoiceSlot(fzfbuilder.BanklessDumpVoices-1, ExtractFZV); cerr != nil {
		t.Fatalf("ExtractVoiceSlot: %v", cerr)
	}
}

// Undo and redo restore the mode the state was recorded under, not a
// re-derivation from bytes an edit had already moved.
func TestUndoRedoKeepDISMode(t *testing.T) {
	t.Parallel()
	s, _ := openBanklessDisk(t)

	if _, cerr := s.AddArea(0, 0); cerr != nil {
		t.Fatalf("AddArea: %v", cerr)
	}
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	if _, cerr := s.Redo(); cerr != nil {
		t.Fatalf("Redo: %v", cerr)
	}
	snap, cerr := s.DeleteArea(0, 1)
	if cerr != nil {
		t.Fatalf("DeleteArea: %v", cerr)
	}
	if got := len(snap.Disk.Instrument.Voices); got != fzfbuilder.BanklessDumpVoices {
		t.Fatalf("voices after undo redo delete = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
	}

	// The other door: deleting the dump and undoing it restores the
	// document with its mode.
	if _, cerr := s.DeleteFile(disk.FullDumpName); cerr != nil {
		t.Fatalf("DeleteFile: %v", cerr)
	}
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	snap, cerr = s.DeleteArea(1, 0)
	if cerr != nil {
		t.Fatalf("DeleteArea after undo: %v", cerr)
	}
	if got := len(snap.Disk.Instrument.Voices); got != fzfbuilder.BanklessDumpVoices {
		t.Fatalf("voices after dump-delete undo = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
	}
}

// Undoing a wholesale load restores the replaced document's mode.
func TestUndoOfLoadRestoresDISMode(t *testing.T) {
	t.Parallel()
	s, _ := openBanklessDisk(t)

	fzf := testFZF(t, "PLAIN1", "PLAIN2")
	if _, cerr := s.LoadFZF(fzf); cerr != nil {
		t.Fatalf("LoadFZF: %v", cerr)
	}
	if _, cerr := s.Undo(); cerr != nil {
		t.Fatalf("Undo: %v", cerr)
	}
	snap, cerr := s.DeleteArea(1, 0)
	if cerr != nil {
		t.Fatalf("DeleteArea: %v", cerr)
	}
	if got := len(snap.Disk.Instrument.Voices); got != fzfbuilder.BanklessDumpVoices {
		t.Fatalf("voices after load undo = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
	}
}

// A document that loses its DIS authority is in walk mode: rebuilding
// the instrument after deleting the dump must write counts a walk
// reader agrees with.
func TestRebuiltInstrumentWritesWalkCounts(t *testing.T) {
	t.Parallel()
	s, _ := openBanklessDisk(t)

	if _, cerr := s.DeleteFile(disk.FullDumpName); cerr != nil {
		t.Fatalf("DeleteFile: %v", cerr)
	}
	if _, cerr := s.NewInstrument("FRESH"); cerr != nil {
		t.Fatalf("NewInstrument: %v", cerr)
	}
	for range 3 {
		if _, cerr := s.AddArea(0, 0); cerr != nil {
			t.Fatalf("AddArea: %v", cerr)
		}
	}
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	fzf, cerr := s.ExtractFile(disk.FullDumpName)
	if cerr != nil {
		t.Fatalf("ExtractFile: %v", cerr)
	}
	walk, err := fzutil.ParseFZFHeader(fzf)
	if err != nil {
		t.Fatal(err)
	}
	dis := fullDumpDISTail(t, out)
	if int(dis.VoiceCount) != walk.NVoice {
		t.Errorf("DIS vn = %d, walk = %d; a walk-mode document must write the count a reader derives",
			dis.VoiceCount, walk.NVoice)
	}
}

// A corrupt DIS count falls back to the walk inside operations too.
func TestEditFallsBackOnCorruptDISVoiceCount(t *testing.T) {
	t.Parallel()
	fzf := fzfbuilder.MakeBanklessVoiceDump(t)
	out, outVN, cerr := patchDumpBytes(bytes.Clone(fzf), 63, func(_ *dumpState) ([]model.Patch, *Error) {
		return nil, nil
	})
	if cerr != nil {
		t.Fatalf("patchDumpBytes: %v", cerr)
	}
	walk, err := fzutil.ParseFZFHeader(out)
	if err != nil {
		t.Fatal(err)
	}
	if outVN != walk.NVoice {
		t.Errorf("outVN = %d, want the walked %d after corrupt-count fallback", outVN, walk.NVoice)
	}
}

// A walk-mode extract carries no marker, stale or otherwise.
func TestWalkModeExtractClearsStaleMarker(t *testing.T) {
	t.Parallel()
	dump := fzfbuilder.MakeSharedVoiceDump(t)
	fzutil.StampVoiceCountMarker(dump, fzfbuilder.SharedVoiceDumpVoices)
	data, err := diskformat.BuildImage("KIT")
	if err != nil {
		t.Fatal(err)
	}
	img, err := disk.ReadImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := diskadd.AddToImageWithVoiceCount(img, dump, 0, fzfbuilder.SharedVoiceDumpVoices); err != nil {
		t.Fatal(err)
	}
	s := NewSession()
	if _, cerr := s.OpenImage(img.Bytes()); cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	fzf, cerr := s.ExtractFile(disk.FullDumpName)
	if cerr != nil {
		t.Fatalf("ExtractFile: %v", cerr)
	}
	if string(fzf[disk.BankVoiceMarkerOffset:disk.BankVoiceMarkerOffset+2]) == "fz" {
		t.Error("walk-mode extract still carries a voice-count marker")
	}
}

// A slot the voice area grows over must come up empty: adopting a
// stale header verbatim resurrects a deleted voice.
func TestGrownSlotIsCleared(t *testing.T) {
	t.Parallel()
	// One bank playing two voices, a stale but plausible third slot,
	// non-zero audio: walk mode with bstep as the bound.
	fzf := make([]byte, 4*disk.SectorSize)
	binary.LittleEndian.PutUint16(fzf[disk.BankVoiceCountOffset:], 2)
	name := disk.PadLabel("GROW BANK")
	copy(fzf[disk.BankNameOffset:], name[:])
	for i, slot := range []int{0, 1} {
		binary.LittleEndian.PutUint16(fzf[disk.BankVoiceNumOffset+i*disk.VPEntrySize:], uint16(slot)) //nolint:gosec // 0..1
	}
	for i := range 3 {
		off := disk.VoiceSlotOffset(disk.SectorSize, i)
		binary.LittleEndian.PutUint16(fzf[off+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
		voiceName := disk.PadLabel("GROWN" + string(rune('0'+i)))
		copy(fzf[off+disk.VoiceNameOffset:], voiceName[:])
	}
	for i := 2 * disk.SectorSize; i < len(fzf); i++ {
		fzf[i] = 0xAB
	}

	out, _, cerr := patchDumpBytes(fzf, 0, func(d *dumpState) ([]model.Patch, *Error) {
		return addAreaPatches(d, 0, 0)
	})
	if cerr != nil {
		t.Fatalf("patchDumpBytes: %v", cerr)
	}
	off := disk.VoiceSlotOffset(disk.SectorSize, 2)
	slot := out[off : off+disk.VoicePackSize]
	for _, b := range slot {
		if b != 0 {
			t.Fatalf("grown slot holds stale bytes: % x", slot[:16])
		}
	}
}
