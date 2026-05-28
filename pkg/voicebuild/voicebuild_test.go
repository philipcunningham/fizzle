package voicebuild

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
	"github.com/philipcunningham/fizzle/pkg/render"
)

func TestAssembleSingleVoice(t *testing.T) {
	t.Parallel()
	voice := testutil.MakeTestVoice("KICK", 512)
	out, err := assemble([][]byte{voice})
	if err != nil {
		t.Fatal(err)
	}
	// Output must be at least: bank sector + 1 voice sector + audio.
	if len(out) < disk.SectorSize*2 {
		t.Errorf("output too small: %d bytes", len(out))
	}
	// Voice count in bank sector (first 2 bytes, LE).
	voiceCount := binary.LittleEndian.Uint16(out[0:2])
	if voiceCount != 1 {
		t.Errorf("voiceCount: got %d, want 1", voiceCount)
	}
}

func TestAssembleVoiceKeyMapping(t *testing.T) {
	t.Parallel()
	voice := testutil.MakeTestVoice("A", 10)
	out, err := assemble([][]byte{voice})
	if err != nil {
		t.Fatal(err)
	}
	if out[disk.BankKeyHighOffset] != disk.FirstMIDINote {
		t.Errorf("key high: got %d, want %d", out[disk.BankKeyHighOffset], disk.FirstMIDINote)
	}
	if out[disk.BankKeyLowOffset] != disk.FirstMIDINote {
		t.Errorf("key low: got %d, want %d", out[disk.BankKeyLowOffset], disk.FirstMIDINote)
	}
}

// TestAssembleWritesBVol pins the per-voice bvol[64] volume array (spec
// §2-2, range 0-127) to DefaultBankVolume. We default to 0 because factory
// Casio disks sit in 0..27 and a disk written with bvol=0 plays loud on
// real hardware while bvol=127 plays much quieter. Field semantics are not
// fully understood; see the DefaultBankVolume comment in pkg/disk.
func TestAssembleWritesBVol(t *testing.T) {
	t.Parallel()
	const n = 5
	voices := make([][]byte, n)
	for i := range voices {
		voices[i] = testutil.MakeTestVoice(fmt.Sprintf("V%d", i), 10)
	}
	out, err := assemble(voices)
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if got := out[disk.BankVolumeOffset+i]; got != disk.DefaultBankVolume {
			t.Errorf("bvol[%d] = %d, want %d", i, got, disk.DefaultBankVolume)
		}
	}
}

func TestAssembleMaxVoices(t *testing.T) {
	t.Parallel()
	voices := make([][]byte, disk.MaxVoices)
	for i := range voices {
		voices[i] = testutil.MakeTestVoice(fmt.Sprintf("V%02d", i), 10)
	}
	out, err := assemble(voices)
	if err != nil {
		t.Fatalf("assemble with %d voices: %v", disk.MaxVoices, err)
	}
	voiceCount := binary.LittleEndian.Uint16(out[0:2])
	if int(voiceCount) != disk.MaxVoices {
		t.Errorf("voice count: got %d, want %d", voiceCount, disk.MaxVoices)
	}
}

func TestAssembleTooManyVoices(t *testing.T) {
	t.Parallel()
	voices := make([][]byte, disk.MaxVoices+1)
	for i := range voices {
		voices[i] = testutil.MakeTestVoice("X", 10)
	}
	_, err := assemble(voices)
	if err == nil {
		t.Error("expected error for too many voices")
	}
}

func TestFixSampleOffsets(t *testing.T) {
	t.Parallel()
	voice := make([]byte, disk.SectorSize)
	binary.LittleEndian.PutUint32(voice[0x00:], 0)
	binary.LittleEndian.PutUint32(voice[0x04:], 100)
	fixSampleOffsets(voice, 50)
	if binary.LittleEndian.Uint32(voice[0x00:]) != 50 {
		t.Errorf("wavest after fix: got %d, want 50", binary.LittleEndian.Uint32(voice[0x00:]))
	}
	if binary.LittleEndian.Uint32(voice[0x04:]) != 150 {
		t.Errorf("waved after fix: got %d, want 150", binary.LittleEndian.Uint32(voice[0x04:]))
	}
}

// TestFixSampleOffsetsPreservesLoopFlagBits is a regression test for a bug
// where fixSampleOffsets treated loopst[i] and looped[i] as plain 32-bit
// addresses and so corrupted the reserved flag bits (spec §2-1: loopst
// upper 8 bits = loop-fine, looped MSB = skip flag). Third-party voices
// with non-zero flag bits would survive a build round-trip with garbage in
// those bits.
func TestFixSampleOffsetsPreservesLoopFlagBits(t *testing.T) {
	t.Parallel()
	voice := make([]byte, disk.SectorSize)
	// loopst[0]: loop-fine = 0x37, address = 0x400.
	binary.LittleEndian.PutUint32(voice[disk.VoiceLoopSt0Offset:], 0x37000400)
	// looped[0]: skip flag set, address = 0x500.
	binary.LittleEndian.PutUint32(voice[disk.VoiceLoopEd0Offset:], 0x80000500)

	fixSampleOffsets(voice, 100)

	gotSt := binary.LittleEndian.Uint32(voice[disk.VoiceLoopSt0Offset:])
	if got := disk.LoopFineBits(gotSt); got != 0x37 {
		t.Errorf("loopst[0] loop-fine after fix: got 0x%02x, want 0x37", got)
	}
	if got := disk.LoopStartAddress(gotSt); got != 0x400+100 {
		t.Errorf("loopst[0] address after fix: got 0x%x, want 0x%x", got, 0x400+100)
	}

	gotEd := binary.LittleEndian.Uint32(voice[disk.VoiceLoopEd0Offset:])
	if !disk.LoopSkipFlag(gotEd) {
		t.Errorf("looped[0] skip flag lost after fix: raw=0x%08x", gotEd)
	}
	if got := disk.LoopEndAddress(gotEd); got != 0x500+100 {
		t.Errorf("looped[0] address after fix: got 0x%x, want 0x%x", got, 0x500+100)
	}
}

