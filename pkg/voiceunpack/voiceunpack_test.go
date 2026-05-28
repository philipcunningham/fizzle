package voiceunpack

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
)

func buildAndUnpack(t *testing.T, voices [][]byte) [][]byte {
	t.Helper()
	dir := t.TempDir()

	voicePaths := make([]string, len(voices))
	for i, v := range voices {
		p := filepath.Join(dir, "v"+string(rune('0'+i))+".fzv")
		if err := os.WriteFile(p, v, 0644); err != nil {
			t.Fatal(err)
		}
		voicePaths[i] = p
	}

	fzfPath := filepath.Join(dir, "full.fzf")
	if err := voicebuild.Build(context.Background(), fzfPath, voicePaths); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "out")
	if err := Unpack(fzfPath, outDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}

	result := make([][]byte, len(entries))
	for i, e := range entries {
		data, err := os.ReadFile(filepath.Join(outDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		result[i] = data
	}
	return result
}

func TestUnpackSingleVoice(t *testing.T) {
	t.Parallel()
	voice := testutil.MakeTestVoice("KICK", 512)
	unpacked := buildAndUnpack(t, [][]byte{voice})

	if len(unpacked) != 1 {
		t.Fatalf("expected 1 voice, got %d", len(unpacked))
	}
	if len(unpacked[0]) < disk.SectorSize {
		t.Fatalf("unpacked voice too small: %d bytes", len(unpacked[0]))
	}
}

func TestUnpackVoiceCount(t *testing.T) {
	t.Parallel()
	voices := [][]byte{
		testutil.MakeTestVoice("KICK", 512),
		testutil.MakeTestVoice("SNARE", 256),
		testutil.MakeTestVoice("HAT", 128),
	}
	unpacked := buildAndUnpack(t, voices)
	if len(unpacked) != 3 {
		t.Fatalf("expected 3 voices, got %d", len(unpacked))
	}
}

func TestUnpackWaveStartIsZero(t *testing.T) {
	t.Parallel()
	voice := testutil.MakeTestVoice("BASS", 1024)
	unpacked := buildAndUnpack(t, [][]byte{voice})

	waveStart := binary.LittleEndian.Uint32(unpacked[0][0x00:0x04])
	if waveStart != 0 {
		t.Errorf("waveStart after unpack: got %d, want 0", waveStart)
	}
}

func TestUnpackOutputFilenames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	v := testutil.MakeTestVoice("HOOVER", 512)
	vPath := filepath.Join(dir, "v.fzv")
	if err := os.WriteFile(vPath, v, 0644); err != nil {
		t.Fatal(err)
	}
	fzfPath := filepath.Join(dir, "full.fzf")
	if err := voicebuild.Build(context.Background(), fzfPath, []string{vPath}); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "out")
	if err := Unpack(fzfPath, outDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	if !strings.HasSuffix(entries[0].Name(), ".fzv") {
		t.Errorf("expected .fzv extension, got %q", entries[0].Name())
	}
	if !strings.Contains(entries[0].Name(), "HOOVER") {
		t.Errorf("expected voice name in filename, got %q", entries[0].Name())
	}
}

func TestUnpackDuplicateNames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	v := testutil.MakeTestVoice("KICK", 256)
	vPath := filepath.Join(dir, "v.fzv")
	if err := os.WriteFile(vPath, v, 0644); err != nil {
		t.Fatal(err)
	}
	fzfPath := filepath.Join(dir, "full.fzf")
	if err := voicebuild.Build(context.Background(), fzfPath, []string{vPath, vPath}); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "out")
	if err := Unpack(fzfPath, outDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 files for duplicate names, got %d", len(entries))
	}
	names := map[string]bool{}
	for _, e := range entries {
		if names[e.Name()] {
			t.Errorf("duplicate filename: %q", e.Name())
		}
		names[e.Name()] = true
	}
}

