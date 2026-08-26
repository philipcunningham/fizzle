package fzutil_test

import (
	"bytes"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
)

func TestResolveDiskFZFLayoutRetainsDISAuthority(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	layout, err := fzutil.ResolveDiskFZFLayout(data, fzfbuilder.BanklessDumpVoices)
	if err != nil {
		t.Fatal(err)
	}
	if layout.VoiceCountSource() != fzutil.VoiceCountDIS {
		t.Fatalf("source = %v, want DIS", layout.VoiceCountSource())
	}
	if layout.BankCount() != fzfbuilder.BanklessDumpBanks ||
		layout.VoiceCount() != fzfbuilder.BanklessDumpVoices ||
		layout.BStep0() != 1 ||
		layout.VoiceStart() != fzfbuilder.BanklessDumpBanks*disk.SectorSize ||
		layout.AudioStart() != layout.VoiceStart()+disk.VoiceAreaSectors(layout.VoiceCount())*disk.SectorSize {
		t.Fatalf("unexpected layout: banks=%d voices=%d bstep=%d voiceStart=%d audioStart=%d",
			layout.BankCount(), layout.VoiceCount(), layout.BStep0(), layout.VoiceStart(), layout.AudioStart())
	}
}

func TestResolveStandaloneFZFLayoutRetainsMarkerAuthority(t *testing.T) {
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(data, fzfbuilder.BanklessDumpVoices)
	layout, err := fzutil.ResolveStandaloneFZFLayout(data)
	if err != nil {
		t.Fatal(err)
	}
	if layout.VoiceCountSource() != fzutil.VoiceCountMarker || layout.VoiceCount() != fzfbuilder.BanklessDumpVoices {
		t.Fatalf("source/voices = %v/%d, want marker/%d",
			layout.VoiceCountSource(), layout.VoiceCount(), fzfbuilder.BanklessDumpVoices)
	}
}

func TestResolveFZFLayoutRejectsUnreadableDump(t *testing.T) {
	garbage := bytes.Repeat([]byte{0xff}, disk.SectorSize)
	if _, err := fzutil.ResolveStandaloneFZFLayout(garbage); err == nil {
		t.Fatal("standalone layout accepted garbage")
	}
	if _, err := fzutil.ResolveDiskFZFLayout(garbage, 3); err == nil {
		t.Fatal("disk layout accepted garbage")
	}
}
