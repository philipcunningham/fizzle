package webcore

// Opening documents the FZ firmware authored: the DIS tail's vn is
// read where it is the authority, and the harness below is shared by
// the voice_count_*_test.go files.

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskfs"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
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
	storeFullDump(t, img, dump, fzfbuilder.BanklessDumpVoices)
	return img.Bytes()
}

func storeFullDump(t *testing.T, img *disk.Image, dump []byte, voices int) {
	t.Helper()
	file, err := diskfs.FullDump(dump, 0, voices)
	if err != nil {
		t.Fatal(err)
	}
	if err := diskfs.Add(img, dump, file); err != nil {
		t.Fatal(err)
	}
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

// exportDISTail exports the document and decodes its FULL-DATA-FZ DIS
// tail, the step the edit tests repeat.
func exportDISTail(t *testing.T, s *Session) disk.DisSector {
	t.Helper()
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	return fullDumpDISTail(t, out)
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