// TestAssembleWithGroupsPreservesLoopFlagBits is the end-to-end regression
// test: a voice carrying loop-fine and skip-flag bits should survive
// assembly into an FZF with both flag bits intact and addresses shifted by
// the priorSamples count for the voice's slot.
func TestAssembleWithGroupsPreservesLoopFlagBits(t *testing.T) {
	t.Parallel()

	// Voice 0 contributes 50 samples (100 bytes -> padded to one sector).
	// Voice 1's slot has priorSamples = padded(100)/2 = 512 samples.
	v0 := testutil.MakeTestVoice("FIRST", 50)
	v1 := testutil.MakeTestVoice("LOOPY", 50)

	// Stamp loop-flag bits into voice 1 before assembly.
	binary.LittleEndian.PutUint32(v1[disk.VoiceLoopSt0Offset:], 0x37000400)
	binary.LittleEndian.PutUint32(v1[disk.VoiceLoopEd0Offset:], 0x80000500)

	groups := []Keygroup{
		NewKeygroup(36, 36, 36),
		NewKeygroup(37, 37, 37),
	}
	out, err := AssembleWithKeygroups([][]byte{v0, v1}, groups)
	if err != nil {
		t.Fatal(err)
	}

	// Locate voice 1's header in the assembled voice area.
	voice1HdrOff := disk.SectorSize + disk.VoiceSlotOffset(0, 1)

	// MakeTestVoice's 50 samples = 100 bytes, padded to 1 sector (1024B) =
	// 512 samples preceding voice 1.
	const priorSamples = 512

	gotSt := binary.LittleEndian.Uint32(out[voice1HdrOff+disk.VoiceLoopSt0Offset:])
	if got := disk.LoopFineBits(gotSt); got != 0x37 {
		t.Errorf("loopst[0] loop-fine in assembled FZF: got 0x%02x, want 0x37", got)
	}
	if got := disk.LoopStartAddress(gotSt); got != 0x400+priorSamples {
		t.Errorf("loopst[0] address in assembled FZF: got 0x%x, want 0x%x",
			got, 0x400+priorSamples)
	}

	gotEd := binary.LittleEndian.Uint32(out[voice1HdrOff+disk.VoiceLoopEd0Offset:])
	if !disk.LoopSkipFlag(gotEd) {
		t.Errorf("looped[0] skip flag lost in assembled FZF: raw=0x%08x", gotEd)
	}
	if got := disk.LoopEndAddress(gotEd); got != 0x500+priorSamples {
		t.Errorf("looped[0] address in assembled FZF: got 0x%x, want 0x%x",
			got, 0x500+priorSamples)
	}
}

// TestAssembleAudioOffsets verifies the core invariant: each voice's audio
// bytes in the assembled FZF must start at waveStart*2 bytes from the audio
// area. This catches wrong audioOffset accumulation, the class of bug that
// caused FUNKY 05 to contain bass audio when voices crossed a sector boundary.
func TestAssembleAudioOffsets(t *testing.T) {
	t.Parallel()
	// 9 voices with distinct, recognisable audio content (each byte is the
	// voice index+1, so voice 1 = 0x01, voice 2 = 0x02, etc).
	n := 9
	voices := make([][]byte, n)
	sampleCounts := []int{500, 300, 700, 400, 600, 200, 800, 350, 450}
	for i := range n {
		v := make([]byte, disk.SectorSize+sampleCounts[i]*2)
		name := disk.PadLabel(fmt.Sprintf("V%02d", i+1))
		copy(v[disk.VoiceNameOffset:], name[:])
		binary.LittleEndian.PutUint32(v[0x00:], 0)
		binary.LittleEndian.PutUint32(v[0x04:], uint32(sampleCounts[i])) //nolint:gosec // G115: test constant
		binary.LittleEndian.PutUint32(v[0x08:], 0)
		binary.LittleEndian.PutUint32(v[0x0c:], uint32(sampleCounts[i])) //nolint:gosec // G115: test constant
		// Fill audio with a recognisable pattern: voice index repeated.
		marker := byte(i + 1)
		for j := disk.SectorSize; j < len(v); j++ {
			v[j] = marker
		}
		voices[i] = v
	}

	out, err := assemble(voices)
	if err != nil {
		t.Fatal(err)
	}

	// Audio area starts after bank sector + voice area.
	voiceSectors := disk.VoiceAreaSectors(n)
	audioAreaStart := disk.SectorSize + voiceSectors*disk.SectorSize

	for i := range n {
		// Read waveStart from the voice header in the voice area.
		voff := (i/4)*disk.SectorSize + (i%4)*disk.VoicePackSize
		hdrOff := disk.SectorSize + voff // after bank sector
		waveStart := int(binary.LittleEndian.Uint32(out[hdrOff+0x00 : hdrOff+0x04]))
		waveEnd := int(binary.LittleEndian.Uint32(out[hdrOff+0x04 : hdrOff+0x08]))

		// The audio for voice i must be at audioAreaStart + waveStart*2.
		audioByteStart := audioAreaStart + waveStart*2
		if audioByteStart+2 > len(out) {
			t.Errorf("voice %d: audioByteStart %d out of range", i+1, audioByteStart)
			continue
		}

		// Check that the first byte of audio matches the marker for this voice.
		got := out[audioByteStart]
		want := byte(i + 1)
		if got != want {
			t.Errorf("voice %d: audio at waveStart*2 = 0x%02x, want 0x%02x (wrong audio block offset)",
				i+1, got, want)
		}

		// Also verify waveEnd - waveStart == sampleCounts[i].
		if waveEnd-waveStart != sampleCounts[i] {
			t.Errorf("voice %d: waveEnd-waveStart = %d, want %d", i+1, waveEnd-waveStart, sampleCounts[i])
		}
	}
}

