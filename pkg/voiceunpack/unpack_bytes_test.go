package voiceunpack

import (
	"testing"

	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
)

// A stamped dump unpacks all its voices, matching the browser.
func TestUnpackHonoursVoiceCountMarker(t *testing.T) {
	t.Parallel()
	dump := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(dump, fzfbuilder.BanklessDumpVoices)
	voices, _, err := UnpackDataFromBytes(dump)
	if err != nil {
		t.Fatal(err)
	}
	if len(voices) != fzfbuilder.BanklessDumpVoices {
		t.Errorf("voices = %d, want %d", len(voices), fzfbuilder.BanklessDumpVoices)
	}
}

func TestUnpackWithVoiceCountRejectsInvalidCount(t *testing.T) {
	t.Parallel()
	dump := fzfbuilder.MakeBanklessVoiceDump(t)
	if _, _, err := UnpackDataFromBytesWithVoiceCount(dump, 0); err == nil {
		t.Fatal("zero voice count was accepted")
	}
}
