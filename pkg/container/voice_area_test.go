package container

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

func TestShrinkPreservesKnownRegionsAndClearsOnlyRetiredVoiceCapacity(t *testing.T) {
	t.Parallel()
	const (
		bankCount  = 1
		voiceCount = 14
		target     = 13
	)
	voiceStart := disk.SectorSize
	audioStart := voiceStart + disk.VoiceAreaSectors(voiceCount)*disk.SectorSize
	data := make([]byte, audioStart+disk.SectorSize)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], target)
	for area := range target {
		binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset+area*disk.VPEntrySize:], uint16(area)) //nolint:gosec // bounded fixture index
	}

	bankReserved := disk.BankVoiceMarkerOffset - 1
	data[bankReserved] = 0x5A
	liveUnknown := disk.VoiceSlotOffset(voiceStart, target-1) + disk.VoiceHeaderUsed
	data[liveUnknown] = 0xA5
	retired := disk.VoiceSlotOffset(voiceStart, target)
	for i := retired; i < audioStart; i++ {
		data[i] = 0xCC
	}
	name := disk.PadLabel("STALE VOICE")
	copy(data[retired+disk.VoiceNameOffset:], name[:])
	binary.LittleEndian.PutUint16(data[retired+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	for i := audioStart; i < len(data); i++ {
		data[i] = 0x3C
	}
	audio := bytes.Clone(data[audioStart:])

	resized, err := ResizeVoiceAreaOwned(data, VoiceAreaResizeParams{
		BankCount: bankCount, VoiceCount: voiceCount, VoiceStart: voiceStart,
		AudioStart: audioStart, WalkBound: voiceCount, FreedSlot: target,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resized.AudioStart != audioStart {
		t.Fatalf("audio start = %d, want unchanged %d", resized.AudioStart, audioStart)
	}
	if resized.Data[bankReserved] != 0x5A {
		t.Fatal("resize changed a reserved bank byte")
	}
	if resized.Data[liveUnknown] != 0xA5 {
		t.Fatal("resize changed an unknown byte in a surviving voice slot")
	}
	if !bytes.Equal(resized.Data[retired:audioStart], make([]byte, audioStart-retired)) {
		t.Fatal("retired voice capacity still contains stale bytes")
	}
	if !bytes.Equal(resized.Data[audioStart:], audio) {
		t.Fatal("resize changed audio bytes")
	}
}
