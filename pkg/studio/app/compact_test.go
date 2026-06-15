package app

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/studio/loader"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

// buildOneVoiceContainer returns a hand-built container: one bank
// (bstep=1, vp[0]=0), a voice area of voiceSectors sectors with a single
// plausible voice in slot 0 whose audio ends at sample 100, and
// audioSectors sectors of audio. AudioAreaStart is the byte offset where
// the audio begins.
func buildOneVoiceContainer(voiceSectors, audioSectors int) ([]byte, loader.ContainerInfo) {
	const bankCount = 1
	voiceAreaStart := bankCount * disk.SectorSize
	audioStart := voiceAreaStart + voiceSectors*disk.SectorSize
	total := audioStart + audioSectors*disk.SectorSize
	data := make([]byte, total)

	// Bank 0: bstep=1, vp[0]=0.
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], 1)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset:], 0)

	// Voice slot 0: wave 0..100, gen end 100, no loops.
	voff := disk.VoiceSlotOffset(voiceAreaStart, 0)
	binary.LittleEndian.PutUint32(data[voff+disk.VoiceWaveStartOffset:], 0)
	binary.LittleEndian.PutUint32(data[voff+disk.VoiceWaveEndOffset:], 100)
	binary.LittleEndian.PutUint32(data[voff+disk.VoiceGenEndOffset:], 100)

	info := loader.ContainerInfo{
		BankCount:      bankCount,
		AudioAreaStart: audioStart,
		TotalBytes:     int64(total),
	}
	return data, info
}

func newAppWithContainer(t *testing.T, data []byte, info loader.ContainerInfo) App {
	t.Helper()
	a := New(t.TempDir())
	a.containerModel = model.FromBytes("", data)
	a.containerInfo = info
	return a
}

// TestCompactVoiceArea_WiresInfoFields checks the App wrapper's own
// responsibility: after container.CompactVoiceArea shrinks the buffer,
// the wrapper republishes AudioAreaStart, TotalBytes, and (when present)
// Header.VoiceAreaStart. The shrink byte-math itself is exercised in
// container/container_test.go.
func TestCompactVoiceArea_WiresInfoFields(t *testing.T) {
	t.Parallel()
	data, info := buildOneVoiceContainer(2, 1) // 1 orphan voice sector, audio with slack
	info.Header = &fzutil.FZFHeader{VoiceAreaStart: info.AudioAreaStart}
	a := newAppWithContainer(t, data, info)

	a = a.compactVoiceArea()

	newLen := len(a.containerModel.Bytes())
	wantAudioStart := 2 * disk.SectorSize // bank + compacted voice sector
	if a.containerInfo.AudioAreaStart != wantAudioStart {
		t.Errorf("AudioAreaStart = %d, want %d", a.containerInfo.AudioAreaStart, wantAudioStart)
	}
	if a.containerInfo.TotalBytes != int64(newLen) {
		t.Errorf("TotalBytes = %d, want %d", a.containerInfo.TotalBytes, newLen)
	}
	if a.containerInfo.Header.VoiceAreaStart != wantAudioStart {
		t.Errorf("Header.VoiceAreaStart = %d, want %d", a.containerInfo.Header.VoiceAreaStart, wantAudioStart)
	}
}

// TestCompactVoiceArea_NoopLeavesAppUntouched pins the fail-safe no-op
// path at the App layer: a tight container is returned byte-for-byte
// unchanged with its info fields intact.
func TestCompactVoiceArea_NoopLeavesAppUntouched(t *testing.T) {
	t.Parallel()
	data, info := buildOneVoiceContainer(1, 0)
	data = append(data, make([]byte, 101*disk.BytesPerSample)...) // tight audio: required == current
	info.TotalBytes = int64(len(data))
	a := newAppWithContainer(t, data, info)
	before := append([]byte(nil), data...)
	beforeInfo := a.containerInfo

	a = a.compactVoiceArea()

	if !bytes.Equal(a.containerModel.Bytes(), before) {
		t.Fatalf("tight container changed under no-op")
	}
	if a.containerInfo != beforeInfo {
		t.Errorf("info mutated on no-op: %+v != %+v", a.containerInfo, beforeInfo)
	}
}
