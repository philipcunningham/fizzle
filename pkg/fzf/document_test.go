package fzf_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzf"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
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

func TestAddVoiceReturnsOneAtomicStructuralOperation(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	doc, err := fzf.NewDiskFile(data, fzfbuilder.BanklessDumpVoices)
	if err != nil {
		t.Fatal(err)
	}
	voice := testutil.MakeTestVoice("JOINED", 321)
	before := doc.Bytes()

	result, err := doc.AddVoice(voice)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsStructural() {
		t.Fatal("AddVoice returned a fixed-size operation")
	}
	if !bytes.Equal(doc.Bytes(), before) {
		t.Fatal("AddVoice mutated its document")
	}
	stale := bytes.Clone(before)
	stale[0] ^= 0xff
	if _, err := result.Apply(stale); err == nil {
		t.Fatal("AddVoice accepted a stale document")
	}

	updated, err := result.Apply(before)
	if err != nil {
		t.Fatal(err)
	}
	voiceCount, audioStart, ok := result.VoiceGeometry()
	if !ok || voiceCount != fzfbuilder.BanklessDumpVoices+1 {
		t.Fatalf("geometry = %d voices at %d, %v", voiceCount, audioStart, ok)
	}
	reopened, err := fzf.NewDiskFile(updated, voiceCount)
	if err != nil {
		t.Fatal(err)
	}
	joined, err := reopened.Voice(voiceCount - 1)
	if err != nil {
		t.Fatal(err)
	}
	if joined.Name() != "JOINED" {
		t.Fatalf("joined voice = %q, want JOINED", joined.Name())
	}
	if got, want := len(updated)-audioStart, len(before)-doc.Layout().AudioStart()+len(voice)-disk.SectorSize; got != want {
		t.Fatalf("audio bytes = %d, want %d", got, want)
	}
}

func TestAddVoiceRejectsMalformedInputWithoutMutation(t *testing.T) {
	doc, err := fzf.NewDiskFile(fzfbuilder.MakeBanklessVoiceDump(t), fzfbuilder.BanklessDumpVoices)
	if err != nil {
		t.Fatal(err)
	}
	before := doc.Bytes()
	for _, test := range []struct {
		name string
		data []byte
		want error
	}{
		{name: "short", data: make([]byte, disk.SectorSize-1), want: fzf.ErrVoiceFileTooShort},
		{name: "misaligned PCM", data: make([]byte, disk.SectorSize+1), want: fzf.ErrVoicePCMMisaligned},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := doc.AddVoice(test.data); !errors.Is(err, test.want) {
				t.Fatalf("AddVoice error = %v, want %v", err, test.want)
			}
			if !bytes.Equal(doc.Bytes(), before) {
				t.Fatal("rejected AddVoice mutated the document")
			}
		})
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

func TestEditVoiceReturnsAtomicOperationAndRetainsLayoutAuthority(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(data, fzfbuilder.BanklessDumpVoices)
	doc, err := fzf.NewStandalone(data)
	if err != nil {
		t.Fatal(err)
	}
	before := doc.Bytes()
	edits, err := voiceedit.BuildKeyRangePatch(41, voiceedit.Unchanged, voiceedit.Unchanged)
	if err != nil {
		t.Fatal(err)
	}

	result, err := doc.EditVoice(0, edits)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsStructural() {
		t.Fatal("EditVoice returned a structural operation")
	}
	if !bytes.Equal(doc.Bytes(), before) {
		t.Fatal("EditVoice mutated its document")
	}
	stale := bytes.Clone(before)
	stale[doc.Layout().VoiceStart()+disk.VoiceKeyLowOffset] ^= 0xff
	staleBefore := bytes.Clone(stale)
	if _, err := result.Apply(stale); err == nil {
		t.Fatal("EditVoice accepted a stale voice header")
	}
	if !bytes.Equal(stale, staleBefore) {
		t.Fatal("rejected EditVoice mutated the stale document")
	}

	updated, err := result.Apply(before)
	if err != nil {
		t.Fatal(err)
	}
	if got := updated[doc.Layout().VoiceStart()+disk.VoiceKeyLowOffset]; got != 41 {
		t.Fatalf("voice key low = %d, want 41", got)
	}
	if got := updated[disk.BankKeyLowOffset]; got != 41 {
		t.Fatalf("bank key low = %d, want 41", got)
	}
	reopened, err := fzf.NewStandalone(updated)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Layout().VoiceCountSource() != fzutil.VoiceCountMarker {
		t.Fatalf("authority = %v, want marker", reopened.Layout().VoiceCountSource())
	}
}

