package voicepatch_test

import (
	"bytes"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/model"
	"github.com/philipcunningham/fizzle/pkg/voicepatch"
)

func TestResolveFZFSlotUsesResolvedLayoutAndFansOutKeyRange(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(data, fzfbuilder.BanklessDumpVoices)
	layout, err := fzutil.ResolveStandaloneFZFLayout(data)
	if err != nil {
		t.Fatal(err)
	}
	edits := []voicepatch.Edit{{Offset: disk.VoiceKeyLowOffset, Size: 1, Value: 42}}
	patches, err := voicepatch.ResolveFZFSlot(data, layout, 0, edits)
	if err != nil {
		t.Fatal(err)
	}
	updated := bytes.Clone(data)
	if err := model.Apply(updated, patches); err != nil {
		t.Fatal(err)
	}
	if got := updated[layout.VoiceStart()+disk.VoiceKeyLowOffset]; got != 42 {
		t.Fatalf("voice key low = %d, want 42", got)
	}
	if got := updated[disk.BankKeyLowOffset]; got != 42 {
		t.Fatalf("bank key low = %d, want 42", got)
	}
}

func TestResolveHeaderRejectsInvalidEditWithoutChangingInput(t *testing.T) {
	data := make([]byte, disk.VoiceHeaderUsed)
	before := bytes.Clone(data)
	edits := []voicepatch.Edit{{Offset: disk.VoiceHeaderUsed, Size: 1, Value: 1}}
	if _, err := voicepatch.ResolveHeader(data, 0, edits); err == nil {
		t.Fatal("ResolveHeader accepted an edit past the header")
	}
	if !bytes.Equal(data, before) {
		t.Fatal("rejected edit changed the input")
	}
}
