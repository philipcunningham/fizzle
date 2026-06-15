package app

import (
	"encoding/binary"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/studio/loader"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

// TestGrowBanksTo_WiresInfoFields checks the App wrapper's own
// responsibility: after container.GrowBanks inserts bank sectors it
// republishes BankCount, AudioAreaStart, TotalBytes, and the Header's
// NBankSectors / VoiceAreaStart, and returns ok=true. The sector-insert
// byte-math is exercised in container/container_test.go.
func TestGrowBanksTo_WiresInfoFields(t *testing.T) {
	t.Parallel()
	data := make([]byte, 3*disk.SectorSize)                           // bank0 + voice sector + audio sector
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset:], 42) // bank0 vp[0]=42 (preserved at container level)
	const voiceAreaStart = disk.SectorSize

	a := New(t.TempDir())
	a.containerModel = model.FromBytes("", data)
	a.containerInfo = loader.ContainerInfo{
		BankCount:      1,
		AudioAreaStart: 2 * disk.SectorSize,
		TotalBytes:     int64(len(data)),
		Header:         &fzutil.FZFHeader{NBankSectors: 1, VoiceAreaStart: voiceAreaStart},
	}

	a, ok := a.growBanksTo(3)
	if !ok {
		t.Fatal("growBanksTo returned ok=false")
	}
	if a.containerInfo.BankCount != 3 {
		t.Fatalf("BankCount = %d, want 3", a.containerInfo.BankCount)
	}
	if a.containerInfo.AudioAreaStart != 4*disk.SectorSize {
		t.Errorf("AudioAreaStart = %d, want %d", a.containerInfo.AudioAreaStart, 4*disk.SectorSize)
	}
	if a.containerInfo.TotalBytes != int64(5*disk.SectorSize) {
		t.Errorf("TotalBytes = %d, want %d", a.containerInfo.TotalBytes, 5*disk.SectorSize)
	}
	if a.containerInfo.Header.NBankSectors != 3 {
		t.Errorf("Header.NBankSectors = %d, want 3", a.containerInfo.Header.NBankSectors)
	}
	if a.containerInfo.Header.VoiceAreaStart != voiceAreaStart+2*disk.SectorSize {
		t.Errorf("Header.VoiceAreaStart = %d, want %d", a.containerInfo.Header.VoiceAreaStart, voiceAreaStart+2*disk.SectorSize)
	}
}