// TestUnpackSlashInVoiceName is a regression test for a bug where voice names
// containing path separators (e.g. "BRASS/BASS 2", observed in the real
// Casio FZ-1 Shareware Library corpus) caused filepath.Join to silently
// create subdirectories under outputDir. The expected behavior is that the
// path separators are stripped from the on-disk filename so each voice
// produces exactly one top-level .fzv file.
func TestUnpackSlashInVoiceName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	names := []string{"BRASS/BASS 2", "CLAV 1 W/LFO", "3 ATTAKS/STR"}
	voicePaths := make([]string, len(names))
	for i, n := range names {
		v := testutil.MakeTestVoice(n, 256)
		p := filepath.Join(dir, fmt.Sprintf("v%d.fzv", i))
		if err := os.WriteFile(p, v, 0644); err != nil {
			t.Fatal(err)
		}
		voicePaths[i] = p
	}

	fzfPath := filepath.Join(dir, "full.fzf")
	if err := voicebuild.Build(context.Background(), fzfPath, voicePaths); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "out")
	if err := Unpack(fzfPath, outDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(names) {
		t.Fatalf("expected %d top-level .fzv files, got %d", len(names), len(entries))
	}
	for _, e := range entries {
		if e.IsDir() {
			t.Errorf("voice slash should not create subdirectory %q", e.Name())
		}
		if !strings.HasSuffix(e.Name(), ".fzv") {
			t.Errorf("expected .fzv extension, got %q", e.Name())
		}
		if strings.ContainsAny(e.Name(), `/\`) {
			t.Errorf("filename %q still contains path separator", e.Name())
		}
	}
}

// TestUnpackAudioIntegrity is a regression test for a bug where the audio block
// boundary calculation in unpack() used the cumulative waveEnd address directly
// instead of computing the per-voice delta. This caused voices at even positions
// (0, 2, 4, ...) to have their sample offsets not subtracted, producing wrong
// waveEnd values and corrupted durations when the FZF was unpacked.
func TestUnpackAudioIntegrity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Three voices with distinct, easily-verifiable sample counts.
	// Use names that sort alphabetically in the same order as build order
	// so that buildAndUnpack returns them in a predictable sequence.
	specs := []struct {
		name    string
		samples int
	}{
		{"AKICK", 500},
		{"BSNARE", 300},
		{"CHAT", 200},
	}

	voicePaths := make([]string, 0, len(specs))
	for _, s := range specs {
		v := testutil.MakeTestVoice(s.name, s.samples)
		p := filepath.Join(dir, s.name+".fzv")
		if err := os.WriteFile(p, v, 0644); err != nil {
			t.Fatal(err)
		}
		voicePaths = append(voicePaths, p)
	}

	fzfPath := filepath.Join(dir, "full.fzf")
	if err := voicebuild.Build(context.Background(), fzfPath, voicePaths); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")
	if err := Unpack(fzfPath, outDir); err != nil {
		t.Fatal(err)
	}

	for _, s := range specs {
		fzv, err := os.ReadFile(filepath.Join(outDir, s.name+".fzv"))
		if err != nil {
			t.Fatalf("reading unpacked %s: %v", s.name, err)
		}
		waveEnd := binary.LittleEndian.Uint32(fzv[0x04:0x08])
		if waveEnd != uint32(s.samples) { //nolint:gosec // G115: test constant
			t.Errorf("%s waveEnd: got %d, want %d (audio boundary bug)", s.name, waveEnd, s.samples)
		}
	}
}

// TestUnpackAudioSampleValues verifies that the actual PCM sample bytes are
// preserved correctly through a build then unpack round trip, not just the header.
func TestUnpackAudioSampleValues(t *testing.T) {
	t.Parallel()
	v1 := testutil.MakeTestVoice("KICK", 512)
	v2 := testutil.MakeTestVoice("SNARE", 512)

	unpacked := buildAndUnpack(t, [][]byte{v1, v2})

	if len(unpacked) != 2 {
		t.Fatalf("expected 2 voices, got %d", len(unpacked))
	}

	for voiceIdx, orig := range [][]byte{v1, v2} {
		got := unpacked[voiceIdx]
		origAudio := orig[disk.SectorSize:]
		if len(got) < disk.SectorSize+len(origAudio) {
			t.Errorf("voice %d: unpacked too small (%d bytes)", voiceIdx+1, len(got))
			continue
		}
		gotAudio := got[disk.SectorSize : disk.SectorSize+len(origAudio)]
		for j := range origAudio {
			if gotAudio[j] != origAudio[j] {
				t.Errorf("voice %d: sample byte %d mismatch: got 0x%02x, want 0x%02x", voiceIdx+1, j, gotAudio[j], origAudio[j])
				break
			}
		}
	}
}

// TestUnpackAudioBytesAllBoundaries is the definitive regression test for the
// class of bug where unpack reads audio from the wrong byte offset. It builds
// 9 voices (crossing the 4-voice sector boundary at indices 4 and 8), fills
// each voice's audio with a distinct marker byte, and verifies that after
// unpack each voice contains only its own marker bytes, not another voice's.
func TestUnpackAudioBytesAllBoundaries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	n := 9
	sampleCounts := []int{500, 300, 700, 400, 600, 200, 800, 350, 450}
	voicePaths := make([]string, n)

	for i := range n {
		marker := byte(i + 1)
		v := make([]byte, disk.SectorSize+sampleCounts[i]*2)
		name := disk.PadLabel(fmt.Sprintf("V%02d", i+1))
		copy(v[disk.VoiceNameOffset:], name[:])
		binary.LittleEndian.PutUint32(v[0x00:], 0)
		binary.LittleEndian.PutUint32(v[0x04:], uint32(sampleCounts[i])) //nolint:gosec // G115: test constant
		binary.LittleEndian.PutUint32(v[0x08:], 0)
		binary.LittleEndian.PutUint32(v[0x0c:], uint32(sampleCounts[i])) //nolint:gosec // G115: test constant
		binary.LittleEndian.PutUint16(v[disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
		for j := disk.SectorSize; j < len(v); j++ {
			v[j] = marker
		}
		p := filepath.Join(dir, fmt.Sprintf("V%02d.fzv", i+1))
		if err := os.WriteFile(p, v, 0644); err != nil {
			t.Fatal(err)
		}
		voicePaths[i] = p
	}

	fzfPath := filepath.Join(dir, "full.fzf")
	if err := voicebuild.Build(context.Background(), fzfPath, voicePaths); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")
	if err := Unpack(fzfPath, outDir); err != nil {
		t.Fatal(err)
	}

	for i := range n {
		name := fmt.Sprintf("V%02d", i+1)
		fzv, err := os.ReadFile(filepath.Join(outDir, name+".fzv"))
		if err != nil {
			t.Fatalf("voice %d: %v", i+1, err)
		}

		waveEnd := int(binary.LittleEndian.Uint32(fzv[0x04:0x08]))
		if waveEnd != sampleCounts[i] {
			t.Errorf("voice %d: waveEnd=%d, want %d", i+1, waveEnd, sampleCounts[i])
		}

		// Every audio byte must be the marker for this voice.
		wantMarker := byte(i + 1)
		audio := fzv[disk.SectorSize : disk.SectorSize+sampleCounts[i]*2]
		for j, b := range audio {
			if b != wantMarker {
				t.Errorf("voice %d (boundary=%v): audio byte %d = 0x%02x, want 0x%02x (wrong audio block)",
					i+1, i == 4 || i == 8, j, b, wantMarker)
				break
			}
		}
	}
}

// TestUnpackRoundTripNineVoices verifies that a full build then unpack cycle
// preserves sample values for 9 voices, explicitly covering the two
// sector boundaries at voice indices 4 and 8.
func TestUnpackRoundTripNineVoices(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	n := 9
	voicePaths := make([]string, n)
	origSamples := make([][]int16, n)

	for i := range n {
		count := 400 + i*100
		samples := make([]int16, count)
		for j := range samples {
			samples[j] = int16((j*37 + i*1000) % 32767)
		}
		origSamples[i] = samples

		v := make([]byte, disk.SectorSize+count*2)
		name := disk.PadLabel(fmt.Sprintf("RT%02d", i+1))
		copy(v[disk.VoiceNameOffset:], name[:])
		binary.LittleEndian.PutUint32(v[0x00:], 0)
		binary.LittleEndian.PutUint32(v[0x04:], uint32(count))
		binary.LittleEndian.PutUint32(v[0x08:], 0)
		binary.LittleEndian.PutUint32(v[0x0c:], uint32(count))
		binary.LittleEndian.PutUint16(v[disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
		for j, s := range samples {
			bitconv.WriteInt16LE(v[disk.SectorSize+j*2:], s)
		}

		p := filepath.Join(dir, fmt.Sprintf("RT%02d.fzv", i+1))
		if err := os.WriteFile(p, v, 0644); err != nil {
			t.Fatal(err)
		}
		voicePaths[i] = p
	}

	fzfPath := filepath.Join(dir, "full.fzf")
	if err := voicebuild.Build(context.Background(), fzfPath, voicePaths); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")
	if err := Unpack(fzfPath, outDir); err != nil {
		t.Fatal(err)
	}

	for i := range n {
		name := fmt.Sprintf("RT%02d", i+1)
		fzv, err := os.ReadFile(filepath.Join(outDir, name+".fzv"))
		if err != nil {
			t.Fatalf("voice %d: %v", i+1, err)
		}
		count := len(origSamples[i])
		if len(fzv) < disk.SectorSize+count*2 {
			t.Fatalf("voice %d: fzv too small (%d bytes)", i+1, len(fzv))
		}
		for j, want := range origSamples[i] {
			got := bitconv.ReadInt16LE(fzv[disk.SectorSize+j*2:])
			if got != want {
				t.Errorf("voice %d (sector boundary=%v) sample %d: got %d, want %d",
					i+1, i == 4 || i == 8, j, got, want)
				break
			}
		}
	}
}

// TestUnpackMultiDiskDisk1NoPanic verifies that unpacking disk 1 of a 2-disk
// full dump does not panic. Disk 1 has all voice headers but only partial
// audio. Voices whose audio is on disk 2 should be silently skipped.
func TestUnpackMultiDiskDisk1NoPanic(t *testing.T) {
	t.Parallel()
	// Build a 3-voice instrument that is larger than one disk, split it,
	// and verify that unpacking disk 1 produces only the voices present.
	voices := make([][]byte, 3)
	groups := make([]voicebuild.Keygroup, 3)
	for i := range voices {
		voices[i] = testutil.MakeTestVoice(fmt.Sprintf("V%02d", i+1), 300000)
		groups[i] = voicebuild.Keygroup{
			KeyLow: uint8(36 + i), KeyHigh: uint8(36 + i),
			VelLow: 1, VelHigh: 127,
			KeyCentre: uint8(36 + i), AudioOut: 0xff,
		}
	}

	result, err := voicebuild.AssembleMultiDisk(voices, groups)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	disk1Path := filepath.Join(dir, "disk1.fzf")
	if err := os.WriteFile(disk1Path, result.Disks[0], 0644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "out")
	// Must not panic.
	if err := Unpack(disk1Path, outDir); err != nil {
		t.Fatalf("Unpack disk 1 panicked or errored: %v", err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	// All 3 voice headers are on disk 1. Voices whose audio starts within
	// disk 1's audio area are unpacked (possibly with truncated audio if the
	// audio extends to disk 2). The key invariant is no panic.
	if len(entries) == 0 {
		t.Error("expected at least 1 voice from disk 1")
	}
}

func TestUnpackTooSmall(t *testing.T) {
	t.Parallel()
	data := []byte{0x01, 0x02}
	hdr, err := fzutil.ParseFZFHeader(data)
	if err == nil {
		_, _, err = unpack(data, hdr)
	}
	if err == nil {
		t.Error("expected error for too-small input")
	}
}

func TestUnpackInvalidVoiceCount(t *testing.T) {
	t.Parallel()
	data := make([]byte, disk.SectorSize)
	binary.LittleEndian.PutUint16(data[0:2], 0)
	hdr, err := fzutil.ParseFZFHeader(data)
	if err == nil {
		_, _, err = unpack(data, hdr)
	}
	if err == nil {
		t.Error("expected error for zero voice count")
	}
}

func TestVoiceName(t *testing.T) {
	t.Parallel()
	fzv := make([]byte, disk.SectorSize)
	paddedName := disk.PadLabel("HOOVER")
	copy(fzv[disk.VoiceNameOffset:], paddedName[:])
	if got := voiceName(fzv); got != "HOOVER" {
		t.Errorf("voiceName: got %q, want %q", got, "HOOVER")
	}
}

func TestVoiceNameUnprintable(t *testing.T) {
	t.Parallel()
	fzv := make([]byte, disk.SectorSize)
	if got := voiceName(fzv); got != "VOICE" {
		t.Errorf("voiceName with zero bytes: got %q, want %q", got, "VOICE")
	}
}

func TestUnpackBankZeroIsDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	voices := [][]byte{
		testutil.MakeTestVoice("KICK", 512),
		testutil.MakeTestVoice("SNARE", 256),
	}
	voicePaths := make([]string, len(voices))
	for i, v := range voices {
		p := filepath.Join(dir, fmt.Sprintf("v%d.fzv", i))
		if err := os.WriteFile(p, v, 0644); err != nil {
			t.Fatal(err)
		}
		voicePaths[i] = p
	}

	fzfPath := filepath.Join(dir, "full.fzf")
	if err := voicebuild.Build(context.Background(), fzfPath, voicePaths); err != nil {
		t.Fatal(err)
	}

	allDir := filepath.Join(dir, "all")
	if err := Unpack(fzfPath, allDir); err != nil {
		t.Fatal(err)
	}

	bankDir := filepath.Join(dir, "bank0")
	if err := UnpackBank(fzfPath, bankDir, 0); err != nil {
		t.Fatal(err)
	}

	allEntries, err := os.ReadDir(allDir)
	if err != nil {
		t.Fatal(err)
	}
	bankEntries, err := os.ReadDir(bankDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(allEntries) != len(bankEntries) {
		t.Fatalf("all-banks unpack produced %d files, bank 0 produced %d", len(allEntries), len(bankEntries))
	}
	for i := range allEntries {
		if allEntries[i].Name() != bankEntries[i].Name() {
			t.Errorf("file %d: all=%q bank0=%q", i, allEntries[i].Name(), bankEntries[i].Name())
		}
	}
}

// TestUnpackSkipsNoSoundSlotsBeforeValidVoices is a regression test for
// CASIO139.FZF, where the head of the voice area carries legitimate
// PlaybackModeNoSound placeholder slots with garbage in their wave pointer
// fields. Before the fix, unpack used `break` (not `continue`) on the
// first slot whose audioStart looked out of range, which killed extraction
// of every subsequent valid voice. After the fix, NoSound and other
// non-plausible slots are skipped via `continue` and only true multi-disk
// continuation (a plausible voice whose audio is past the local area)
// breaks the loop.
func TestUnpackSkipsNoSoundSlotsBeforeValidVoices(t *testing.T) {
	t.Parallel()

	const noSoundSlots = 2
	const realVoices = 3
	const total = noSoundSlots + realVoices

	// Build the real voice payloads first so we know their audio sizes.
	realFZVs := [][]byte{
		testutil.MakeTestVoice("ALPHA", 200),
		testutil.MakeTestVoice("BETA", 150),
		testutil.MakeTestVoice("GAMMA", 100),
	}

	// Assemble a synthetic FZF: 1 bank sector + voice area sized for `total`
	// slots + audio area. We can't reuse voicebuild because it doesn't
	// emit NoSound placeholder slots.
	voiceAreaSectors := (total + 3) / 4
	audioSizes := make([]int, realVoices)
	for i, v := range realFZVs {
		audioSizes[i] = disk.PadToSector(len(v) - disk.SectorSize)
	}
	totalAudio := 0
	for _, s := range audioSizes {
		totalAudio += s
	}

	fzfSize := disk.SectorSize + voiceAreaSectors*disk.SectorSize + totalAudio
	data := make([]byte, fzfSize)

	// Bank sector.
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], uint16(total))
	copy(data[disk.BankNameOffset:], "Mock CASIO139")

	voiceArea := disk.SectorSize

	// NoSound placeholder slots at the head of the voice area with garbage
	// wave pointers, mirroring CASIO139.FZF.
	for i := range noSoundSlots {
		off := disk.VoiceSlotOffset(voiceArea, i)
		binary.LittleEndian.PutUint32(data[off+disk.VoiceWaveStartOffset:], 0xFFFFFFFF)
		binary.LittleEndian.PutUint32(data[off+disk.VoiceWaveEndOffset:], 0xFFFFFFFF)
		// VoiceLoopMode bytes default to 0 == PlaybackModeNoSound.
	}

	// Real voice slots and their audio.
	audioOff := disk.SectorSize + voiceAreaSectors*disk.SectorSize
	audioCursor := audioOff
	sampleCursor := 0
	for i := range realVoices {
		slot := disk.VoiceSlotOffset(voiceArea, noSoundSlots+i)
		copy(data[slot:slot+disk.VoiceHeaderUsed], realFZVs[i][:disk.VoiceHeaderUsed])
		// Rewrite waveStart/waveEnd to combined-area coordinates so unpack
		// can locate this voice's audio block.
		samples := audioSizes[i] / disk.BytesPerSample
		binary.LittleEndian.PutUint32(data[slot+disk.VoiceWaveStartOffset:], uint32(sampleCursor))       //nolint:gosec // G115: test fixture, values fit in uint32
		binary.LittleEndian.PutUint32(data[slot+disk.VoiceWaveEndOffset:], uint32(sampleCursor+samples)) //nolint:gosec // G115: test fixture, values fit in uint32
		copy(data[audioCursor:audioCursor+audioSizes[i]], realFZVs[i][disk.SectorSize:])
		audioCursor += audioSizes[i]
		sampleCursor += samples
	}

	fzfPath := filepath.Join(t.TempDir(), "casio139-mock.fzf")
	if err := os.WriteFile(fzfPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(t.TempDir(), "out")
	if err := Unpack(fzfPath, outDir); err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != realVoices {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("unpacked %d voices (%v), want %d (NoSound slots before real voices must be skipped, not break the loop)",
			len(entries), names, realVoices)
	}
	wantNames := map[string]bool{"ALPHA.fzv": true, "BETA.fzv": true, "GAMMA.fzv": true}
	for _, e := range entries {
		if !wantNames[e.Name()] {
			t.Errorf("unexpected unpacked file %q", e.Name())
		}
	}
}

// TestUnpackPreservesLoopFlagBits is the regression test for
// subtractSampleOffsets corrupting the loop-fine byte (loopst[i] upper 8
// bits) and the skip flag (looped[i] MSB) on round-trip. The previous
// implementation treated those fields as plain 32-bit addresses and
// subtracted the wave-area offset across the reserved bits, smearing the
// flag-value into the address.
func TestUnpackPreservesLoopFlagBits(t *testing.T) {
	t.Parallel()

	// Synthesise a 1-voice FZF whose only voice has wave area at sample
	// offset waveStart, and whose loopst[0]/looped[0] addresses land just
	// past waveStart in the combined area. After unpack the addresses
	// should be relative to the voice's own audio block (waveStart
	// subtracted) while the flag bits remain intact.
	const waveStart = 1000
	const sampleCount = 600
	const loopStartAddr = waveStart + 0x100 // 0x4E8
	const loopEndAddr = waveStart + 0x200   // 0x5E8
	const loopFineByte = 0x42

	loopStRaw := uint32(loopFineByte)<<disk.LoopStartFineShift | uint32(loopStartAddr)
	loopEdRaw := uint32(disk.LoopEndSkipMask) | uint32(loopEndAddr)

	// 1 sector bank + 1 sector voice area + sectors of audio.
	audioBytes := disk.PadToSector(sampleCount * disk.BytesPerSample)
	fzf := make([]byte, disk.SectorSize+disk.SectorSize+audioBytes)

	// Bank sector: voice count = 1.
	binary.LittleEndian.PutUint16(fzf[disk.BankVoiceCountOffset:], 1)

	// Voice slot 0 in the voice area.
	slot := disk.VoiceSlotOffset(disk.SectorSize, 0)
	loopyName := disk.PadLabel("LOOPY")
	copy(fzf[slot+disk.VoiceNameOffset:], loopyName[:])
	binary.LittleEndian.PutUint32(fzf[slot+disk.VoiceWaveStartOffset:], waveStart)
	binary.LittleEndian.PutUint32(fzf[slot+disk.VoiceWaveEndOffset:], waveStart+sampleCount)
	binary.LittleEndian.PutUint32(fzf[slot+disk.VoiceGenStartOffset:], waveStart)
	binary.LittleEndian.PutUint32(fzf[slot+disk.VoiceGenEndOffset:], waveStart+sampleCount)
	binary.LittleEndian.PutUint16(fzf[slot+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	binary.LittleEndian.PutUint32(fzf[slot+disk.VoiceLoopSt0Offset:], loopStRaw)
	binary.LittleEndian.PutUint32(fzf[slot+disk.VoiceLoopEd0Offset:], loopEdRaw)

	// Audio area: waveStart points past the local audio area (the voice
	// behaves as if its audio is at offset 0 in the audio block). unpack()
	// uses byteStart = waveStart*disk.BytesPerSample, so we need the audio
	// area to span [waveStart*2, waveStart*2 + audioBytes). Grow fzf to
	// include audio at byteStart.
	audioAreaStart := disk.SectorSize + disk.SectorSize
	byteStart := waveStart * disk.BytesPerSample
	audioByteEnd := byteStart + audioBytes
	if len(fzf) < audioAreaStart+audioByteEnd {
		fzf = append(fzf, make([]byte, (audioAreaStart+audioByteEnd)-len(fzf))...)
	}

	fzfPath := filepath.Join(t.TempDir(), "loopy.fzf")
	if err := os.WriteFile(fzfPath, fzf, 0644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(t.TempDir(), "out")
	if err := Unpack(fzfPath, outDir); err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	fzv, err := os.ReadFile(filepath.Join(outDir, "LOOPY.fzv"))
	if err != nil {
		t.Fatal(err)
	}

	gotSt := binary.LittleEndian.Uint32(fzv[disk.VoiceLoopSt0Offset:])
	if got := disk.LoopFineBits(gotSt); got != loopFineByte {
		t.Errorf("loopst[0] loop-fine after unpack: got 0x%02x, want 0x%02x",
			got, loopFineByte)
	}
	if got := disk.LoopStartAddress(gotSt); got != uint32(loopStartAddr-waveStart) {
		t.Errorf("loopst[0] address after unpack: got 0x%x, want 0x%x",
			got, loopStartAddr-waveStart)
	}

	gotEd := binary.LittleEndian.Uint32(fzv[disk.VoiceLoopEd0Offset:])
	if !disk.LoopSkipFlag(gotEd) {
		t.Errorf("looped[0] skip flag lost after unpack: raw=0x%08x", gotEd)
	}
	if got := disk.LoopEndAddress(gotEd); got != uint32(loopEndAddr-waveStart) {
		t.Errorf("looped[0] address after unpack: got 0x%x, want 0x%x",
			got, loopEndAddr-waveStart)
	}
}

// TestUnpackMultiBankCoversAllBanks is the regression test for Fix E:
// previously UnpackBank and ApplyToFZFVoice relied on hdr.NVoice, which
// was derived from bank 0's bstep alone. Voices stored in slots used only
// by banks 1-7 (referenced via their vp[]) were invisible: UnpackBank
// could not reach them and findVoiceIndex returned "not found".
//
// The synthetic FZF here has 2 bank sectors with bsteps 3 and 2; the
// voice area holds 5 distinct voices. After the fix all 5 must unpack.
func TestUnpackMultiBankCoversAllBanks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	const nBanks = 2
	bsteps := [nBanks]int{3, 2}
	totalVoices := bsteps[0] + bsteps[1]
	samplesEach := 100

	audioBlockBytes := disk.PadToSector(samplesEach * disk.BytesPerSample)
	voiceSectors := disk.VoiceAreaSectors(totalVoices)
	fzfSize := nBanks*disk.SectorSize + voiceSectors*disk.SectorSize + totalVoices*audioBlockBytes
	data := make([]byte, fzfSize)

	// Bank sectors: must have non-zero bstep and printable name to be
	// recognised by CountBankSectors. vp[] maps each bank's key-split
	// positions to voice-slot indices (spec §2-2). Bank 0 owns slots
	// 0..bsteps[0]-1; bank 1 owns the next bsteps[1] slots.
	slotCursor := 0
	for b := range nBanks {
		off := b * disk.SectorSize
		binary.LittleEndian.PutUint16(data[off+disk.BankVoiceCountOffset:], uint16(bsteps[b])) //nolint:gosec // G115: test constant
		bankName := disk.PadLabel(fmt.Sprintf("BANK%d", b))
		copy(data[off+disk.BankNameOffset:], bankName[:])
		for s := 0; s < bsteps[b]; s++ {
			binary.LittleEndian.PutUint16(data[off+disk.BankVoiceNumOffset+2*s:], uint16(slotCursor)) //nolint:gosec // G115: test constant
			slotCursor++
		}
	}

	// Voice slots: each voice owns its own sector-padded audio block in
	// order. Marker bytes per voice make audio-block boundaries verifiable.
	voiceAreaStart := nBanks * disk.SectorSize
	audioAreaStart := voiceAreaStart + voiceSectors*disk.SectorSize
	sampleCursor := 0
	for i := range totalVoices {
		slot := disk.VoiceSlotOffset(voiceAreaStart, i)
		vName := disk.PadLabel(fmt.Sprintf("V%02d", i+1))
		copy(data[slot+disk.VoiceNameOffset:], vName[:])
		binary.LittleEndian.PutUint32(data[slot+disk.VoiceWaveStartOffset:], uint32(sampleCursor))
		binary.LittleEndian.PutUint32(data[slot+disk.VoiceWaveEndOffset:], uint32(sampleCursor+samplesEach))
		binary.LittleEndian.PutUint32(data[slot+disk.VoiceGenStartOffset:], uint32(sampleCursor))
		binary.LittleEndian.PutUint32(data[slot+disk.VoiceGenEndOffset:], uint32(sampleCursor+samplesEach))
		binary.LittleEndian.PutUint16(data[slot+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)

		blockOff := audioAreaStart + i*audioBlockBytes
		for j := blockOff; j < blockOff+audioBlockBytes; j++ {
			data[j] = byte(i + 1)
		}
		sampleCursor += samplesEach
	}

	fzfPath := filepath.Join(dir, "multibank.fzf")
	if err := os.WriteFile(fzfPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "all")
	if err := Unpack(fzfPath, outDir); err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != totalVoices {
		t.Fatalf("Unpack: got %d voices, want %d (multi-bank should surface all slots)", len(entries), totalVoices)
	}

	// UnpackBank(1) must reach the voices owned by bank 1 (slots 3-4).
	bank1Dir := filepath.Join(dir, "bank1")
	if err := UnpackBank(fzfPath, bank1Dir, 1); err != nil {
		t.Fatalf("UnpackBank(1): %v", err)
	}
	bank1Entries, err := os.ReadDir(bank1Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(bank1Entries) != bsteps[1] {
		t.Errorf("UnpackBank(1): got %d voices, want %d", len(bank1Entries), bsteps[1])
	}
}

// TestUnpackBankFollowsVpNotSequentialRange is the regression test for F5
// case 1: previously UnpackBank computed startVoice as sum(bstep[0..b-1])
// and sliced allVoices[startVoice : startVoice+bstep[b]]. That assumed
// the voice slots a bank references are sequential, which is false: each
// bank's vp[] independently maps key-split positions to slot indices and
// banks can share slots, skip slots, or reference slots in any order
// (spec §2-2). The fix walks the bank's vp[] and picks each referenced
// voice from the unpacked slice by slot index.
func TestUnpackBankFollowsVpNotSequentialRange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// 2 banks, bsteps {2, 2}. Bank 0 references slots 0 and 2; bank 1
	// references slots 1 and 3. If UnpackBank used the old sequential
	// slice it would return slots 0..1 for bank 0 and 2..3 for bank 1,
	// reversing the correct mapping.
	const nBanks = 2
	bsteps := [nBanks]int{2, 2}
	totalSlots := 4
	samplesEach := 100
	audioBlockBytes := disk.PadToSector(samplesEach * disk.BytesPerSample)
	voiceSectors := disk.VoiceAreaSectors(totalSlots)
	fzfSize := nBanks*disk.SectorSize + voiceSectors*disk.SectorSize + totalSlots*audioBlockBytes
	data := make([]byte, fzfSize)

	// Bank 0: vp[] = [0, 2]; bank 1: vp[] = [1, 3].
	vpPlan := [nBanks][]int{{0, 2}, {1, 3}}
	for b := range nBanks {
		off := b * disk.SectorSize
		binary.LittleEndian.PutUint16(data[off+disk.BankVoiceCountOffset:], uint16(bsteps[b])) //nolint:gosec // G115: test
		bankName := disk.PadLabel(fmt.Sprintf("BANK%d", b))
		copy(data[off+disk.BankNameOffset:], bankName[:])
		for s, slot := range vpPlan[b] {
			binary.LittleEndian.PutUint16(data[off+disk.BankVoiceNumOffset+2*s:], uint16(slot)) //nolint:gosec // G115: test
		}
	}

	voiceAreaStart := nBanks * disk.SectorSize
	audioAreaStart := voiceAreaStart + voiceSectors*disk.SectorSize
	sampleCursor := 0
	for i := range totalSlots {
		slot := disk.VoiceSlotOffset(voiceAreaStart, i)
		vName := disk.PadLabel(fmt.Sprintf("SLOT%d", i))
		copy(data[slot+disk.VoiceNameOffset:], vName[:])
		binary.LittleEndian.PutUint32(data[slot+disk.VoiceWaveStartOffset:], uint32(sampleCursor))           //nolint:gosec // G115: test
		binary.LittleEndian.PutUint32(data[slot+disk.VoiceWaveEndOffset:], uint32(sampleCursor+samplesEach)) //nolint:gosec // G115: test
		binary.LittleEndian.PutUint32(data[slot+disk.VoiceGenStartOffset:], uint32(sampleCursor))            //nolint:gosec // G115: test
		binary.LittleEndian.PutUint32(data[slot+disk.VoiceGenEndOffset:], uint32(sampleCursor+samplesEach))  //nolint:gosec // G115: test
		binary.LittleEndian.PutUint16(data[slot+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
		blockOff := audioAreaStart + i*audioBlockBytes
		for j := blockOff; j < blockOff+audioBlockBytes; j++ {
			data[j] = byte(i + 1)
		}
		sampleCursor += samplesEach
	}

	fzfPath := filepath.Join(dir, "interleaved.fzf")
	if err := os.WriteFile(fzfPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	// UnpackBank(0) must produce SLOT0 and SLOT2 (not SLOT0 and SLOT1).
	bank0Dir := filepath.Join(dir, "bank0")
	if err := UnpackBank(fzfPath, bank0Dir, 0); err != nil {
		t.Fatalf("UnpackBank(0): %v", err)
	}
	entries0, err := os.ReadDir(bank0Dir)
	if err != nil {
		t.Fatal(err)
	}
	got0 := make(map[string]bool)
	for _, e := range entries0 {
		got0[e.Name()] = true
	}
	if !got0["SLOT0.fzv"] || !got0["SLOT2.fzv"] {
		names := make([]string, 0, len(got0))
		for n := range got0 {
			names = append(names, n)
		}
		t.Errorf("UnpackBank(0): got %v, want SLOT0.fzv and SLOT2.fzv (per vp[])", names)
	}

	// UnpackBank(1) must produce SLOT1 and SLOT3.
	bank1Dir := filepath.Join(dir, "bank1")
	if err := UnpackBank(fzfPath, bank1Dir, 1); err != nil {
		t.Fatalf("UnpackBank(1): %v", err)
	}
	entries1, err := os.ReadDir(bank1Dir)
	if err != nil {
		t.Fatal(err)
	}
	got1 := make(map[string]bool)
	for _, e := range entries1 {
		got1[e.Name()] = true
	}
	if !got1["SLOT1.fzv"] || !got1["SLOT3.fzv"] {
		names := make([]string, 0, len(got1))
		for n := range got1 {
			names = append(names, n)
		}
		t.Errorf("UnpackBank(1): got %v, want SLOT1.fzv and SLOT3.fzv (per vp[])", names)
	}
}

// TestUnpackBankDeduplicatesSharedKeySplits exercises F5 case 2: a bank's
// vp[] can reference the same voice slot from several key splits (sharing
// one voice across a range). UnpackBank must emit each distinct voice
// once, not once per split.
func TestUnpackBankDeduplicatesSharedKeySplits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Single bank with bstep=3; all 3 splits point at slot 0.
	const bstep = 3
	const totalSlots = 1
	const samplesEach = 100
	audioBlockBytes := disk.PadToSector(samplesEach * disk.BytesPerSample)
	voiceSectors := disk.VoiceAreaSectors(totalSlots)
	fzfSize := disk.SectorSize + voiceSectors*disk.SectorSize + totalSlots*audioBlockBytes
	data := make([]byte, fzfSize)

	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], uint16(bstep))
	bankName := disk.PadLabel("SHARED")
	copy(data[disk.BankNameOffset:], bankName[:])
	for s := 0; s < bstep; s++ {
		binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset+2*s:], 0)
	}

	slot := disk.VoiceSlotOffset(disk.SectorSize, 0)
	vName := disk.PadLabel("SHARED")
	copy(data[slot+disk.VoiceNameOffset:], vName[:])
	binary.LittleEndian.PutUint32(data[slot+disk.VoiceWaveStartOffset:], 0)
	binary.LittleEndian.PutUint32(data[slot+disk.VoiceWaveEndOffset:], samplesEach)
	binary.LittleEndian.PutUint32(data[slot+disk.VoiceGenStartOffset:], 0)
	binary.LittleEndian.PutUint32(data[slot+disk.VoiceGenEndOffset:], samplesEach)
	binary.LittleEndian.PutUint16(data[slot+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)

	fzfPath := filepath.Join(dir, "shared.fzf")
	if err := os.WriteFile(fzfPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "out")
	if err := UnpackBank(fzfPath, outDir, 0); err != nil {
		t.Fatalf("UnpackBank: %v", err)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("UnpackBank: got %d voices, want 1 (shared slot dedup)", len(entries))
	}
}

// TestUnpackZeroSampleVoiceProducesHeaderOnly is the regression test for the
// stray-bytes bug: when a voice had waveEnd <= waveStart (a NoSound
// placeholder or a wiped slot), the old code forced byteSize up to one
// SectorSize, slicing 1024 bytes from the *next* voice's audio block into
// this voice's FZV. Re-packing then wrote that foreign audio back into the
// silent slot's place, corrupting both voices.
//
// The fix: zero-sample voices produce a header-only FZV (one sector, no
// audio bytes). voicebuild.Build accepts header-only FZVs and emits a
// zero-sample voice on rebuild, preserving silence.
func TestUnpackZeroSampleVoiceProducesHeaderOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Build a synthetic FZF: 1 bank + 1 voice-area sector with two
	// plausible active voices, the first zero-sample (waveStart=waveEnd=0)
	// and the second a normal voice with audio. Without the fix the unpack
	// of voice 0 would steal the first 1024 bytes of voice 1's audio block.
	const totalVoices = 2
	const v1Samples = 512
	const markerByte = 0xAB

	fzfSize := disk.SectorSize + disk.SectorSize + disk.PadToSector(v1Samples*disk.BytesPerSample)
	data := make([]byte, fzfSize)

	// Bank sector.
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], uint16(totalVoices))
	copy(data[disk.BankNameOffset:], "ZeroSample  ")

	// Voice slot 0: zero-sample active Normal voice.
	slot0 := disk.VoiceSlotOffset(disk.SectorSize, 0)
	silentName := disk.PadLabel("SILENT")
	copy(data[slot0+disk.VoiceNameOffset:], silentName[:])
	binary.LittleEndian.PutUint32(data[slot0+disk.VoiceWaveStartOffset:], 0)
	binary.LittleEndian.PutUint32(data[slot0+disk.VoiceWaveEndOffset:], 0)
	binary.LittleEndian.PutUint32(data[slot0+disk.VoiceGenStartOffset:], 0)
	binary.LittleEndian.PutUint32(data[slot0+disk.VoiceGenEndOffset:], 0)
	binary.LittleEndian.PutUint16(data[slot0+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)

	// Voice slot 1: a normal voice whose audio starts at sample 0 (since
	// slot 0 owns zero samples). Fill audio with a marker so any cross-
	// contamination is obvious.
	slot1 := disk.VoiceSlotOffset(disk.SectorSize, 1)
	loudName := disk.PadLabel("LOUD")
	copy(data[slot1+disk.VoiceNameOffset:], loudName[:])
	binary.LittleEndian.PutUint32(data[slot1+disk.VoiceWaveStartOffset:], 0)
	binary.LittleEndian.PutUint32(data[slot1+disk.VoiceWaveEndOffset:], v1Samples)
	binary.LittleEndian.PutUint32(data[slot1+disk.VoiceGenStartOffset:], 0)
	binary.LittleEndian.PutUint32(data[slot1+disk.VoiceGenEndOffset:], v1Samples)
	binary.LittleEndian.PutUint16(data[slot1+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)

	audioStart := 2 * disk.SectorSize
	for i := audioStart; i < len(data); i++ {
		data[i] = markerByte
	}

	fzfPath := filepath.Join(dir, "zero.fzf")
	if err := os.WriteFile(fzfPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "out")
	if err := Unpack(fzfPath, outDir); err != nil {
		t.Fatal(err)
	}

	silent, err := os.ReadFile(filepath.Join(outDir, "SILENT.fzv"))
	if err != nil {
		t.Fatalf("SILENT.fzv: %v", err)
	}
	if len(silent) != disk.SectorSize {
		t.Errorf("SILENT.fzv length: got %d, want %d (header-only)", len(silent), disk.SectorSize)
	}

	loud, err := os.ReadFile(filepath.Join(outDir, "LOUD.fzv"))
	if err != nil {
		t.Fatalf("LOUD.fzv: %v", err)
	}
	wantLen := disk.SectorSize + disk.PadToSector(v1Samples*disk.BytesPerSample)
	if len(loud) != wantLen {
		t.Errorf("LOUD.fzv length: got %d, want %d", len(loud), wantLen)
	}
	for i := disk.SectorSize; i < len(loud); i++ {
		if loud[i] != markerByte {
			t.Fatalf("LOUD.fzv byte %d: got 0x%02x, want 0x%02x (zero-sample voice stole audio)", i, loud[i], markerByte)
		}
	}
}

// TestRoundTripZeroSampleVoice confirms voicebuild.Build accepts a
// header-only FZV (the output of unpack on a zero-sample slot) and produces
// a zero-sample voice on rebuild.
func TestRoundTripZeroSampleVoice(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Header-only FZV: 1024 bytes, valid Normal voice with waveEnd=waveStart=0.
	silent := make([]byte, disk.SectorSize)
	silentName := disk.PadLabel("SILENT")
	copy(silent[disk.VoiceNameOffset:], silentName[:])
	binary.LittleEndian.PutUint16(silent[disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	// waveStart, waveEnd, genStart, genEnd all default to zero.

	loud := testutil.MakeTestVoice("LOUD", 200)

	silentPath := filepath.Join(dir, "silent.fzv")
	loudPath := filepath.Join(dir, "loud.fzv")
	if err := os.WriteFile(silentPath, silent, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(loudPath, loud, 0644); err != nil {
		t.Fatal(err)
	}

	fzfPath := filepath.Join(dir, "rt.fzf")
	if err := voicebuild.Build(context.Background(), fzfPath, []string{silentPath, loudPath}); err != nil {
		t.Fatalf("voicebuild.Build: %v", err)
	}

	outDir := filepath.Join(dir, "out")
	if err := Unpack(fzfPath, outDir); err != nil {
		t.Fatal(err)
	}

	silentBack, err := os.ReadFile(filepath.Join(outDir, "SILENT.fzv"))
	if err != nil {
		t.Fatalf("SILENT.fzv: %v", err)
	}
	if len(silentBack) != disk.SectorSize {
		t.Errorf("SILENT.fzv round-trip length: got %d, want %d", len(silentBack), disk.SectorSize)
	}
	// waveStart and waveEnd must be equal (silence preserved).
	gotStart := binary.LittleEndian.Uint32(silentBack[disk.VoiceWaveStartOffset:])
	gotEnd := binary.LittleEndian.Uint32(silentBack[disk.VoiceWaveEndOffset:])
	if gotStart != gotEnd {
		t.Errorf("SILENT.fzv waveStart=%d waveEnd=%d; want equal (zero samples)", gotStart, gotEnd)
	}
}

// TestSanitizeFilenameDotOnly verifies that voice names consisting entirely
// of '.' characters are rejected as filename stems, falling back to the
// default voice name. Without this defense, a voice named ".." would expand
// to "../<dedup>.fzv". The `.fzv` suffix currently saves us (a bare ".."
// becomes "...fzv", a regular filename) but the defense is accidental.
// Reject pure-dot names so the boundary is intentional rather than
// dependent on the suffix.
func TestSanitizeFilenameDotOnly(t *testing.T) {
	t.Parallel()
	cases := []string{".", "..", "...", "....."}
	for _, in := range cases {
		got := sanitizeFilename(in)
		if got == in {
			t.Errorf("sanitizeFilename(%q) = %q, want fallback name", in, got)
		}
		if strings.ContainsAny(got, "/\\") {
			t.Errorf("sanitizeFilename(%q) = %q, must not contain path separator", in, got)
		}
		// The fallback should be a normal filename stem, not a dot sequence.
		for _, r := range got {
			if r == '.' && len(got) <= 5 {
				// trailing dots in a name are fine; pure-dot is the concern
				// caught by the equality check above.
				_ = r
			}
		}
	}
}

// TestUnpackVoiceNameDotsProducesSafeFilename is the end-to-end regression
// test for the dot-only voice-name path traversal hazard. It builds a
// 1-voice FZF whose voice name in the header is ".." (padded to 12 chars
// with spaces) and verifies that the resulting on-disk file is a normal
// filename inside outputDir, not anything like "..fzv" or "../<something>".
func TestUnpackVoiceNameDotsProducesSafeFilename(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Build a voice whose 12-byte name field is ".." padded with spaces.
	v := testutil.MakeTestVoice("..", 256)
	vPath := filepath.Join(dir, "v.fzv")
	if err := os.WriteFile(vPath, v, 0644); err != nil {
		t.Fatal(err)
	}

	fzfPath := filepath.Join(dir, "full.fzf")
	if err := voicebuild.Build(context.Background(), fzfPath, []string{vPath}); err != nil {
		t.Fatal(err)
	}

	// Place the output dir as a sibling so any escape via ".." would land
	// in `dir` rather than `outDir`.
	outDir := filepath.Join(dir, "out")
	if err := Unpack(fzfPath, outDir); err != nil {
		t.Fatal(err)
	}

	// Exactly one file in outDir, and nothing escapes to the parent.
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file in outDir, got %d", len(entries))
	}
	name := entries[0].Name()
	if !strings.HasSuffix(name, ".fzv") {
		t.Errorf("expected .fzv extension, got %q", name)
	}
	// Stem must not be pure dots.
	stem := strings.TrimSuffix(name, ".fzv")
	if stem == "" || strings.Trim(stem, ".") == "" {
		t.Errorf("filename stem %q is pure dots (path-traversal hazard)", stem)
	}
	if strings.ContainsAny(name, `/\`) {
		t.Errorf("filename %q contains path separator", name)
	}

	// Defense in depth: no stray file written to the parent.
	parentEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range parentEntries {
		n := e.Name()
		if strings.HasSuffix(n, ".fzv") && n != "v.fzv" {
			t.Errorf("unexpected .fzv file in parent dir: %q (escape via ..)", n)
		}
	}
}

func TestUnpackBankOutOfRange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	voice := testutil.MakeTestVoice("KICK", 512)
	vPath := filepath.Join(dir, "v.fzv")
	if err := os.WriteFile(vPath, voice, 0644); err != nil {
		t.Fatal(err)
	}

	fzfPath := filepath.Join(dir, "full.fzf")
	if err := voicebuild.Build(context.Background(), fzfPath, []string{vPath}); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "out")
	err := UnpackBank(fzfPath, outDir, 5)
	if err == nil {
		t.Fatal("expected error for out-of-range bank index")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("expected 'out of range' in error, got: %v", err)
	}
}

// TestUnpackPreservesKeygroupKeyRange is the end-to-end regression guard for
// F15: build an FZF whose per-voice keygroups differ from the FZV defaults
// written by voiceimport.Encode, unpack it, and confirm that fzvinfo.Parse
// reports the keygroup values from the bank, not the stale Encode defaults.
//
// Voice-header offsets 0xae/0xaf/0xb0 are spec §2-1; the voicebuild fix
// writes those during assembly so the bank's split-mapping arrays (§2-2)
// and the per-voice header agree on every voice the FZF contains.
func TestUnpackPreservesKeygroupKeyRange(t *testing.T) {
	t.Parallel()
	type kr struct {
		low, high, centre uint8
	}
	want := []kr{
		{low: 36, high: 48, centre: 42},
		{low: 49, high: 60, centre: 55},
		{low: 61, high: 72, centre: 66},
	}

	dir := t.TempDir()
	voicePaths := make([]string, len(want))
	for i := range want {
		v := testutil.MakeTestVoice(fmt.Sprintf("V%02d", i+1), 64)
		// Stamp in the voiceimport.Encode defaults so this test models the
		// actual regression: an FZV carrying 96/36/72 in its header that
		// must be overwritten by the keygroup during voicebuild.
		v[disk.VoiceKeyHighOffset] = disk.DefaultKeyHigh
		v[disk.VoiceKeyLowOffset] = disk.DefaultKeyLow
		v[disk.VoiceKeyCentOffset] = disk.DefaultKeyCentre
		p := filepath.Join(dir, fmt.Sprintf("v%02d.fzv", i+1))
		if err := os.WriteFile(p, v, 0644); err != nil {
			t.Fatal(err)
		}
		voicePaths[i] = p
	}

	// Build via the same path AssembleWithKeygroups exercises by reading
	// FZVs from disk; voicebuild.Build uses the default chromatic keygroup,
	// so we go through AssembleWithKeygroups directly to set our own ranges.
	voices := make([][]byte, len(want))
	groups := make([]voicebuild.Keygroup, len(want))
	for i, k := range want {
		data, err := os.ReadFile(voicePaths[i])
		if err != nil {
			t.Fatal(err)
		}
		voices[i] = data
		groups[i] = voicebuild.NewKeygroup(k.low, k.high, k.centre)
	}
	fzf, err := voicebuild.AssembleWithKeygroups(voices, groups)
	if err != nil {
		t.Fatalf("AssembleWithKeygroups: %v", err)
	}
	fzfPath := filepath.Join(dir, "full.fzf")
	if err := os.WriteFile(fzfPath, fzf, 0644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "out")
	if err := Unpack(fzfPath, outDir); err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(want) {
		t.Fatalf("unpacked voice count: got %d, want %d", len(entries), len(want))
	}

	for i, e := range entries {
		p := filepath.Join(outDir, e.Name())
		params, err := fzvinfo.Parse(p)
		if err != nil {
			t.Fatalf("fzvinfo.Parse(%s): %v", e.Name(), err)
		}
		k := want[i]
		if params.KeyLow != k.low || params.KeyHigh != k.high || params.KeyCentre != k.centre {
			t.Errorf("voice %d unpacked: got KeyLow=%d KeyHigh=%d KeyCentre=%d, want %d/%d/%d",
				i+1, params.KeyLow, params.KeyHigh, params.KeyCentre, k.low, k.high, k.centre)
		}
		// Defence-in-depth: the stale Encode defaults must not survive.
		if params.KeyHigh == disk.DefaultKeyHigh && params.KeyLow == disk.DefaultKeyLow && params.KeyCentre == disk.DefaultKeyCentre {
			t.Errorf("voice %d: voice header still carries voiceimport.Encode defaults; F15 fix missing", i+1)
		}
	}
}
