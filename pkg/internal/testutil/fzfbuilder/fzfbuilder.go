// Package fzfbuilder provides test helpers for building FZF files.
package fzfbuilder

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
)

// BanklessDumpVoices is the live voice count of the dump
// MakeBanklessVoiceDump builds; BanklessDumpBanks is its bank sectors.
const (
	BanklessDumpVoices = 5
	BanklessDumpBanks  = 2
)

// MakeBanklessVoiceDump builds a full dump shaped like the PREY.img
// fixture's: two banks whose bsteps sum to 4, five live voices
// (VOICE0..VOICE4, the fifth in no bank), and three stale but
// byte-plausible slots repeating VOICE1..VOICE3, then 2 audio
// sectors. The bstep-bounded walk reads it as 4 voices and a full
// slot walk as 8; only a DIS vn of 5 identifies the live ones.
func MakeBanklessVoiceDump(t *testing.T) []byte {
	t.Helper()
	voiceAreaSectors := disk.VoiceAreaSectors(8)
	data := make([]byte, BanklessDumpBanks*disk.SectorSize+voiceAreaSectors*disk.SectorSize+2*disk.SectorSize)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], 1)
	bankName := disk.PadLabel("BANK ONE")
	copy(data[disk.BankNameOffset:], bankName[:])
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset:], 0)
	binary.LittleEndian.PutUint16(data[disk.SectorSize+disk.BankVoiceCountOffset:], 3)
	bankName = disk.PadLabel("BANK TWO")
	copy(data[disk.SectorSize+disk.BankNameOffset:], bankName[:])
	for i := range 3 {
		binary.LittleEndian.PutUint16(
			data[disk.SectorSize+disk.BankVoiceNumOffset+i*disk.VPEntrySize:], uint16(i+1)) //nolint:gosec // G115: 1..3
	}
	voiceArea := BanklessDumpBanks * disk.SectorSize
	writeSlot := func(slot int, name string) {
		off := disk.VoiceSlotOffset(voiceArea, slot)
		binary.LittleEndian.PutUint16(data[off+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
		padded := disk.PadLabel(name)
		copy(data[off+disk.VoiceNameOffset:], padded[:])
	}
	for i := range BanklessDumpVoices {
		writeSlot(i, "VOICE"+string(rune('0'+i)))
	}
	for i := 1; i <= 3; i++ {
		writeSlot(BanklessDumpVoices-1+i, "VOICE"+string(rune('0'+i)))
	}
	return data
}

// MakeTestFZF assembles a minimal FZF with the given voice names and writes it to a temp file.
func MakeTestFZF(t *testing.T, names []string) ([]byte, string) {
	t.Helper()
	n := len(names)
	voices := make([][]byte, n)
	groups := make([]voicebuild.Keygroup, n)
	for i, name := range names {
		v := make([]byte, disk.SectorSize+512*2)
		padded := disk.PadLabel(name)
		copy(v[disk.VoiceNameOffset:], padded[:])
		binary.LittleEndian.PutUint32(v[disk.VoiceWaveStartOffset:], 0)
		binary.LittleEndian.PutUint32(v[disk.VoiceWaveEndOffset:], 512)
		binary.LittleEndian.PutUint32(v[disk.VoiceGenStartOffset:], 0)
		binary.LittleEndian.PutUint32(v[disk.VoiceGenEndOffset:], 512)
		binary.LittleEndian.PutUint16(v[disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
		voices[i] = v
		note := uint8(disk.FirstMIDINote + i)
		groups[i] = voicebuild.NewKeygroup(note, note, note)
	}
	out, err := voicebuild.AssembleWithKeygroups(voices, groups)
	if err != nil {
		t.Fatal(err)
	}
	fzfPath := filepath.Join(t.TempDir(), "test.fzf")
	if err := os.WriteFile(fzfPath, out, 0644); err != nil {
		t.Fatal(err)
	}
	return out, fzfPath
}
