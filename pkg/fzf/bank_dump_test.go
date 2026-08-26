package fzf_test

import (
	"encoding/binary"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzf"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
)

func bankDumpBytes(t *testing.T, names []string) []byte {
	t.Helper()
	full, _ := fzfbuilder.MakeTestFZF(t, names)
	return full[:disk.SectorSize+disk.VoiceAreaSectors(len(names))*disk.SectorSize]
}

func TestBankDumpRetainsResolvedLayout(t *testing.T) {
	t.Parallel()
	doc, err := fzf.NewBankDump(bankDumpBytes(t, []string{"ONE", "TWO"}))
	if err != nil {
		t.Fatal(err)
	}
	layout := doc.Layout()
	if layout.VoiceCount() != 2 || layout.VoiceStart() != disk.SectorSize || layout.CountFromWalk() {
		t.Fatalf("layout = %+v, want stored two-voice geometry", layout)
	}
	voice, err := doc.Voice(1)
	if err != nil {
		t.Fatal(err)
	}
	if voice.Name() != "TWO" {
		t.Fatalf("voice name = %q, want TWO", voice.Name())
	}
}

func TestBankDumpWalkOverridesStaleStoredCount(t *testing.T) {
	t.Parallel()
	data := bankDumpBytes(t, []string{"ONE", "TWO"})
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], 5)
	slot2 := disk.VoiceSlotOffset(disk.SectorSize, 2)
	binary.LittleEndian.PutUint16(data[slot2+disk.VoiceLoopModeOffset:], 0xbeef)
	doc, err := fzf.NewBankDump(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Layout().VoiceCount() != 2 || !doc.Layout().CountFromWalk() {
		t.Fatalf("layout = %+v, want walked two-voice geometry", doc.Layout())
	}
}

func TestBankDumpRejectsTruncatedVoiceArea(t *testing.T) {
	t.Parallel()
	data := bankDumpBytes(t, []string{"ONE", "TWO"})
	_, err := fzf.NewBankDump(data[:disk.SectorSize+100])
	if err == nil {
		t.Fatal("NewBankDump accepted a truncated voice area")
	}
}

func TestBankDumpOwnsInputBytes(t *testing.T) {
	t.Parallel()
	data := bankDumpBytes(t, []string{"ONE"})
	doc, err := fzf.NewBankDump(data)
	if err != nil {
		t.Fatal(err)
	}
	data[disk.BankNameOffset] = 'X'
	if doc.Bank().Name() == "X" {
		t.Fatal("bank dump retained caller-owned bytes")
	}
}

func TestBankDumpMappedVoiceUsesBoundedBankArea(t *testing.T) {
	t.Parallel()
	data := bankDumpBytes(t, []string{"ONE"})
	data[disk.BankKeyLowOffset] = 12
	data[disk.BankKeyHighOffset] = 130
	data[disk.BankKeyCentOffset] = 60
	data[disk.BankVelLowOffset] = 2
	data[disk.BankVelHighOffset] = 100
	data[disk.BankMIDIRecvChanOffset] = 3
	data[disk.BankAudioOutOffset] = 1
	data[disk.BankVolumeOffset] = 9
	doc, err := fzf.NewBankDump(data)
	if err != nil {
		t.Fatal(err)
	}
	got, audible, err := doc.MappedVoice(0)
	if err != nil {
		t.Fatal(err)
	}
	if !audible {
		t.Fatal("mapped voice reported as a no-sound placeholder")
	}
	if got.Name != "ONE" || got.KeyLow != 12 || got.KeyHigh != disk.MaxMIDINote || got.RootKey != 60 ||
		got.VelocityLow != 2 || got.VelocityHigh != 100 || got.MIDIChannel != 4 || got.Output == "" || got.Volume != 9 {
		t.Fatalf("mapped voice = %+v", got)
	}
	if !doc.Bank().ShowsVelocity() || !doc.Bank().ShowsVolume() {
		t.Fatal("bank display hints did not observe mapped area fields")
	}
}
