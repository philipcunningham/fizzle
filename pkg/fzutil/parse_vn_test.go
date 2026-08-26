package fzutil_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
)

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

// The marker binds to the dump it describes: a structural edit or a
// length change after stamping invalidates it.
func TestMarkerBindsToTheDump(t *testing.T) {
	t.Parallel()
	base := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(base, fzfbuilder.BanklessDumpVoices)
	if got := fzutil.MarkerVoiceCount(base); got != fzfbuilder.BanklessDumpVoices {
		t.Fatalf("fresh marker = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
	}

	t.Run("voice header edit invalidates", func(t *testing.T) {
		t.Parallel()
		edited := append([]byte(nil), base...)
		off := disk.VoiceSlotOffset(fzfbuilder.BanklessDumpBanks*disk.SectorSize, 0)
		edited[off+disk.VoiceNameOffset] ^= 0x01
		if got := fzutil.MarkerVoiceCount(edited); got != 0 {
			t.Errorf("marker after header edit = %d, want 0", got)
		}
	})

	t.Run("length change invalidates", func(t *testing.T) {
		t.Parallel()
		grown := append(append([]byte(nil), base...), make([]byte, disk.SectorSize)...)
		if got := fzutil.MarkerVoiceCount(grown); got != 0 {
			t.Errorf("marker after growth = %d, want 0", got)
		}
	})

	t.Run("audio-only edit keeps the marker", func(t *testing.T) {
		t.Parallel()
		edited := append([]byte(nil), base...)
		edited[len(edited)-1] ^= 0xFF
		if got := fzutil.MarkerVoiceCount(edited); got != fzfbuilder.BanklessDumpVoices {
			t.Errorf("marker after audio edit = %d, want %d", got, fzfbuilder.BanklessDumpVoices)
		}
	})
}

// A disk's DIS candidate never falls through to an embedded marker:
// the two authorities have different lifetimes.
func TestResolveDiskFZFIgnoresMarker(t *testing.T) {
	t.Parallel()
	data := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(data, fzfbuilder.BanklessDumpVoices)
	layout, err := fzutil.ResolveDiskFZFLayout(data, 63)
	if err != nil {
		t.Fatal(err)
	}
	if layout.VoiceCountSource() != fzutil.VoiceCountWalk {
		t.Errorf("source = %v, want the walk (an unusable DIS count must not fall to the marker)", layout.VoiceCountSource())
	}
}

// ReadFZF resolves the marker, so every CLI reader built on it sees a
// stamped export's count.
func TestReadFZFHonoursMarker(t *testing.T) {
	t.Parallel()
	dump := fzfbuilder.MakeBanklessVoiceDump(t)
	fzutil.StampVoiceCountMarker(dump, fzfbuilder.BanklessDumpVoices)
	path := filepath.Join(t.TempDir(), "stamped.fzf")
	if err := os.WriteFile(path, dump, 0o644); err != nil {
		t.Fatal(err)
	}
	_, hdr, err := fzutil.ReadFZF(path)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.NVoice != fzfbuilder.BanklessDumpVoices {
		t.Errorf("NVoice = %d, want %d", hdr.NVoice, fzfbuilder.BanklessDumpVoices)
	}
}
