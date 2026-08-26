package integration_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzf"
	"github.com/philipcunningham/fizzle/pkg/model"
)

// TestCorpusDocumentInvariants exercises properties that must hold for every
// real-hardware full dump, while detailed JSON goldens remain the reviewable
// record for representative output behavior.
func TestCorpusDocumentInvariants(t *testing.T) {
	skipShort(t)
	requireFullCorpus(t)
	walkCorpus(t, []string{".fzf"}, func(t *testing.T, rel, path string) { //nolint:goconst // The extension is clearer at each independent corpus entry point.
		data, err := os.ReadFile(path) //nolint:gosec // path belongs to the verified fixture tree.
		if err != nil {
			t.Fatal(err)
		}
		document, err := fzf.NewStandalone(data)
		if err != nil {
			t.Fatalf("construct %s: %v", rel, err)
		}
		if !bytes.Equal(document.Bytes(), data) {
			t.Fatal("construction changed source bytes")
		}
		layout := document.Layout()
		if layout.VoiceStart() != layout.BankCount()*disk.SectorSize || layout.AudioStart() < layout.VoiceStart() || layout.AudioStart() > len(data) {
			t.Fatalf("layout bounds: banks=%d voices=%d voiceStart=%d audioStart=%d size=%d", layout.BankCount(), layout.VoiceCount(), layout.VoiceStart(), layout.AudioStart(), len(data))
		}
		for bankIndex := range layout.BankCount() {
			bank, err := document.Bank(bankIndex)
			if err != nil {
				t.Fatal(err)
			}
			for areaIndex := range bank.AreaCount() {
				// Some shareware dumps retain corrupt or unused voice pointers.
				// The invariant is that the bounded view rejects them rather than
				// exposing memory outside the resolved document.
				_, _ = bank.Area(areaIndex)
			}
		}
		for voiceIndex := range layout.VoiceCount() {
			if _, err := document.Voice(voiceIndex); err != nil {
				t.Fatalf("voice %d: %v", voiceIndex, err)
			}
		}
		assertTargetedBankNamePatch(t, document)
	})
}

func assertTargetedBankNamePatch(t *testing.T, document *fzf.Document) {
	t.Helper()
	bank, err := document.Bank(0)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.Repeat("X", disk.LabelSize)
	patch, err := bank.NamePatch(name)
	if err != nil {
		t.Fatal(err)
	}
	before := document.Bytes()
	after := bytes.Clone(before)
	if err := model.Apply(after, []model.Patch{patch}); err != nil {
		t.Fatal(err)
	}
	start := disk.BankNameOffset
	end := start + disk.LabelSize
	if !bytes.Equal(before[:start], after[:start]) || !bytes.Equal(before[end:], after[end:]) {
		t.Fatal("bank name patch changed bytes outside its bounded field")
	}
}
