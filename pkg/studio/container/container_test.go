package container

import (
	"encoding/binary"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

// buildOneVoice returns a container with one bank (bstep=1, vp[0]=0), a
// voiceSectors-sector voice area whose slot 0 ends at sample 100, and
// audioSectors sectors of audio.
func buildOneVoice(voiceSectors, audioSectors int) []byte {
	voiceAreaStart := disk.SectorSize
	audioStart := voiceAreaStart + voiceSectors*disk.SectorSize
	total := audioStart + audioSectors*disk.SectorSize
	data := make([]byte, total)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], 1)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset:], 0)
	voff := disk.VoiceSlotOffset(voiceAreaStart, 0)
	binary.LittleEndian.PutUint32(data[voff+disk.VoiceWaveEndOffset:], 100)
	binary.LittleEndian.PutUint32(data[voff+disk.VoiceGenEndOffset:], 100)
	return data
}

func TestCompactVoiceArea_Shrinks(t *testing.T) {
	t.Parallel()
	data := buildOneVoice(2, 1) // 2 voice sectors (1 orphan), 1 audio sector (slack)
	audioStart := disk.SectorSize + 2*disk.SectorSize
	out, newAudioStart, changed := CompactVoiceArea(data, 1, audioStart)
	if !changed {
		t.Fatal("expected changed=true")
	}
	wantTotal := disk.SectorSize + disk.SectorSize + 101*disk.BytesPerSample // 2250
	if len(out) != wantTotal {
		t.Fatalf("len = %d, want %d", len(out), wantTotal)
	}
	if newAudioStart != 2*disk.SectorSize {
		t.Errorf("newAudioStart = %d, want %d", newAudioStart, 2*disk.SectorSize)
	}
	voff := disk.VoiceSlotOffset(disk.SectorSize, 0)
	if v := binary.LittleEndian.Uint32(out[voff+disk.VoiceWaveEndOffset:]); v != 100 {
		t.Errorf("voice 0 waved = %d, want 100", v)
	}
}

func TestCompactVoiceArea_Noop(t *testing.T) {
	t.Parallel()
	data := buildOneVoice(1, 0)
	data = append(data, make([]byte, 101*disk.BytesPerSample)...) // tight audio
	audioStart := 2 * disk.SectorSize
	out, _, changed := CompactVoiceArea(data, 1, audioStart)
	if changed {
		t.Fatalf("expected no-op (changed=false)")
	}
	if len(out) != len(data) {
		t.Fatalf("no-op should return the original slice; got len %d want %d", len(out), len(data))
	}
}

func TestMaxReferencedSlot(t *testing.T) {
	t.Parallel()
	data := make([]byte, disk.SectorSize)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], 3)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset+0*2:], 5)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset+1*2:], 2)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset+2*2:], 9)
	if got := maxReferencedSlot(data, 1); got != 9 {
		t.Errorf("maxReferencedSlot = %d, want 9", got)
	}
	if got := maxReferencedSlot(make([]byte, disk.SectorSize), 1); got != -1 {
		t.Errorf("empty maxReferencedSlot = %d, want -1", got)
	}
}

func TestCompactEmptyBanks_DropsMiddleEmpty(t *testing.T) {
	t.Parallel()
	const banks = 3
	voiceAreaStart := banks * disk.SectorSize
	audioStart := voiceAreaStart + disk.SectorSize
	data := make([]byte, audioStart+disk.SectorSize)
	binary.LittleEndian.PutUint16(data[0*disk.SectorSize+disk.BankVoiceCountOffset:], 5)
	binary.LittleEndian.PutUint16(data[0*disk.SectorSize+disk.BankVoiceNumOffset:], 10)
	binary.LittleEndian.PutUint16(data[2*disk.SectorSize+disk.BankVoiceCountOffset:], 7)
	binary.LittleEndian.PutUint16(data[2*disk.SectorSize+disk.BankVoiceNumOffset:], 20)

	out, newBankCount, newAudioStart, changed := CompactEmptyBanks(data, banks, audioStart)
	if !changed || newBankCount != 2 {
		t.Fatalf("changed=%v newBankCount=%d, want true/2", changed, newBankCount)
	}
	if newAudioStart != audioStart-disk.SectorSize {
		t.Errorf("newAudioStart=%d want %d", newAudioStart, audioStart-disk.SectorSize)
	}
	if v := binary.LittleEndian.Uint16(out[1*disk.SectorSize+disk.BankVoiceNumOffset:]); v != 20 {
		t.Errorf("compacted bank1 vp[0]=%d want 20 (was bank 2)", v)
	}
}