// TestAssembleSectorBoundary verifies that voice 5 (the first voice in the
// second voice sector, i/4 == 1) is packed at the correct offset and its
// sample pointers are set correctly.
func TestAssembleSectorBoundary(t *testing.T) {
	t.Parallel()
	// Exactly 5 voices. Voice 5 is at (5-1)/4 = 1st voice sector, slot 0.
	voices := make([][]byte, 5)
	for i := range voices {
		voices[i] = testutil.MakeTestVoice(fmt.Sprintf("V%02d", i+1), 512)
	}

	out, err := assemble(voices)
	if err != nil {
		t.Fatal(err)
	}

	// Voice 5 header is at: bank(1024) + sector1(1024) + slot0(0) = 2048.
	voice5HdrOff := disk.SectorSize + 1*disk.SectorSize + 0*disk.VoicePackSize
	waveStart5 := int(binary.LittleEndian.Uint32(out[voice5HdrOff+0x00 : voice5HdrOff+0x04]))
	waveEnd5 := int(binary.LittleEndian.Uint32(out[voice5HdrOff+0x04 : voice5HdrOff+0x08]))

	// Voice 5's waveStart must equal the cumulative padded sample count of voices 1-4.
	// Each of voices 1-4 has 512 samples = 512*2=1024 bytes (already sector-aligned) = 512 samples.
	// Cumulative = 4 * 512 = 2048.
	if waveStart5 != 2048 {
		t.Errorf("voice 5 waveStart: got %d, want 2048", waveStart5)
	}
	if waveEnd5-waveStart5 != 512 {
		t.Errorf("voice 5 sample count: got %d, want 512", waveEnd5-waveStart5)
	}
}

// TestAssembleWithKeygroupsAudioOut verifies that gchn (AudioOut) values are
// written correctly. Polyphonic voices get 0xff; mono voices get their
// assigned single-bit generator value.
func TestAssembleWithKeygroupsAudioOut(t *testing.T) {
	t.Parallel()
	voices := [][]byte{
		testutil.MakeTestVoice("DRUM", 64),
		testutil.MakeTestVoice("BASS", 64),
		testutil.MakeTestVoice("PAD", 64),
	}
	groups := []Keygroup{
		{KeyLow: 36, KeyHigh: 36, VelLow: 1, VelHigh: 127, KeyCentre: 36, AudioOut: 0x01}, // mono gen 1
		{KeyLow: 48, KeyHigh: 60, VelLow: 1, VelHigh: 127, KeyCentre: 48, AudioOut: 0x02}, // mono gen 2
		{KeyLow: 72, KeyHigh: 84, VelLow: 1, VelHigh: 127, KeyCentre: 72, AudioOut: 0xff}, // poly
	}
	out, err := AssembleWithKeygroups(voices, groups)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		voice int
		want  uint8
	}{
		{0, 0x01},
		{1, 0x02},
		{2, 0xff},
	}
	for _, tt := range tests {
		got := out[disk.BankAudioOutOffset+tt.voice]
		if got != tt.want {
			t.Errorf("voice %d gchn: got 0x%02x, want 0x%02x", tt.voice+1, got, tt.want)
		}
	}
}

// TestAssembleDefaultAudioOutIsPolyphonic verifies that the default assemble
// (no explicit keygroups) sets gchn=0xff for all voices.
func TestAssembleDefaultAudioOutIsPolyphonic(t *testing.T) {
	t.Parallel()
	voices := [][]byte{testutil.MakeTestVoice("A", 64), testutil.MakeTestVoice("B", 64)}
	out, err := assemble(voices)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		got := out[disk.BankAudioOutOffset+i]
		if got != 0xff {
			t.Errorf("voice %d default gchn: got 0x%02x, want 0xff (polyphonic)", i+1, got)
		}
	}
}

