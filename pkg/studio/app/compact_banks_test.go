package app

import (
	"encoding/binary"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/studio/loader"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

// TestCompactEmptyBanks_WiresInfoFields checks the App wrapper's own
// responsibility: after container.CompactEmptyBanks drops an empty bank
// it republishes BankCount, AudioAreaStart, TotalBytes, and the Header's
// NBankSectors / VoiceAreaStart. The bank-shifting byte-math is exercised
// in container/container_test.go.
func TestCompactEmptyBanks_WiresInfoFields(t *testing.T) {
	t.Parallel()
	const banks = 3
	voiceAreaStart := banks * disk.SectorSize
	audioStart := voiceAreaStart + disk.SectorSize
	total := audioStart + disk.SectorSize
	data := make([]byte, total)
	// Bank 0: bstep=5. Bank 1: empty. Bank 2: bstep=7.
	binary.LittleEndian.PutUint16(data[0*disk.SectorSize+disk.BankVoiceCountOffset:], 5)
	binary.LittleEndian.PutUint16(data[2*disk.SectorSize+disk.BankVoiceCountOffset:], 7)

	a := New(t.TempDir())
	a.containerModel = model.FromBytes("", data)
	a.containerInfo = loader.ContainerInfo{
		BankCount:      banks,
		AudioAreaStart: audioStart,
		TotalBytes:     int64(total),
		Header:         &fzutil.FZFHeader{NBankSectors: banks, VoiceAreaStart: voiceAreaStart},
	}

	a = a.compactEmptyBanks()

	if a.containerInfo.BankCount != 2 {
		t.Fatalf("BankCount = %d, want 2", a.containerInfo.BankCount)
	}
	if a.containerInfo.AudioAreaStart != audioStart-disk.SectorSize {
		t.Errorf("AudioAreaStart = %d, want %d", a.containerInfo.AudioAreaStart, audioStart-disk.SectorSize)
	}
	if a.containerInfo.TotalBytes != int64(total-disk.SectorSize) {
		t.Errorf("TotalBytes = %d, want %d", a.containerInfo.TotalBytes, total-disk.SectorSize)
	}
	if a.containerInfo.Header.NBankSectors != 2 {
		t.Errorf("Header.NBankSectors = %d, want 2", a.containerInfo.Header.NBankSectors)
	}
	if a.containerInfo.Header.VoiceAreaStart != voiceAreaStart-disk.SectorSize {
		t.Errorf("Header.VoiceAreaStart = %d, want %d", a.containerInfo.Header.VoiceAreaStart, voiceAreaStart-disk.SectorSize)
	}
}