func TestCompactEmptyBanks_NoopWhenAllFull(t *testing.T) {
	t.Parallel()
	data := make([]byte, 2*disk.SectorSize+disk.SectorSize)
	binary.LittleEndian.PutUint16(data[0*disk.SectorSize+disk.BankVoiceCountOffset:], 3)
	binary.LittleEndian.PutUint16(data[1*disk.SectorSize+disk.BankVoiceCountOffset:], 4)
	out, _, _, changed := CompactEmptyBanks(data, 2, 2*disk.SectorSize)
	if changed {
		t.Fatalf("expected no-op (changed=false)")
	}
	if len(out) != len(data) {
		t.Fatalf("no-op should return the original slice; got len %d want %d", len(out), len(data))
	}
}

func TestSwapAreaPatches(t *testing.T) {
	t.Parallel()
	data := make([]byte, 2*disk.SectorSize)
	for _, off := range swapPerAreaFields {
		data[off+0] = 0x10
		data[off+1] = 0x20
	}
	data[disk.BankMIDIRecvChanOffset+0] = 0x05
	data[disk.BankMIDIRecvChanOffset+1] = 0x06
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset+0*disk.VPEntrySize:], 100)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset+1*disk.VPEntrySize:], 200)

	patches := SwapAreaPatches(data, SwapAreaParams{Base: 0, SrcArea: 0, TgtArea: 1})
	// apply in-place to verify the swap result
	for _, p := range patches {
		copy(data[p.Offset:p.Offset+len(p.New)], p.New)
	}
	for _, off := range swapPerAreaFields {
		if data[off+0] != 0x20 || data[off+1] != 0x10 {
			t.Errorf("field %#x not swapped", off)
		}
	}
	if data[disk.BankMIDIRecvChanOffset+0] != 0x05 || data[disk.BankMIDIRecvChanOffset+1] != 0x06 {
		t.Errorf("MIDI recv chan must not swap (REMAIN-008)")
	}
	if binary.LittleEndian.Uint16(data[disk.BankVoiceNumOffset:]) != 200 {
		t.Errorf("vp[0] not swapped")
	}
}

func applyInPlace(data []byte, patches []model.Patch) {
	for _, p := range patches {
		copy(data[p.Offset:p.Offset+len(p.New)], p.New)
	}
}

func TestDeleteAreaPatches(t *testing.T) {
	t.Parallel()
	data := make([]byte, disk.SectorSize)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], 3)
	for _, off := range perAreaMetadataOffsets {
		data[off+0], data[off+1], data[off+2] = 0, 1, 2
	}
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset+0*disk.VPEntrySize:], 10)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset+1*disk.VPEntrySize:], 11)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset+2*disk.VPEntrySize:], 12)

	applyInPlace(data, DeleteAreaPatches(data, DeleteAreaParams{Base: 0, AreaIdx: 0, Bstep: 3}))

	if b := binary.LittleEndian.Uint16(data[disk.BankVoiceCountOffset:]); b != 2 {
		t.Fatalf("bstep = %d, want 2", b)
	}
	for _, off := range perAreaMetadataOffsets {
		if data[off+0] != 1 || data[off+1] != 2 || data[off+2] != 0 {
			t.Errorf("field %#x = [%d,%d,%d], want [1,2,0]", off, data[off+0], data[off+1], data[off+2])
		}
	}
	if v := binary.LittleEndian.Uint16(data[disk.BankVoiceNumOffset:]); v != 11 {
		t.Errorf("vp[0] = %d, want 11", v)
	}
}

func TestDuplicateAreaPatches(t *testing.T) {
	t.Parallel()
	// 1 bank + 1 voice sector; bstep=1, area 0 metadata set.
	data := make([]byte, 2*disk.SectorSize)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], 1)
	for _, off := range perAreaMetadataOffsets {
		data[off+0] = 0x42
	}
	srcHeader := make([]byte, disk.VoicePackSize)
	for i := range srcHeader {
		srcHeader[i] = 0x7E
	}
	newSlot := 1
	newOff := disk.VoiceSlotOffset(disk.SectorSize, newSlot)

	applyInPlace(data, DuplicateAreaPatches(data, DuplicateAreaParams{Base: 0, NewOff: newOff, SrcAreaIdx: 0, Bstep: 1, NewSlot: newSlot, SrcHeader: srcHeader}))

	if b := binary.LittleEndian.Uint16(data[disk.BankVoiceCountOffset:]); b != 2 {
		t.Fatalf("bstep = %d, want 2", b)
	}
	if v := binary.LittleEndian.Uint16(data[disk.BankVoiceNumOffset+1*disk.VPEntrySize:]); v != uint16(newSlot) {
		t.Errorf("vp[1] = %d, want %d", v, newSlot)
	}
	for _, off := range perAreaMetadataOffsets {
		if data[off+1] != 0x42 {
			t.Errorf("metadata %#x area1 = %#x, want 0x42 (copied from area0)", off, data[off+1])
		}
	}
	if data[newOff] != 0x7E || data[newOff+disk.VoicePackSize-1] != 0x7E {
		t.Errorf("voice header not cloned to newOff")
	}
}