// multiDiskFixture builds a set of 3 large voices (300,000 samples each,
// ~600 KB per voice) and assembles them into a multi-disk result.
// 3 voices at 36kHz = ~1.8 MB, which requires 2 disks.
// Returns the voices and assembled result for use in tests.
func multiDiskFixture(t *testing.T) (voices [][]byte, result MultiDiskResult) {
	t.Helper()
	const nVoices = 3
	const samplesPerVoice = 300000
	voices = make([][]byte, nVoices)
	groups := make([]Keygroup, nVoices)
	for i := range voices {
		voices[i] = testutil.MakeTestVoice(fmt.Sprintf("V%02d", i+1), samplesPerVoice)
		groups[i] = NewKeygroup(uint8(36+i), uint8(36+i), uint8(36+i))
	}
	var err error
	result, err = AssembleMultiDisk(voices, groups)
	if err != nil {
		t.Fatalf("AssembleMultiDisk: %v", err)
	}
	return
}

// bankVoiceCount reads the bstep field from a bank sector (bytes 0-1).
// This is the number of voices the sampler expects to load from this file.
func bankVoiceCount(data []byte) int {
	return int(binary.LittleEndian.Uint16(data[0:2]))
}

// TestAssembleMultiDiskBothDisksFit verifies that both produced disks fit within
// the floppy disk capacity and that a valid split was made.
func TestAssembleMultiDiskBothDisksFit(t *testing.T) {
	t.Parallel()
	_, result := multiDiskFixture(t)

	if len(result.Disks) != 2 {
		t.Fatalf("expected 2 disks, got %d", len(result.Disks))
	}
	maxDiskData := ((disk.UsableDataSize - disk.SectorSize) / disk.SectorSize) * disk.SectorSize
	if len(result.Disks[0]) > maxDiskData {
		t.Errorf("disk 1: %d bytes exceeds capacity %d", len(result.Disks[0]), maxDiskData)
	}
	if len(result.Disks[1]) > maxDiskData {
		t.Errorf("disk 2: %d bytes exceeds capacity %d", len(result.Disks[1]), maxDiskData)
	}
	if len(result.Disks[0])%disk.SectorSize != 0 {
		t.Errorf("disk 1 size %d not sector-aligned", len(result.Disks[0]))
	}
	if len(result.Disks[1]) == 0 {
		t.Error("disk 2 is empty")
	}
}

// TestAssembleMultiDiskDisk1BankHasAllVoices is the critical regression test
// for the multi-disk format. Disk 1's bank sector must contain the total voice
// count for the full instrument, not just the voices whose audio fits on disk 1.
//
// Why this matters: after loading disk 1, the sampler compares the bank voice
// count against how much audio it received. If the counts match, it considers
// loading complete. If disk 1's bank only listed disk 1 voices, the sampler
// would never prompt for disk 2.
func TestAssembleMultiDiskDisk1BankHasAllVoices(t *testing.T) {
	t.Parallel()
	voices, result := multiDiskFixture(t)

	totalVoices := len(voices)
	disk1Bank := bankVoiceCount(result.Disks[0])

	if disk1Bank != totalVoices {
		t.Errorf("disk 1 bank voice count = %d, want %d (all voices in the instrument): "+
			"sampler will not prompt for disk 2 if this is wrong",
			disk1Bank, totalVoices)
	}
}

// TestAssembleMultiDiskTotalWaveExceedsDisk1Audio verifies that the total wave
// sector count (WaveCount in the result) exceeds the audio sectors on disk 1.
//
// The caller (diskadd) writes WaveCount into the DIS tail on both disks. The
// sampler compares total wave sectors against disk 1's local audio to decide
// whether to prompt for disk 2.
func TestAssembleMultiDiskTotalWaveExceedsDisk1Audio(t *testing.T) {
	t.Parallel()
	_, result := multiDiskFixture(t)

	d1 := result.Disks[0]
	nv := bankVoiceCount(d1)
	voiceAreaSectors := disk.VoiceAreaSectors(nv)
	headerBytes := disk.SectorSize + voiceAreaSectors*disk.SectorSize
	localAudioSectors := (len(d1) - headerBytes) / disk.SectorSize

	if result.WaveCount <= 0 {
		t.Fatal("WaveCount is zero: diskadd will write wrong DIS wn")
	}
	if result.WaveCount <= localAudioSectors {
		t.Errorf("total wave sectors (%d) must exceed disk 1 local audio (%d sectors): "+
			"the sampler uses this gap to know disk 2 exists",
			result.WaveCount, localAudioSectors)
	}
}

// TestAssembleMultiDiskDisk1BankKeyMapComplete verifies that disk 1's bank sector
// contains key mappings for every voice in the instrument, including those whose
// audio only arrives on disk 2.
func TestAssembleMultiDiskDisk1BankKeyMapComplete(t *testing.T) {
	t.Parallel()
	const nVoices = 3
	voices := make([][]byte, nVoices)
	groups := make([]Keygroup, nVoices)
	for i := range voices {
		voices[i] = testutil.MakeTestVoice(fmt.Sprintf("V%02d", i+1), 300000)
		groups[i] = NewKeygroup(uint8(36+i), uint8(60+i), uint8(48+i))
	}
	result, err := AssembleMultiDisk(voices, groups)
	if err != nil {
		t.Fatal(err)
	}

	for i := range nVoices {
		wantLow, wantHigh := uint8(36+i), uint8(60+i)
		gotLow, gotHigh := result.Disks[0][disk.BankKeyLowOffset+i], result.Disks[0][disk.BankKeyHighOffset+i]
		if gotLow != wantLow || gotHigh != wantHigh {
			t.Errorf("voice %d key map in disk 1 bank: got [%d-%d], want [%d-%d]",
				i+1, gotLow, gotHigh, wantLow, wantHigh)
		}
	}
}

