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

// MakeBanklessVoiceDump builds a dump shaped like PREY.img's: bsteps
// sum to 4, five live voices (the fifth in no bank), three stale
// slots. Only a DIS vn of 5 counts the live ones.
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
	writeSlot := func(slot int, name string, wavst, waved uint32) {
		off := disk.VoiceSlotOffset(voiceArea, slot)
		binary.LittleEndian.PutUint16(data[off+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
		binary.LittleEndian.PutUint32(data[off+disk.VoiceWaveStartOffset:], wavst)
		binary.LittleEndian.PutUint32(data[off+disk.VoiceWaveEndOffset:], waved)
		padded := disk.PadLabel(name)
		copy(data[off+disk.VoiceNameOffset:], padded[:])
	}
	const perVoice = 1024 / BanklessDumpVoices // samples per live voice, 2 sectors tiled
	for i := range BanklessDumpVoices {
		writeSlot(i, "VOICE"+string(rune('0'+i)), uint32(i*perVoice), uint32((i+1)*perVoice)) //nolint:gosec // small test values
	}
	// Stale slots carry the earlier, larger save's layout: old names
	// and old pointers, distinct from every live slot's bytes.
	for i := 1; i <= 3; i++ {
		writeSlot(BanklessDumpVoices-1+i, "OLDTAKE"+string(rune('0'+i)), uint32(i*300), uint32(i*300+250)) //nolint:gosec // small test values
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

// SharedVoiceDumpVoices is the voice count of the dump
// MakeSharedVoiceDump builds; its single bank's bstep is 5.
const SharedVoiceDumpVoices = 4

// MakeSharedVoiceDump builds a dump shaped like a shared-voice kit
// (CASIO097): one bank, five areas playing four voices through vp, so
// the summed bstep runs above vn while the walk agrees with vn.
func MakeSharedVoiceDump(t *testing.T) []byte {
	t.Helper()
	data := make([]byte, 4*disk.SectorSize)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], 5)
	bankName := disk.PadLabel("SHARED KIT")
	copy(data[disk.BankNameOffset:], bankName[:])
	for i, slot := range []int{0, 1, 2, 3, 0} {
		binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset+i*disk.VPEntrySize:], uint16(slot)) //nolint:gosec // small test values
	}
	for i := range SharedVoiceDumpVoices {
		off := disk.VoiceSlotOffset(disk.SectorSize, i)
		binary.LittleEndian.PutUint16(data[off+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
		padded := disk.PadLabel("SHARED" + string(rune('0'+i)))
		copy(data[off+disk.VoiceNameOffset:], padded[:])
	}
	// Non-zero audio, so the walk stops at the voice area's end.
	for i := 2 * disk.SectorSize; i < len(data); i++ {
		data[i] = 0xAB
	}
	return data
}

// FullDumpDISTail decodes the DIS sector of the image's FULL-DATA-FZ.
func FullDumpDISTail(t *testing.T, img *disk.Image) disk.DisSector {
	t.Helper()
	entries, err := img.Directory()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.NameString() != disk.FullDumpName {
			continue
		}
		sec, err := img.SectorRef(int(e.DisSector))
		if err != nil {
			t.Fatal(err)
		}
		dis, err := disk.DecodeDisSector(sec)
		if err != nil {
			t.Fatal(err)
		}
		return dis
	}
	t.Fatal("no FULL-DATA-FZ entry on image")
	return disk.DisSector{}
}
