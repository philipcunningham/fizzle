package webcore

// The exported voice-count marker: stamped in DIS mode, honoured on
// load, cleared when stale, and never invented over firmware bytes.

import (
	"bytes"
	"os"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
)

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
	dis := exportDISTail(t, s)
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
	dis := exportDISTail(t, s)
	if got := int(dis.VoiceCount); got != fzfbuilder.BanklessDumpVoices {
		t.Errorf("DIS vn = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
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

// A walk-mode extract of a dump with firmware garbage at the marker
// offset stays byte identical: only a real marker is cleared.
func TestWalkModeExtractKeepsGarbageBytes(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../testdata/synthetic/TECHNO.img")
	if err != nil {
		t.Fatal(err)
	}
	img, err := disk.ReadImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	want, err := diskget.FromImage(img, disk.FullDumpName)
	if err != nil {
		t.Fatal(err)
	}
	s := NewSession()
	if _, cerr := s.OpenImage(data); cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	got, cerr := s.ExtractFile(disk.FullDumpName)
	if cerr != nil {
		t.Fatalf("ExtractFile: %v", cerr)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("extract differs from disk get at byte 0x%x", firstDiff(got, want))
	}
}

func firstDiff(a, b []byte) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}