// TestAssembleMultiDiskDisk1VoiceAreaCoversAllVoices verifies that the voice
// parameter area on disk 1 is sized for the full instrument. The sampler reads
// envelopes, loop points, and tuning from disk 1 for every voice, including
// those whose audio is on disk 2.
func TestAssembleMultiDiskDisk1VoiceAreaCoversAllVoices(t *testing.T) {
	t.Parallel()
	voices, result := multiDiskFixture(t)

	nVoices := len(voices)
	voiceAreaSize := disk.VoiceAreaSectors(nVoices) * disk.SectorSize
	minDisk1Size := disk.SectorSize + voiceAreaSize

	if len(result.Disks[0]) < minDisk1Size {
		t.Fatalf("disk 1 (%d bytes) too small to hold voice area for all %d voices (need %d bytes)",
			len(result.Disks[0]), nVoices, minDisk1Size)
	}

	for i := range nVoices {
		voff := disk.SectorSize + (i/4)*disk.SectorSize + (i%4)*disk.VoicePackSize
		gotName := string(result.Disks[0][voff+disk.VoiceNameOffset : voff+disk.VoiceNameOffset+4])
		wantPrefix := fmt.Sprintf("V%02d", i+1)
		if gotName[:len(wantPrefix)] != wantPrefix {
			t.Errorf("disk 1 voice area slot %d: got name %q, want prefix %q",
				i+1, gotName, wantPrefix)
		}
	}
}

// TestAssembleMultiDiskDisk2IsNotParseable verifies that disk 2 does not start
// with a recognisable bank sector. Under the new split strategy, disk 2 is
// pure audio continuation: the sampler appends it to RAM after disk 1's audio.
func TestAssembleMultiDiskDisk2IsNotParseable(t *testing.T) {
	t.Parallel()
	_, result := multiDiskFixture(t)

	d2 := result.Disks[1]
	if len(d2) < 2 {
		t.Fatal("disk 2 is too small")
	}
	bstep := binary.LittleEndian.Uint16(d2[0:2])
	if bstep >= 1 && bstep <= uint16(disk.MaxVoices) {
		t.Errorf("disk 2 first two bytes look like a valid bstep (%d): "+
			"disk 2 should be pure audio, not a bank sector", bstep)
	}
}

// TestAssembleMultiDiskExceedsHardwareLimit verifies that an *ErrTooManyDisks
// is returned when the total FZF exceeds the capacity of 2 floppy disks.
// The FZ series has 2 MB of sample RAM, so 2 disks is the hardware maximum.
func TestAssembleMultiDiskExceedsHardwareLimit(t *testing.T) {
	t.Parallel()
	maxPerDisk := ((disk.UsableDataSize - disk.SectorSize) / disk.SectorSize) * disk.SectorSize
	twoDisksBytes := 2 * maxPerDisk
	samplesPerVoice := twoDisksBytes/3/2 + 1
	voices := make([][]byte, 3)
	groups := make([]Keygroup, 3)
	for i := range voices {
		voices[i] = testutil.MakeTestVoice(fmt.Sprintf("V%02d", i+1), samplesPerVoice)
		groups[i] = NewKeygroup(uint8(36+i), uint8(36+i), uint8(36+i))
	}

	_, err := AssembleMultiDisk(voices, groups)
	if err == nil {
		t.Fatal("expected ErrTooManyDisks, got nil")
	}
	var tmd *ErrTooManyDisks
	if !errors.As(err, &tmd) {
		t.Fatalf("expected *ErrTooManyDisks, got %T: %v", err, err)
	}
	if tmd.CapacityBytes != twoDisksBytes {
		t.Errorf("CapacityBytes = %d, want %d", tmd.CapacityBytes, twoDisksBytes)
	}
	if tmd.TotalAudioBytes <= tmd.CapacityBytes {
		t.Errorf("TotalAudioBytes %d should exceed CapacityBytes %d", tmd.TotalAudioBytes, tmd.CapacityBytes)
	}
	if !strings.Contains(err.Error(), "over the limit") {
		t.Errorf("error message missing 'over the limit': %v", err)
	}
	if !strings.Contains(err.Error(), "2 disks hold") {
		t.Errorf("error message missing '2 disks hold': %v", err)
	}
}

func TestAssembleMultiDiskExceedsSampleRAM(t *testing.T) {
	t.Parallel()
	// Two voices whose total audio fits on 2 floppies but exceeds the
	// hardware's 2 MB sample RAM. Each voice is just over 1 MB so the
	// combined audio is ~2.1 MB (fits on 2 × 1.25 MB floppies but not
	// in 2 MB of RAM).
	samplesPerVoice := (disk.MaxSampleRAM/2 + 512) / 2
	voices := make([][]byte, 2)
	groups := make([]Keygroup, 2)
	for i := range voices {
		voices[i] = testutil.MakeTestVoice(fmt.Sprintf("V%02d", i+1), samplesPerVoice)
		groups[i] = NewKeygroup(uint8(36+i), uint8(36+i), uint8(36+i))
	}

	_, err := AssembleMultiDisk(voices, groups)
	if err == nil {
		t.Fatal("expected ErrSampleRAMExceeded, got nil")
	}
	var ram *ErrSampleRAMExceeded
	if !errors.As(err, &ram) {
		t.Fatalf("expected *ErrSampleRAMExceeded, got %T: %v", err, err)
	}
	if ram.TotalAudioBytes <= disk.MaxSampleRAM {
		t.Errorf("TotalAudioBytes %d should exceed MaxSampleRAM %d", ram.TotalAudioBytes, disk.MaxSampleRAM)
	}
	if !strings.Contains(err.Error(), "sample RAM") {
		t.Errorf("error message missing 'sample RAM': %v", err)
	}
}

