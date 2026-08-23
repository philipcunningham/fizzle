package webcore

// The parse mode is a session property: listings, edits, and rebuilt
// instruments agree on it.

import (
	"bytes"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/model"
)

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
	dis := exportDISTail(t, s)
	if got := int(dis.VoiceCount); got != fzfbuilder.BanklessDumpVoices {
		t.Errorf("DIS vn after add+delete = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
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

// What the editor shows is what a reopen shows: an edit that raises
// the bstep walk past the count must not let stale slots return as
// voices on the next open.
func TestReopenAfterEditsKeepsVoiceCount(t *testing.T) {
	t.Parallel()
	s, _ := openBanklessDisk(t)
	for range 2 {
		if _, cerr := s.AddArea(0, 0); cerr != nil {
			t.Fatalf("AddArea: %v", cerr)
		}
	}
	out, cerr := s.ExportImage()
	if cerr != nil {
		t.Fatalf("ExportImage: %v", cerr)
	}
	reopened := NewSession()
	snap, cerr := reopened.OpenImage(out)
	if cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	if got := len(snap.Disk.Instrument.Voices); got != fzfbuilder.BanklessDumpVoices {
		names := make([]string, 0, got)
		for _, v := range snap.Disk.Instrument.Voices {
			names = append(names, v.Name)
		}
		t.Fatalf("reopened voices = %d (%v), want %d", got, names, fzfbuilder.BanklessDumpVoices)
	}
}
