package webcore

// Edits over firmware-authored documents: the DIS counts survive, the
// bank-less voice stays editable, and grown slots come up empty.

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/model"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
)

func TestEditKeepsDISVoiceCount(t *testing.T) {
	t.Parallel()
	s, _ := openBanklessDisk(t)

	if _, cerr := s.RenameBank(0, "RENAMED"); cerr != nil {
		t.Fatalf("RenameBank: %v", cerr)
	}

	dis := exportDISTail(t, s)
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
	dis := exportDISTail(t, s)
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
	dis := exportDISTail(t, s)
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
	dis := exportDISTail(t, s)
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
	dis := exportDISTail(t, s)
	if got := int(dis.VoiceCount); got != 32 {
		t.Errorf("DIS vn after edit = %d, want the walked 32", got)
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