// TestAssembleMultiDiskDisk2IsPureAudio verifies that disk 2 contains only
// audio data: no bank sector, no voice headers. Every byte is part of the
// wave area that the sampler appends after disk 1's audio.
func TestAssembleMultiDiskDisk2IsPureAudio(t *testing.T) {
	t.Parallel()
	_, result := multiDiskFixture(t)

	d2 := result.Disks[1]
	if len(d2) == 0 {
		t.Fatal("disk 2 is empty")
	}
	if len(d2)%disk.SectorSize != 0 {
		t.Errorf("disk 2 size %d not sector-aligned", len(d2))
	}
	d1 := result.Disks[0]
	nv := bankVoiceCount(d1)
	voiceAreaSectors := disk.VoiceAreaSectors(nv)
	headerBytes := disk.SectorSize + voiceAreaSectors*disk.SectorSize
	d1AudioBytes := len(d1) - headerBytes
	if d1AudioBytes <= 0 {
		t.Fatal("disk 1 has no audio")
	}
	if len(d2) == 0 {
		t.Fatal("disk 2 has no audio")
	}
	totalAudio := d1AudioBytes + len(d2)
	if totalAudio%disk.SectorSize != 0 {
		t.Errorf("total audio %d not sector-aligned", totalAudio)
	}
}

// TestAssembleMultiDiskMetadata verifies that BankCount, VoiceCount, and
// WaveCount in MultiDiskResult are set correctly.
func TestAssembleMultiDiskMetadata(t *testing.T) {
	t.Parallel()
	voices, result := multiDiskFixture(t)

	if result.BankCount != 1 {
		t.Errorf("BankCount = %d, want 1", result.BankCount)
	}
	if result.VoiceCount != len(voices) {
		t.Errorf("VoiceCount = %d, want %d", result.VoiceCount, len(voices))
	}
	if result.WaveCount <= 0 {
		t.Errorf("WaveCount = %d, want > 0", result.WaveCount)
	}
	d1 := result.Disks[0]
	nv := bankVoiceCount(d1)
	voiceAreaSectors := disk.VoiceAreaSectors(nv)
	headerBytes := disk.SectorSize + voiceAreaSectors*disk.SectorSize
	d1AudioSectors := (len(d1) - headerBytes) / disk.SectorSize
	d2AudioSectors := len(result.Disks[1]) / disk.SectorSize
	if result.WaveCount != d1AudioSectors+d2AudioSectors {
		t.Errorf("WaveCount = %d, want %d (sum of audio sectors across both disks)",
			result.WaveCount, d1AudioSectors+d2AudioSectors)
	}
}

// TestAssembleMultiDiskAudioContinuity verifies that concatenating disk 1's
// audio area and disk 2's data reconstructs the complete FZF audio. This
// proves that the split is a clean byte-level partition with no duplication
// or gaps.
func TestAssembleMultiDiskAudioContinuity(t *testing.T) {
	t.Parallel()
	voices, result := multiDiskFixture(t)

	groups := make([]Keygroup, len(voices))
	for i := range voices {
		groups[i] = NewKeygroup(uint8(36+i), uint8(36+i), uint8(36+i))
	}
	completeFZF, err := AssembleWithKeygroups(voices, groups)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(completeFZF[disk.BankTotalWaveOffset:], uint32(result.WaveCount)) //nolint:gosec // G115: test value fits uint32

	d1 := result.Disks[0]
	d2 := result.Disks[1]
	reconstructed := make([]byte, len(d1)+len(d2))
	copy(reconstructed, d1)
	copy(reconstructed[len(d1):], d2)

	if len(reconstructed) != len(completeFZF) {
		t.Fatalf("reconstructed size %d != complete FZF size %d", len(reconstructed), len(completeFZF))
	}
	for i := range completeFZF {
		if reconstructed[i] != completeFZF[i] {
			t.Fatalf("mismatch at byte %d: reconstructed 0x%02x != original 0x%02x",
				i, reconstructed[i], completeFZF[i])
		}
	}
}

// TestAssembleMultiDiskAllFitsErrors verifies an error is returned when the
// instrument fits on one disk. The caller should use AssembleWithKeygroups.
func TestAssembleMultiDiskAllFitsErrors(t *testing.T) {
	t.Parallel()
	voices := [][]byte{testutil.MakeTestVoice("KICK", 512)}
	groups := []Keygroup{NewKeygroup(36, 36, 36)}
	_, err := AssembleMultiDisk(voices, groups)
	if err == nil {
		t.Error("expected error when instrument fits on one disk")
	}
}