func TestGrowBanks(t *testing.T) {
	t.Parallel()
	data := make([]byte, 2*disk.SectorSize)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset:], 42)
	out, growBytes := GrowBanks(data, 1, 3)
	if growBytes != 2*disk.SectorSize {
		t.Fatalf("growBytes = %d, want %d", growBytes, 2*disk.SectorSize)
	}
	if len(out) != 4*disk.SectorSize {
		t.Fatalf("len = %d, want %d", len(out), 4*disk.SectorSize)
	}
	if v := binary.LittleEndian.Uint16(out[disk.BankVoiceNumOffset:]); v != 42 {
		t.Errorf("bank0 vp[0] = %d, want 42 (preserved)", v)
	}
	for b := 1; b <= 2; b++ {
		if out[b*disk.SectorSize+disk.BankNameOffset] != ' ' {
			t.Errorf("new bank %d name not space-seeded", b)
		}
	}
}

func TestRewriteWavePointers(t *testing.T) {
	t.Parallel()
	hdr := make([]byte, disk.VoicePackSize)
	binary.LittleEndian.PutUint32(hdr[disk.VoiceWaveStartOffset:], 10)
	binary.LittleEndian.PutUint32(hdr[disk.VoiceWaveEndOffset:], 20)
	binary.LittleEndian.PutUint32(hdr[disk.VoiceGenStartOffset:], 12)
	binary.LittleEndian.PutUint32(hdr[disk.VoiceGenEndOffset:], 18)

	RewriteWavePointers(hdr, 100)

	for _, c := range []struct {
		off  int
		want uint32
	}{
		{disk.VoiceWaveStartOffset, 110}, {disk.VoiceWaveEndOffset, 120},
		{disk.VoiceGenStartOffset, 112}, {disk.VoiceGenEndOffset, 118},
	} {
		if v := binary.LittleEndian.Uint32(hdr[c.off:]); v != c.want {
			t.Errorf("ptr %#x = %d, want %d", c.off, v, c.want)
		}
	}
}

func TestBankBstepBumpPatch(t *testing.T) {
	t.Parallel()
	data := make([]byte, disk.SectorSize)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], 2)
	if p, ok := BankBstepBumpPatch(data, 0, 5); !ok || binary.LittleEndian.Uint16(p.New) != 6 {
		t.Errorf("bump to area 5: ok=%v new=%v, want ok/6", ok, p.New)
	}
	if _, ok := BankBstepBumpPatch(data, 0, 1); ok {
		t.Errorf("bump to area 1 (<= bstep 2) should be ok=false")
	}
}

func TestDefaultBankRangePatches(t *testing.T) {
	t.Parallel()
	data := make([]byte, disk.SectorSize)
	patches := DefaultBankRangePatches(data, 0, 0)
	if len(patches) != 4 {
		t.Fatalf("got %d patches, want 4", len(patches))
	}
	applyInPlace(data, patches)
	if data[disk.BankKeyHighOffset] != 0x7F || data[disk.BankVelHighOffset] != 0x7F || data[disk.BankVelLowOffset] != 0x01 {
		t.Errorf("default ranges not set: keyHigh=%#x velHigh=%#x velLow=%#x",
			data[disk.BankKeyHighOffset], data[disk.BankVelHighOffset], data[disk.BankVelLowOffset])
	}
}

// TestCompactedSize_AgreesWithCompactVoiceArea pins that the cheap,
// non-allocating size predictor returns exactly what CompactVoiceArea
// would shrink the buffer to, so the free-space display (which uses
// CompactedSize) matches what a save actually reclaims (N-04).
func TestCompactedSize_AgreesWithCompactVoiceArea(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                       string
		voiceSectors, audioSectors int
	}{
		{"orphan voice + audio slack", 2, 1},
		{"tight", 1, 0},
		{"audio slack only", 1, 2},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			data := buildOneVoice(c.voiceSectors, c.audioSectors)
			audioStart := disk.SectorSize + c.voiceSectors*disk.SectorSize
			out, _, _ := CompactVoiceArea(data, 1, audioStart)
			if got := CompactedSize(data, 1, audioStart); got != len(out) {
				t.Errorf("CompactedSize = %d, want %d (len of CompactVoiceArea result)", got, len(out))
			}
		})
	}
}
