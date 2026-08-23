package webcore

// Tests for documents the FZ firmware authored rather than fizzle,
// where only the DIS tail's vn identifies the live voices. They pin
// that the session reads it, edits under it, and writes it back.

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
)

// banklessVoiceName names the dump's fifth voice, the one in no bank.
const banklessVoiceName = "VOICE4"

// banklessDiskImage builds a disk holding the bankless-voice dump with
// a correct DIS tail (vn counts the bank-less fifth voice).
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
	entries, err := img.Directory()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.NameString() == disk.FullDumpName {
			sec, err := img.SectorRef(int(e.DisSector))
			if err != nil {
				t.Fatal(err)
			}
			dis, err := disk.DecodeDisSector(sec)
			if err != nil {
				t.Fatal(err)
			}
			return dis
		}
	}
	t.Fatal("no FULL-DATA-FZ on image")
	return disk.DisSector{}
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
