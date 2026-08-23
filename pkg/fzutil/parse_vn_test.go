package fzutil_test

import (
	"encoding/binary"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
)

func TestParseFZFHeaderWithVoiceCount(t *testing.T) {
	t.Parallel()
	data := fzfbuilder.MakeBanklessVoiceDump(t)

	// The walk undercounts a dump holding a voice no bank references;
	// that undercount is why the vn-aware parse exists.
	walked, err := fzutil.ParseFZFHeader(data)
	if err != nil {
		t.Fatalf("ParseFZFHeader: %v", err)
	}
	if walked.NVoice != fzfbuilder.BanklessDumpVoices-1 {
		t.Fatalf("walked NVoice = %d, want %d", walked.NVoice, fzfbuilder.BanklessDumpVoices-1)
	}

	t.Run("trusts the DIS voice count", func(t *testing.T) {
		t.Parallel()
		h, err := fzutil.ParseFZFHeaderWithVoiceCount(data, fzfbuilder.BanklessDumpVoices)
		if err != nil {
			t.Fatalf("ParseFZFHeaderWithVoiceCount: %v", err)
		}
		if h.NVoice != fzfbuilder.BanklessDumpVoices {
			t.Errorf("NVoice = %d, want %d", h.NVoice, fzfbuilder.BanklessDumpVoices)
		}
		if h.NBankSectors != fzfbuilder.BanklessDumpBanks {
			t.Errorf("NBankSectors = %d, want %d", h.NBankSectors, fzfbuilder.BanklessDumpBanks)
		}
		if h.VoiceAreaStart != fzfbuilder.BanklessDumpBanks*disk.SectorSize {
			t.Errorf("VoiceAreaStart = %d, want %d", h.VoiceAreaStart, fzfbuilder.BanklessDumpBanks*disk.SectorSize)
		}
		if h.BStep0 != 1 {
			t.Errorf("BStep0 = %d, want 1", h.BStep0)
		}
	})

	t.Run("rejects a voice count out of range", func(t *testing.T) {
		t.Parallel()
		if _, err := fzutil.ParseFZFHeaderWithVoiceCount(data, 0); err == nil {
			t.Error("vn=0: expected error, got nil")
		}
		if _, err := fzutil.ParseFZFHeaderWithVoiceCount(data, disk.MaxVoices+1); err == nil {
			t.Error("vn=65: expected error, got nil")
		}
	})

	t.Run("rejects a voice area running past the dump", func(t *testing.T) {
		t.Parallel()
		if _, err := fzutil.ParseFZFHeaderWithVoiceCount(data, disk.MaxVoices); err == nil {
			t.Error("expected error for voice area past the dump, got nil")
		}
	})

	t.Run("rejects a count claiming implausible slots", func(t *testing.T) {
		t.Parallel()
		garbled := append([]byte(nil), data...)
		off := disk.VoiceSlotOffset(fzfbuilder.BanklessDumpBanks*disk.SectorSize, fzfbuilder.BanklessDumpVoices-1)
		binary.LittleEndian.PutUint16(garbled[off+disk.VoiceLoopModeOffset:], 0xFFFF)
		if _, err := fzutil.ParseFZFHeaderWithVoiceCount(garbled, fzfbuilder.BanklessDumpVoices); err == nil {
			t.Error("expected error for implausible slot inside vn, got nil")
		}
	})
}

// The acceptance range's top end is geometry: zeroed audio reads as
// placeholder slots, so any count fitting the file validates, and the
// first count needing a voice area past the file is refused.
func TestParseFZFHeaderWithVoiceCountTopEnd(t *testing.T) {
	t.Parallel()
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	if _, err := fzutil.ParseFZFHeaderWithVoiceCount(data, 16); err != nil {
		t.Errorf("vn=16 (fills the file exactly): %v", err)
	}
	if _, err := fzutil.ParseFZFHeaderWithVoiceCount(data, 17); err == nil {
		t.Error("vn=17: expected geometry refusal, got nil")
	}
}

// A count at the marker offset means nothing without the magic.
func TestMarkerVoiceCountRequiresMagic(t *testing.T) {
	t.Parallel()
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	data[disk.BankVoiceMarkerOffset] = 0
	data[disk.BankVoiceMarkerOffset+1] = 0
	binary.LittleEndian.PutUint16(data[disk.BankVoiceMarkerOffset+2:], fzfbuilder.BanklessDumpVoices)
	if got := fzutil.MarkerVoiceCount(data); got != 0 {
		t.Errorf("MarkerVoiceCount without magic = %d, want 0", got)
	}
}