func TestEditVoiceRejectsInvalidSlotAndEditWithoutMutation(t *testing.T) {
	doc, err := fzf.NewDiskFile(fzfbuilder.MakeBanklessVoiceDump(t), fzfbuilder.BanklessDumpVoices)
	if err != nil {
		t.Fatal(err)
	}
	before := doc.Bytes()
	if _, err := doc.EditVoice(99, nil); !errors.Is(err, fzf.ErrVoiceIndexOutOfRange) {
		t.Fatalf("bad slot error = %v, want ErrVoiceIndexOutOfRange", err)
	}
	bad := []voiceedit.Edit{{Offset: disk.VoiceHeaderUsed, Size: 1, Value: 1}}
	if _, err := doc.EditVoice(0, bad); err == nil {
		t.Fatal("EditVoice accepted an out-of-range edit")
	}
	if !bytes.Equal(doc.Bytes(), before) {
		t.Fatal("rejected EditVoice mutated the document")
	}
}

func TestSetAreaFieldOwnsClampingWidthsAndMarkerAuthority(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(data, fzfbuilder.BanklessDumpVoices)
	for _, test := range []struct {
		name   string
		field  fzf.AreaField
		value  int
		offset int
		want   []byte
	}{
		{name: "key low clamps", field: fzf.AreaKeyLow, value: 900, offset: disk.BankKeyLowOffset, want: []byte{127}},
		{name: "key high clamps", field: fzf.AreaKeyHigh, value: -1, offset: disk.BankKeyHighOffset, want: []byte{0}},
		{name: "root clamps", field: fzf.AreaRootKey, value: 64, offset: disk.BankKeyCentOffset, want: []byte{64}},
		{name: "velocity low has floor", field: fzf.AreaVelocityLow, value: 0, offset: disk.BankVelLowOffset, want: []byte{disk.MinVelocity}},
		{name: "velocity high clamps", field: fzf.AreaVelocityHigh, value: 900, offset: disk.BankVelHighOffset, want: []byte{127}},
		{name: "volume converts display scale", field: fzf.AreaVolume, value: 99, offset: disk.BankVolumeOffset, want: []byte{disk.AreaLevelToByte(99)}},
		{name: "MIDI channel converts display scale", field: fzf.AreaMIDIChannel, value: 16, offset: disk.BankMIDIRecvChanOffset, want: []byte{15}},
		{name: "output fills byte", field: fzf.AreaOutput, value: 900, offset: disk.BankAudioOutOffset, want: []byte{255}},
		{name: "voice slot keeps word width", field: fzf.AreaVoiceSlot, value: 1, offset: disk.BankVoiceNumOffset, want: []byte{1, 0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, err := fzf.NewStandalone(data)
			if err != nil {
				t.Fatal(err)
			}
			result, err := doc.SetAreaField(0, 0, test.field, test.value)
			if err != nil {
				t.Fatal(err)
			}
			updated, err := result.Apply(doc.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if got := updated[test.offset : test.offset+len(test.want)]; !bytes.Equal(got, test.want) {
				t.Fatalf("stored bytes = %v, want %v", got, test.want)
			}
			reopened, err := fzf.NewStandalone(updated)
			if err != nil {
				t.Fatal(err)
			}
			if reopened.Layout().VoiceCountSource() != fzutil.VoiceCountMarker {
				t.Fatalf("authority = %v, want marker", reopened.Layout().VoiceCountSource())
			}
		})
	}
}

func TestSetAreaFieldRejectsInvalidTargetsWithoutMutation(t *testing.T) {
	doc, err := fzf.NewDiskFile(fzfbuilder.MakeBanklessVoiceDump(t), fzfbuilder.BanklessDumpVoices)
	if err != nil {
		t.Fatal(err)
	}
	before := doc.Bytes()
	for _, test := range []struct {
		name  string
		bank  int
		area  int
		field fzf.AreaField
		value int
		want  error
	}{
		{name: "bank", bank: 99, field: fzf.AreaKeyLow, want: fzf.ErrBankIndexOutOfRange},
		{name: "area", area: 99, field: fzf.AreaKeyLow, want: fzf.ErrAreaIndexOutOfRange},
		{name: "voice", field: fzf.AreaVoiceSlot, value: 99, want: fzf.ErrVoiceIndexOutOfRange},
		{name: "field", field: fzf.AreaField(255), want: fzf.ErrAreaFieldInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := doc.SetAreaField(test.bank, test.area, test.field, test.value); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if !bytes.Equal(doc.Bytes(), before) {
				t.Fatal("rejected field edit mutated document")
			}
		})
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