// TestBuildKeyCentreNotCorrupted is a regression test for a bug where
// midiChan was written at offset 0x104 instead of 0x142, which overwrote
// the cent[] array starting at voice 3, setting their key centres to 0 (C-1).
// This caused wrong pitch playback on hardware for all but the first two voices.
func TestBuildKeyCentreNotCorrupted(t *testing.T) {
	t.Parallel()
	// Build 5 voices so voices 3-5 span the corrupted region.
	voices := make([][]byte, 5)
	for i := range voices {
		voices[i] = testutil.MakeTestVoice(fmt.Sprintf("V%02d", i+1), 64)
	}
	out, err := assemble(voices)
	if err != nil {
		t.Fatal(err)
	}

	for i := range 5 {
		want := uint8(disk.FirstMIDINote + i)
		got := out[disk.BankKeyCentOffset+i]
		if got != want {
			t.Errorf("voice %d cent: got %d (%s), want %d (midiChan offset bug corrupted cent array)",
				i+1, got, render.NoteName(got), want)
		}
		mc := out[disk.BankMIDIRecvChanOffset+i]
		if mc != 0 {
			t.Errorf("voice %d midiChan: got %d, want 0", i+1, mc)
		}
	}
}

func TestBuildNoVoices(t *testing.T) {
	t.Parallel()
	err := Build(context.Background(), filepath.Join(t.TempDir(), "out.fzf"), nil)
	if err == nil {
		t.Error("expected error for empty voice list")
	}
}

