package fzf_test

import (
	"encoding/binary"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzf"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/model"
)

func TestDocumentBankViewIsBounded(t *testing.T) {
	doc := banklessDocument(t)
	bank, err := doc.Bank(1)
	if err != nil {
		t.Fatal(err)
	}
	if bank.Index() != 1 || bank.Name() != "BANK TWO" || bank.AreaCount() != 3 {
		t.Fatalf("bank = index %d name %q areas %d", bank.Index(), bank.Name(), bank.AreaCount())
	}
	if got, err := bank.VoiceSlot(2); err != nil || got != 3 {
		t.Fatalf("area 2 voice = %d, %v; want 3", got, err)
	}
	if _, err := bank.VoiceSlot(3); err == nil {
		t.Fatal("bank accepted an area at its upper bound")
	}
	if _, err := doc.Bank(doc.Layout().BankCount()); err == nil {
		t.Fatal("document accepted a bank at its upper bound")
	}
}

func TestBankViewRejectsOutOfRangeVoicePointer(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset:], disk.MaxVoices)
	fzutil.StampVoiceCountMarker(data, fzfbuilder.BanklessDumpVoices)
	doc, err := fzf.NewStandalone(data)
	if err != nil {
		t.Fatal(err)
	}
	bank, err := doc.Bank(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bank.VoiceSlot(0); err == nil {
		t.Fatal("bank view accepted an out-of-range voice pointer")
	}
}

func TestDocumentVoiceViewIsBounded(t *testing.T) {
	doc := banklessDocument(t)
	voice, err := doc.Voice(4)
	if err != nil {
		t.Fatal(err)
	}
	if voice.Index() != 4 || voice.Name() != "VOICE4" ||
		voice.PlaybackMode() != disk.PlaybackModeNormal || voice.WaveEnd() <= voice.WaveStart() {
		t.Fatalf("unexpected voice: index=%d name=%q mode=%#x wave=%d..%d",
			voice.Index(), voice.Name(), voice.PlaybackMode(), voice.WaveStart(), voice.WaveEnd())
	}
	if _, err := doc.Voice(doc.Layout().VoiceCount()); err == nil {
		t.Fatal("document accepted a voice at its upper bound")
	}
}

func TestViewNamePatchesAreAbsoluteAndStaleSafe(t *testing.T) {
	doc := banklessDocument(t)
	bank, _ := doc.Bank(0)
	voice, _ := doc.Voice(0)
	bankPatch, err := bank.NamePatch("RENAMED BANK")
	if err != nil {
		t.Fatal(err)
	}
	voicePatch, err := voice.NamePatch("RENAMED")
	if err != nil {
		t.Fatal(err)
	}
	data := doc.Bytes()
	if err := model.Apply(data, []model.Patch{bankPatch, voicePatch}); err != nil {
		t.Fatal(err)
	}
	if got := disk.TrimPadded(data[disk.BankNameOffset : disk.BankNameOffset+disk.LabelSize]); got != "RENAMED BANK" {
		t.Fatalf("bank name = %q", got)
	}
	voiceName := doc.Layout().VoiceStart() + disk.VoiceNameOffset
	if got := disk.TrimPadded(data[voiceName : voiceName+disk.LabelSize]); got != "RENAMED" {
		t.Fatalf("voice name = %q", got)
	}
	if err := model.Apply(data, []model.Patch{voicePatch}); err == nil {
		t.Fatal("reapplying a stale view patch succeeded")
	}
	if _, err := voice.NamePatch("NAME THAT IS TOO LONG"); err == nil {
		t.Fatal("voice view accepted an overlong name")
	}
}

func TestDISViewReadsCountsAndBuildsPatch(t *testing.T) {
	sector := disk.EncodeDisSector(disk.DisSector{
		Extents:    [][2]uint16{{2, 8}},
		BankCount:  2,
		VoiceCount: 5,
		WaveCount:  7,
	})
	view, err := fzf.NewDISView(sector)
	if err != nil {
		t.Fatal(err)
	}
	if view.BankCount() != 2 || view.VoiceCount() != 5 || view.WaveCount() != 7 {
		t.Fatalf("counts = %d/%d/%d", view.BankCount(), view.VoiceCount(), view.WaveCount())
	}
	patch, err := view.VoiceCountPatch(6)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.Apply(sector, []model.Patch{patch}); err != nil {
		t.Fatal(err)
	}
	if got := int(sector[disk.DisVoiceCountOffset]); got != 6 {
		t.Fatalf("patched voice count low byte = %d, want 6", got)
	}
	if view.VoiceCount() != 6 {
		t.Fatalf("zero-copy view still reports voice count %d, want 6", view.VoiceCount())
	}
	if _, err := view.VoiceCountPatch(disk.MaxVoices + 1); err == nil {
		t.Fatal("DIS view accepted an out-of-range voice count")
	}
	if _, err := fzf.NewDISView(sector[:disk.SectorSize-1]); err == nil {
		t.Fatal("DIS view accepted a short sector")
	}
}

func banklessDocument(t *testing.T) *fzf.Document {
	t.Helper()
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(data, fzfbuilder.BanklessDumpVoices)
	doc, err := fzf.NewStandalone(data)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
