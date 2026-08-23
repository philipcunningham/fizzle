package voicebuild

import (
	"encoding/binary"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

// TestSplitDumpWithVoiceCount drives a two-disk split of a dump whose
// voice count only the DIS knows: one bank plays one of two voices, so
// the walk undercounts and would place the split's audio boundary a
// sector early.
func TestSplitDumpWithVoiceCount(t *testing.T) {
	t.Parallel()
	const vn = 2
	headerSectors := 1 + disk.VoiceAreaSectors(vn)
	audioSectors := MaxDiskFileBytes/disk.SectorSize + 4
	fzf := make([]byte, (headerSectors+audioSectors)*disk.SectorSize)
	binary.LittleEndian.PutUint16(fzf[disk.BankVoiceCountOffset:], 1)
	name := disk.PadLabel("SPLIT KIT")
	copy(fzf[disk.BankNameOffset:], name[:])
	for i := range vn {
		off := disk.VoiceSlotOffset(disk.SectorSize, i)
		binary.LittleEndian.PutUint16(fzf[off+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	}

	result, err := SplitDumpWithVoiceCount(fzf, vn)
	if err != nil {
		t.Fatalf("SplitDumpWithVoiceCount: %v", err)
	}
	if result.VoiceCount != vn {
		t.Errorf("VoiceCount = %d, want %d", result.VoiceCount, vn)
	}
	if result.BankCount != 1 {
		t.Errorf("BankCount = %d, want 1", result.BankCount)
	}
	if result.WaveCount != audioSectors {
		t.Errorf("WaveCount = %d, want %d", result.WaveCount, audioSectors)
	}
	if len(result.Disks) != 2 {
		t.Fatalf("Disks = %d, want 2", len(result.Disks))
	}
	if got := len(result.Disks[0]) + len(result.Disks[1]); got != len(fzf) {
		t.Errorf("split bytes = %d, want %d", got, len(fzf))
	}
	// Disk 1 carries the total wave marker the DIS wn derives from.
	marker := int(binary.LittleEndian.Uint32(result.Disks[0][disk.BankTotalWaveOffset:]))
	if marker != audioSectors {
		t.Errorf("total wave marker = %d, want %d", marker, audioSectors)
	}
}
