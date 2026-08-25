package fzf_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzf"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
)

func TestStandaloneDocumentOwnsBytesAndLayout(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(data, fzfbuilder.BanklessDumpVoices)
	doc, err := fzf.NewStandalone(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Layout().VoiceCountSource() != fzutil.VoiceCountMarker ||
		doc.Layout().VoiceCount() != fzfbuilder.BanklessDumpVoices {
		t.Fatalf("layout source/voices = %v/%d, want marker/%d",
			doc.Layout().VoiceCountSource(), doc.Layout().VoiceCount(), fzfbuilder.BanklessDumpVoices)
	}

	want := doc.Bytes()[0]
	data[0] ^= 0xff
	if got := doc.Bytes()[0]; got != want {
		t.Fatalf("document changed with constructor input: got %#x, want %#x", got, want)
	}
	returned := doc.Bytes()
	returned[0] ^= 0xff
	if got := doc.Bytes()[0]; got != want {
		t.Fatalf("document changed through returned bytes: got %#x, want %#x", got, want)
	}
}

func TestDiskFileDocumentRetainsDISAuthority(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	doc, err := fzf.NewDiskFile(data, fzfbuilder.BanklessDumpVoices)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Layout().VoiceCountSource() != fzutil.VoiceCountDIS ||
		doc.Layout().VoiceCount() != fzfbuilder.BanklessDumpVoices {
		t.Fatalf("layout source/voices = %v/%d, want DIS/%d",
			doc.Layout().VoiceCountSource(), doc.Layout().VoiceCount(), fzfbuilder.BanklessDumpVoices)
	}

	want := doc.Bytes()[0]
	data[0] ^= 0xff
	if got := doc.Bytes()[0]; got != want {
		t.Fatalf("document changed with constructor input: got %#x, want %#x", got, want)
	}
}

func TestDiskFileDocumentFallsBackFromUnsupportedDISCount(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	doc, err := fzf.NewDiskFile(data, 60)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Layout().VoiceCountSource() != fzutil.VoiceCountWalk {
		t.Fatalf("source = %v, want walk", doc.Layout().VoiceCountSource())
	}
}

func TestDocumentRejectsUnreadableBytes(t *testing.T) {
	if _, err := fzf.NewStandalone([]byte("not an FZF")); err == nil {
		t.Fatal("standalone document accepted unreadable bytes")
	}
	if _, err := fzf.NewDiskFile([]byte("not an FZF"), 1); err == nil {
		t.Fatal("disk-file document accepted unreadable bytes")
	}
}

func TestDiskFileDocumentRejectsInvalidDISCount(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	if _, err := fzf.NewDiskFile(data, 0); err == nil {
		t.Fatal("disk-file document accepted a zero DIS voice count")
	}
}

func TestRenameVoiceReturnsAtomicOperationAndRetainsMarkerAuthority(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(data, fzfbuilder.BanklessDumpVoices)
	doc, err := fzf.NewStandalone(data)
	if err != nil {
		t.Fatal(err)
	}
	before := doc.Bytes()

	result, err := doc.RenameVoice(0, "NEW NAME")
	if err != nil {
		t.Fatal(err)
	}
	stale := doc.Bytes()
	stale[doc.Layout().VoiceStart()+disk.VoiceNameOffset] ^= 0xff
	staleBefore := bytes.Clone(stale)
	if _, err := result.Apply(stale); err == nil {
		t.Fatal("expected stale voice pre-image to reject the name and marker batch")
	}
	if !bytes.Equal(stale, staleBefore) {
		t.Fatal("rejected rename mutated the stale document")
	}
	updated, err := result.Apply(doc.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := fzf.NewStandalone(updated)
	if err != nil {
		t.Fatal(err)
	}
	voice, err := reopened.Voice(0)
	if err != nil {
		t.Fatal(err)
	}
	if voice.Name() != "NEW NAME" {
		t.Fatalf("renamed voice = %q, want NEW NAME", voice.Name())
	}
	if !bytes.Equal(doc.Bytes(), before) {
		t.Fatal("rename mutated the original document")
	}
	if reopened.Layout().VoiceCountSource() != fzutil.VoiceCountMarker ||
		reopened.Layout().VoiceCount() != doc.Layout().VoiceCount() {
		t.Fatalf("reopened layout source/voices = %v/%d, want marker/%d",
			reopened.Layout().VoiceCountSource(), reopened.Layout().VoiceCount(), doc.Layout().VoiceCount())
	}
}

func TestRenameVoiceRejectsInvalidInputWithoutMutation(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	doc, err := fzf.NewDiskFile(data, fzfbuilder.BanklessDumpVoices)
	if err != nil {
		t.Fatal(err)
	}
	before := doc.Bytes()
	for _, test := range []struct {
		name  string
		slot  int
		value string
	}{
		{name: "empty name", slot: 0, value: ""},
		{name: "non-ASCII name", slot: 0, value: "BAD\u2603"},
		{name: "long name", slot: 0, value: "THIS NAME IS TOO LONG"},
		{name: "missing slot", slot: doc.Layout().VoiceCount(), value: "NAME"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := doc.RenameVoice(test.slot, test.value); err == nil {
				t.Fatal("RenameVoice accepted invalid input")
			}
			if !bytes.Equal(doc.Bytes(), before) {
				t.Fatal("failed rename mutated the document")
			}
		})
	}
}

func TestRenameBankReturnsAtomicOperationWithMarker(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(data, fzfbuilder.BanklessDumpVoices)
	doc, err := fzf.NewStandalone(data)
	if err != nil {
		t.Fatal(err)
	}
	result, err := doc.RenameBank(0, "NEW BANK")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := result.Apply(doc.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := fzf.NewStandalone(updated)
	if err != nil {
		t.Fatal(err)
	}
	bank, err := reopened.Bank(0)
	if err != nil {
		t.Fatal(err)
	}
	if bank.Name() != "NEW BANK" {
		t.Fatalf("renamed bank = %q, want NEW BANK", bank.Name())
	}
	if reopened.Layout().VoiceCountSource() != fzutil.VoiceCountMarker {
		t.Fatalf("reopened source = %v, want marker", reopened.Layout().VoiceCountSource())
	}
}

func TestRenameBankReturnsTypedValidationErrors(t *testing.T) {
	doc, err := fzf.NewDiskFile(fzfbuilder.MakeBanklessVoiceDump(t), fzfbuilder.BanklessDumpVoices)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		bank  int
		value string
		want  error
	}{
		{name: "empty", bank: 0, value: "", want: fzf.ErrBankNameEmpty},
		{name: "too long", bank: 0, value: "THIS NAME IS TOO LONG", want: fzf.ErrBankNameTooLong},
		{name: "not ASCII", bank: 0, value: "BAD☃", want: fzf.ErrBankNameNotASCII},
		{name: "bank index", bank: 99, value: "NAME", want: fzf.ErrBankIndexOutOfRange},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := doc.RenameBank(test.bank, test.value); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
	_, err = doc.RenameBank(0, "BAD☃")
	var nameErr *fzf.NameError
	if !errors.As(err, &nameErr) || nameErr.Character != '☃' {
		t.Fatalf("name error = %#v, want offending snowman", nameErr)
	}
}

func TestSwapAreasReturnsAtomicOperationAndRetainsMarker(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(data, fzfbuilder.BanklessDumpVoices)
	doc, err := fzf.NewStandalone(data)
	if err != nil {
		t.Fatal(err)
	}
	before, err := doc.Bank(1)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := before.VoiceSlot(0)
	second, _ := before.VoiceSlot(1)

	result, err := doc.SwapAreas(1, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := result.Apply(doc.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := fzf.NewStandalone(updated)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := reopened.Bank(1)
	gotFirst, _ := after.VoiceSlot(0)
	gotSecond, _ := after.VoiceSlot(1)
	if gotFirst != second || gotSecond != first {
		t.Fatalf("voice slots = %d, %d; want %d, %d", gotFirst, gotSecond, second, first)
	}
	if reopened.Layout().VoiceCountSource() != fzutil.VoiceCountMarker {
		t.Fatalf("reopened source = %v, want marker", reopened.Layout().VoiceCountSource())
	}
}

func TestSwapAreasRejectsInvalidIndicesWithoutMutation(t *testing.T) {
	data, _ := fzfbuilder.MakeTestFZF(t, []string{"LOW", "HIGH"})
	doc, err := fzf.NewStandalone(data)
	if err != nil {
		t.Fatal(err)
	}
	before := doc.Bytes()
	if _, err := doc.SwapAreas(1, 0, 1); !errors.Is(err, fzf.ErrBankIndexOutOfRange) {
		t.Fatalf("bank error = %v", err)
	}
	_, err = doc.SwapAreas(0, 0, 2)
	var indexErr *fzf.IndexError
	if !errors.Is(err, fzf.ErrAreaIndexOutOfRange) || !errors.As(err, &indexErr) {
		t.Fatalf("area error = %v", err)
	}
	if indexErr.Index != 2 || indexErr.Limit != 2 {
		t.Fatalf("area bounds = %d/%d, want index 2 limit 2", indexErr.Index, indexErr.Limit)
	}
	if !bytes.Equal(doc.Bytes(), before) {
		t.Fatal("invalid swap mutated the document")
	}
}

func TestDiskAreaAddRetainsExplicitVoiceGeometry(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	doc, err := fzf.NewDiskFile(data, fzfbuilder.BanklessDumpVoices)
	if err != nil {
		t.Fatal(err)
	}
	result, err := doc.AddArea(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := result.Apply(doc.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != len(data) {
		t.Fatalf("length = %d, want unchanged %d", len(updated), len(data))
	}
	reopened, err := fzf.NewDiskFile(updated, fzfbuilder.BanklessDumpVoices)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Layout().AudioStart() != doc.Layout().AudioStart() {
		t.Fatalf("audio start = %d, want %d", reopened.Layout().AudioStart(), doc.Layout().AudioStart())
	}
}

func TestAddThenDeleteAreaUsesStructuralReplacementAndPreservesAudio(t *testing.T) {
	data, _ := fzfbuilder.MakeTestFZF(t, []string{"ALPHA", "BETA", "GAMMA", "DELTA"})
	doc, err := fzf.NewStandalone(data)
	if err != nil {
		t.Fatal(err)
	}
	originalAudio := bytes.Clone(data[doc.Layout().AudioStart():])
	bank, _ := doc.Bank(0)
	originalAreas := bank.AreaCount()

	added, err := doc.AddArea(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !added.IsStructural() {
		t.Fatal("area add did not return a structural operation")
	}
	addedBytes, err := added.Apply(doc.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	withArea, err := fzf.NewStandalone(addedBytes)
	if err != nil {
		t.Fatal(err)
	}
	addedBank, _ := withArea.Bank(0)
	if addedBank.AreaCount() != originalAreas+1 {
		t.Fatalf("area count = %d, want %d", addedBank.AreaCount(), originalAreas+1)
	}
	if !bytes.Equal(addedBytes[withArea.Layout().AudioStart():], originalAudio) {
		t.Fatal("area add changed audio bytes")
	}

	deleted, err := withArea.DeleteArea(0, originalAreas)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.IsStructural() {
		t.Fatal("area delete did not return a structural operation")
	}
	restoredBytes, err := deleted.Apply(withArea.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	restored, err := fzf.NewStandalone(restoredBytes)
	if err != nil {
		t.Fatal(err)
	}
	restoredBank, _ := restored.Bank(0)
	if restoredBank.AreaCount() != originalAreas {
		t.Fatalf("restored area count = %d, want %d", restoredBank.AreaCount(), originalAreas)
	}
	if !bytes.Equal(restoredBytes[restored.Layout().AudioStart():], originalAudio) {
		t.Fatal("area delete changed audio bytes")
	}
}

func TestDuplicateAreaClonesVoiceAndPreservesAudioAcrossSectorGrowth(t *testing.T) {
	data, _ := fzfbuilder.MakeTestFZF(t, []string{"ONE", "TWO", "THREE", "FOUR"})
	doc, err := fzf.NewStandalone(data)
	if err != nil {
		t.Fatal(err)
	}
	originalAudio := bytes.Clone(data[doc.Layout().AudioStart():])
	sourceOffset := disk.VoiceSlotOffset(doc.Layout().VoiceStart(), 0)
	sourceHeader := bytes.Clone(data[sourceOffset : sourceOffset+disk.VoicePackSize])

	result, err := doc.DuplicateArea(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsStructural() {
		t.Fatal("duplicate did not return a structural operation")
	}
	updated, err := result.Apply(doc.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := fzf.NewStandalone(updated)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Layout().VoiceCount() != 5 {
		t.Fatalf("voice count = %d, want 5", reopened.Layout().VoiceCount())
	}
	bank, _ := reopened.Bank(0)
	if bank.AreaCount() != 5 {
		t.Fatalf("area count = %d, want 5", bank.AreaCount())
	}
	newSlot, err := bank.VoiceSlot(4)
	if err != nil || newSlot != 4 {
		t.Fatalf("new area voice = %d, %v; want slot 4", newSlot, err)
	}
	cloneOffset := disk.VoiceSlotOffset(reopened.Layout().VoiceStart(), newSlot)
	if !bytes.Equal(updated[cloneOffset:cloneOffset+disk.VoicePackSize], sourceHeader) {
		t.Fatal("new voice slot differs from the source header")
	}
	if !bytes.Equal(updated[reopened.Layout().AudioStart():], originalAudio) {
		t.Fatal("duplicate changed audio bytes")
	}
}

func TestDuplicateAreaRestampsMarkerAuthority(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(data, fzfbuilder.BanklessDumpVoices)
	doc, err := fzf.NewStandalone(data)
	if err != nil {
		t.Fatal(err)
	}
	result, err := doc.DuplicateArea(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := result.Apply(doc.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := fzf.NewStandalone(updated)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Layout().VoiceCountSource() != fzutil.VoiceCountMarker ||
		reopened.Layout().VoiceCount() != fzfbuilder.BanklessDumpVoices+1 {
		t.Fatalf("source/count = %v/%d, want marker/%d", reopened.Layout().VoiceCountSource(), reopened.Layout().VoiceCount(), fzfbuilder.BanklessDumpVoices+1)
	}
}

func TestDuplicateAreaReturnsTypedInvalidVoiceReference(t *testing.T) {
	data, _ := fzfbuilder.MakeTestFZF(t, []string{"BADREF", "SECOND"})
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset:], 99)
	doc, err := fzf.NewStandalone(data)
	if err != nil {
		t.Fatal(err)
	}
	_, err = doc.DuplicateArea(0, 0)
	var voiceErr *fzf.AreaVoiceError
	if !errors.As(err, &voiceErr) || voiceErr.Area != 0 || voiceErr.Voice != 99 || voiceErr.VoiceCount != 2 {
		t.Fatalf("voice reference error = %#v (%v)", voiceErr, err)
	}
}