// TestBuildContextCancelled verifies Build aborts mid-iteration when ctx is
// already cancelled. We don't need a real voice file because the ctx check
// fires before the read attempt.
func TestBuildContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Build(ctx, filepath.Join(t.TempDir(), "out.fzf"), []string{"any.fzv"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestBuildWarnsSizeExceedsDisk verifies that building a dump larger than
// disk.UsableDataSize produces a WARN log.
//
// Not parallel: CaptureLog redirects the global logger.
func TestBuildWarnsSizeExceedsDisk(t *testing.T) {
	buf := testutil.CaptureLog(t)

	dir := t.TempDir()

	// Each voice is ~1 MB of audio (500,000 samples × 2 bytes).
	// Two such voices give ~2 MB, exceeding UsableDataSize (~1.25 MB).
	v := testutil.MakeTestVoice("BIG", 500000)
	vPath := filepath.Join(dir, "big.fzv")
	if err := os.WriteFile(vPath, v, 0644); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(dir, "out.fzf")
	if err := Build(context.Background(), outPath, []string{vPath, vPath}); err != nil {
		t.Fatal(err)
	}
	if !testutil.BufHasWarnContaining(buf, "exceeds floppy disk capacity") {
		t.Error("expected disk capacity warning when dump exceeds UsableDataSize")
	}
}

// TestBuildBankSectorRejectsKeyLowGreaterThanKeyHigh verifies that an
// inverted key range is rejected with a descriptive error rather than
// producing an incoherent bank sector.
func TestBuildBankSectorRejectsKeyLowGreaterThanKeyHigh(t *testing.T) {
	t.Parallel()
	voices := [][]byte{testutil.MakeTestVoice("BAD", 64)}
	groups := []Keygroup{{
		KeyLow:    72,
		KeyHigh:   60, // inverted
		KeyCentre: 60,
		VelLow:    disk.DefaultVelLow,
		VelHigh:   disk.DefaultVelHigh,
		AudioOut:  disk.PolyphonicAudioOut,
	}}
	_, err := AssembleWithKeygroups(voices, groups)
	if err == nil {
		t.Fatal("expected error for KeyLow > KeyHigh")
	}
	msg := err.Error()
	if !strings.Contains(msg, "KeyLow") || !strings.Contains(msg, "KeyHigh") {
		t.Errorf("error should mention KeyLow and KeyHigh: %v", err)
	}
	if !strings.Contains(msg, "BAD") {
		t.Errorf("error should name the offending voice: %v", err)
	}
}

// TestBuildBankSectorRejectsKeyLowOverMaxMIDI verifies values > 127 are
// rejected. A KeyLow of 200 would wrap when written to a uint8 slot.
func TestBuildBankSectorRejectsKeyLowOverMaxMIDI(t *testing.T) {
	t.Parallel()
	voices := [][]byte{testutil.MakeTestVoice("V1", 64)}
	groups := []Keygroup{{
		KeyLow:    200,
		KeyHigh:   220,
		KeyCentre: 210,
	}}
	_, err := AssembleWithKeygroups(voices, groups)
	if err == nil {
		t.Fatal("expected error for KeyLow > MaxMIDINote")
	}
	if !strings.Contains(err.Error(), "KeyLow") {
		t.Errorf("error should mention KeyLow: %v", err)
	}
}

// TestBuildBankSectorRejectsKeyHighOverMaxMIDI checks the KeyHigh bound.
func TestBuildBankSectorRejectsKeyHighOverMaxMIDI(t *testing.T) {
	t.Parallel()
	voices := [][]byte{testutil.MakeTestVoice("V1", 64)}
	groups := []Keygroup{{
		KeyLow:    60,
		KeyHigh:   200,
		KeyCentre: 72,
	}}
	_, err := AssembleWithKeygroups(voices, groups)
	if err == nil {
		t.Fatal("expected error for KeyHigh > MaxMIDINote")
	}
	if !strings.Contains(err.Error(), "KeyHigh") {
		t.Errorf("error should mention KeyHigh: %v", err)
	}
}

// TestBuildBankSectorRejectsKeyCentreOverMaxMIDI checks the KeyCentre bound.
func TestBuildBankSectorRejectsKeyCentreOverMaxMIDI(t *testing.T) {
	t.Parallel()
	voices := [][]byte{testutil.MakeTestVoice("V1", 64)}
	groups := []Keygroup{{
		KeyLow:    60,
		KeyHigh:   72,
		KeyCentre: 200,
	}}
	_, err := AssembleWithKeygroups(voices, groups)
	if err == nil {
		t.Fatal("expected error for KeyCentre > MaxMIDINote")
	}
	if !strings.Contains(err.Error(), "KeyCentre") {
		t.Errorf("error should mention KeyCentre: %v", err)
	}
}

// TestBuildBankSectorWarnsKeyCentreOutsideRange documents that a KeyCentre
// outside [KeyLow, KeyHigh] is a soft warning, not a hard error. Real SFZ
// corpora (e.g. JUNGLISM) legitimately use pitch_keycenter to transpose a
// sample beyond its key range, and the hardware DCP handles it fine.
//
// Not parallel: CaptureLog mutates the global logger.
func TestBuildBankSectorWarnsKeyCentreOutsideRange(t *testing.T) {
	buf := testutil.CaptureLog(t)
	voices := [][]byte{testutil.MakeTestVoice("PAD", 64)}
	groups := []Keygroup{{
		KeyLow:    72,
		KeyHigh:   83,
		KeyCentre: 84,
		VelLow:    disk.DefaultVelLow,
		VelHigh:   disk.DefaultVelHigh,
		AudioOut:  disk.PolyphonicAudioOut,
	}}
	if _, err := AssembleWithKeygroups(voices, groups); err != nil {
		t.Fatalf("expected success with warning, got error: %v", err)
	}
	if !testutil.BufHasWarnContaining(buf, "KeyCentre") {
		t.Errorf("expected KeyCentre warning, got: %s", buf.String())
	}
}

// TestBuildBankSectorAcceptsValidKeygroups guards against false positives.
func TestBuildBankSectorAcceptsValidKeygroups(t *testing.T) {
	t.Parallel()
	voices := [][]byte{testutil.MakeTestVoice("OK", 64)}
	groups := []Keygroup{NewKeygroup(36, 72, 60)}
	if _, err := AssembleWithKeygroups(voices, groups); err != nil {
		t.Fatalf("valid keygroup should not error: %v", err)
	}
}

// TestAssembleWritesVoiceHeaderKeyRange verifies that AssembleWithKeygroups
// writes the per-voice key range (hwid/lwid/cent at 0xae/0xaf/0xb0, spec §2-1)
// into each voice header in the assembled FZF, not just into the bank sector
// arrays (spec §2-2). Without this, voices imported via voiceimport.Encode
// would keep their DefaultKeyHigh/Low/Centre values in the FZF and tools that
// read voice-header bytes (fzv info, voiceunpack) would report stale defaults.
//
// This is the regression guard for F15, which extends F11 (sfzconvert cent)
// to cover all three key-range bytes at the voicebuild level so both the
// `fzf build` and `sfz convert` pipelines benefit.
func TestAssembleWritesVoiceHeaderKeyRange(t *testing.T) {
	t.Parallel()
	type kr struct {
		low, high, centre uint8
	}
	want := []kr{
		{low: 36, high: 48, centre: 42},
		{low: 49, high: 60, centre: 55},
		{low: 61, high: 72, centre: 66},
	}
	voices := make([][]byte, len(want))
	groups := make([]Keygroup, len(want))
	for i, k := range want {
		voices[i] = testutil.MakeTestVoice(fmt.Sprintf("V%02d", i+1), 64)
		groups[i] = NewKeygroup(k.low, k.high, k.centre)
	}

	out, err := AssembleWithKeygroups(voices, groups)
	if err != nil {
		t.Fatalf("AssembleWithKeygroups: %v", err)
	}

	// Voice area starts immediately after the bank sector.
	voiceAreaBase := disk.SectorSize
	for i, k := range want {
		slot := voiceAreaBase + disk.VoiceSlotOffset(0, i)
		gotHigh := out[slot+disk.VoiceKeyHighOffset]
		gotLow := out[slot+disk.VoiceKeyLowOffset]
		gotCent := out[slot+disk.VoiceKeyCentOffset]
		if gotHigh != k.high || gotLow != k.low || gotCent != k.centre {
			t.Errorf("voice %d header key range: got hwid=%d lwid=%d cent=%d, want %d/%d/%d",
				i+1, gotHigh, gotLow, gotCent, k.high, k.low, k.centre)
		}
	}

	// Regression guard: bank-array values (spec §2-2) must still be present.
	for i, k := range want {
		if got := out[disk.BankKeyHighOffset+i]; got != k.high {
			t.Errorf("bank KeyHigh[%d]: got %d, want %d", i, got, k.high)
		}
		if got := out[disk.BankKeyLowOffset+i]; got != k.low {
			t.Errorf("bank KeyLow[%d]: got %d, want %d", i, got, k.low)
		}
		if got := out[disk.BankKeyCentOffset+i]; got != k.centre {
			t.Errorf("bank KeyCent[%d]: got %d, want %d", i, got, k.centre)
		}
	}
}
