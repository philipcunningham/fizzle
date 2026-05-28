package fzfinfo

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
)

// buildSplitIMGs builds a 2-disk split using AssembleMultiDisk and writes
// both disk images to a temp dir, returning their paths.
func buildSplitIMGs(t *testing.T) (disk1Path, disk2Path string) {
	t.Helper()
	dir := t.TempDir()

	maxDisk1 := disk.UsableDataSize - disk.SectorSize
	maxDisk1 = (maxDisk1 / disk.SectorSize) * disk.SectorSize
	overheadSectors := 1 + disk.VoiceAreaSectors(2)
	disk1AudioCapacity := maxDisk1 - overheadSectors*disk.SectorSize
	alphaSamples := disk1AudioCapacity / disk.BytesPerSample
	betaSamples := disk.SectorSize / disk.BytesPerSample

	voices := make([][]byte, 2)
	groups := make([]voicebuild.Keygroup, 2)
	names := []string{"ALPHA", "BETA"}
	samples := []int{alphaSamples, betaSamples}
	for i := range voices {
		v := make([]byte, disk.SectorSize+samples[i]*disk.BytesPerSample)
		name := disk.PadLabel(names[i])
		copy(v[disk.VoiceNameOffset:], name[:])
		binary.LittleEndian.PutUint32(v[disk.VoiceWaveStartOffset:], 0)
		binary.LittleEndian.PutUint32(v[disk.VoiceWaveEndOffset:], uint32(samples[i])) //nolint:gosec // G115: test value fits uint32
		binary.LittleEndian.PutUint32(v[disk.VoiceGenStartOffset:], 0)
		binary.LittleEndian.PutUint32(v[disk.VoiceGenEndOffset:], uint32(samples[i])) //nolint:gosec // G115: test value fits uint32
		binary.LittleEndian.PutUint16(v[disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
		v[disk.VoiceSampOffset] = 0
		voices[i] = v
		groups[i] = voicebuild.Keygroup{
			KeyLow: uint8(disk.FirstMIDINote + i), KeyHigh: uint8(disk.FirstMIDINote + i),
			VelLow: disk.DefaultVelLow, VelHigh: disk.DefaultVelHigh,
			KeyCentre: uint8(disk.FirstMIDINote + i), AudioOut: disk.PolyphonicAudioOut,
		}
	}

	result, err := voicebuild.AssembleMultiDisk(voices, groups)
	if err != nil {
		t.Fatalf("AssembleMultiDisk: %v", err)
	}

	binary.LittleEndian.PutUint32(result.Disks[0][disk.BankTotalWaveOffset:], uint32(result.WaveCount)) //nolint:gosec // G115: wave count fits uint32

	name := disk.PadLabel(disk.FullDumpName)
	for i, d := range result.Disks {
		imgPath := filepath.Join(dir, fmt.Sprintf("split-%d.img", i+1))
		label := fmt.Sprintf("SPLIT %d", i+1)
		if err := diskformat.Format(imgPath, label); err != nil {
			t.Fatalf("diskformat.Format disk %d: %v", i+1, err)
		}
		if err := diskadd.AddBytes(imgPath, d, name, disk.TypeFullDump, uint8(i), result.BankCount, result.VoiceCount, result.WaveCount); err != nil {
			t.Fatalf("diskadd.AddBytes disk %d: %v", i+1, err)
		}
	}

	disk1Path = filepath.Join(dir, "split-1.img")
	disk2Path = filepath.Join(dir, "split-2.img")
	return disk1Path, disk2Path
}

// extractFZF extracts the full dump from a disk image into a temporary FZF
// file and returns its path.
func extractFZF(t *testing.T, imgPath string) string {
	t.Helper()
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "extracted.fzf")
	if err := diskget.Get(imgPath, disk.FullDumpName, fzfPath); err != nil {
		t.Fatalf("diskget.Get(%s): %v", filepath.Base(imgPath), err)
	}
	return fzfPath
}

func buildFZF(t *testing.T, n int) string {
	t.Helper()
	names := make([]string, n)
	for i := range names {
		names[i] = strings.ToUpper(string(rune('A'+i%26))) + "VOICE"
	}
	_, path := fzfbuilder.MakeTestFZF(t, names)
	return path
}

func TestInfoShowsVoiceCount(t *testing.T) {
	t.Parallel()
	fzfPath := buildFZF(t, 3)
	var buf bytes.Buffer
	if err := Info(fzfPath, &buf, nil); err != nil {
		t.Fatalf("Info: %v", err)
	}
	if !strings.Contains(buf.String(), "Voices:    3") {
		t.Errorf("expected 'Voices:    3':\n%s", buf.String())
	}
}

func TestInfoShowsFilenameNotFullPath(t *testing.T) {
	t.Parallel()
	fzfPath := buildFZF(t, 1)
	var buf bytes.Buffer
	if err := Info(fzfPath, &buf, nil); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(fzfPath)
	if strings.Contains(buf.String(), dir) {
		t.Errorf("full directory path leaked into output:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "test.fzf") {
		t.Errorf("expected filename 'test.fzf' in output:\n%s", buf.String())
	}
}

func TestInfoSingleKeyNoRangeNotation(t *testing.T) {
	t.Parallel()
	fzfPath := buildFZF(t, 1)
	var buf bytes.Buffer
	if err := Info(fzfPath, &buf, nil); err != nil {
		t.Fatal(err)
	}
	// Single-key voice should not show "X to X".
	lines := strings.Split(buf.String(), "\n")
	for _, line := range lines {
		// Skip the header line.
		if strings.Contains(line, "Keys") {
			continue
		}
		if strings.Contains(line, " to ") {
			// Check if both sides of "to" are the same note (that would be redundant).
			parts := strings.SplitN(line, " to ", 2)
			if len(parts) == 2 {
				lhs := strings.Fields(parts[0])
				rhs := strings.Fields(parts[1])
				if len(lhs) > 0 && len(rhs) > 0 {
					lhsKey := lhs[len(lhs)-1]
					rhsKey := rhs[0]
					if lhsKey == rhsKey {
						t.Errorf("single-key voice shows redundant range '%s to %s'", lhsKey, rhsKey)
					}
				}
			}
		}
	}
}

func TestInfoVelocityColumnHiddenByDefault(t *testing.T) {
	t.Parallel()
	// When all voices have standard velocity (1-127), Velocity column is omitted.
	fzfPath := buildFZF(t, 2)
	var buf bytes.Buffer
	if err := Info(fzfPath, &buf, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Velocity") {
		t.Errorf("Velocity column should be hidden for standard velocity ranges:\n%s", buf.String())
	}
}

func TestInfoLoopMarkedInOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	v := make([]byte, disk.SectorSize+1024)
	name := disk.PadLabel("LOOPVOX")
	copy(v[disk.VoiceNameOffset:], name[:])
	binary.LittleEndian.PutUint32(v[disk.VoiceWaveStartOffset:], 0)
	binary.LittleEndian.PutUint32(v[disk.VoiceWaveEndOffset:], 512)
	binary.LittleEndian.PutUint32(v[disk.VoiceGenStartOffset:], 0)
	binary.LittleEndian.PutUint32(v[disk.VoiceGenEndOffset:], 512)
	binary.LittleEndian.PutUint16(v[disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	v[disk.VoiceLoopSusOffset] = 0                                  // loop_sus = 0 (active loop)
	binary.LittleEndian.PutUint32(v[disk.VoiceLoopSt0Offset:], 50)  // loopst[0]
	binary.LittleEndian.PutUint32(v[disk.VoiceLoopEd0Offset:], 400) // looped[0]

	out, err := voicebuild.AssembleWithKeygroups([][]byte{v}, []voicebuild.Keygroup{
		{KeyLow: 60, KeyHigh: 60, VelLow: disk.DefaultVelLow, VelHigh: disk.DefaultVelHigh, KeyCentre: 60, AudioOut: disk.PolyphonicAudioOut},
	})
	if err != nil {
		t.Fatal(err)
	}
	fzfPath := filepath.Join(dir, "loop.fzf")
	if err := os.WriteFile(fzfPath, out, 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := Info(fzfPath, &buf, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "[loop]") {
		t.Errorf("expected [loop] marker for voice with active loop:\n%s", buf.String())
	}
}

func TestInfoMissingFile(t *testing.T) {
	t.Parallel()
	err := Info("/nonexistent/path.fzf", &bytes.Buffer{}, nil)
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestInfoRealHardwareTECHNO(t *testing.T) {
	t.Parallel()
	const technoImg = "../../testdata/synthetic/TECHNO.img"
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "techno.fzf")
	if err := diskget.Get(technoImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	var buf bytes.Buffer
	if err := Info(fzfPath, &buf, nil); err != nil {
		t.Fatalf("Info: %v", err)
	}
	out := buf.String()
	// TECHNO is a multi-bank dump: 8 bank sectors with 32 distinct voice
	// slots in the voice area. Prior to the multi-bank fix only bank 0's
	// bstep=11 voices were surfaced; the remaining slots (used by banks
	// 1-7 via vp[]) were dropped on the floor.
	if !strings.Contains(out, "Voices:    32") {
		t.Errorf("expected 32 voices (multi-bank coverage):\n%s", out)
	}
	if !strings.Contains(out, "METAL-BELL") {
		t.Errorf("expected METAL-BELL:\n%s", out)
	}
	t.Logf("fzf info output:\n%s", out)
}

// TestInfoTechnoBank1OnlyVoicesShowCorrectMetadata is the regression test
// for F3: voices that are referenced only from banks 1-7 (not bank 0)
// must show the bank metadata of *their* owning bank, not the misaligned
// bytes that fall at bank-0[offset+voiceSlot] when voiceSlot is past
// bank 0's bstep.
//
// Voice slot 11 in TECHNO (NASTY BASS) is referenced by bank 0 vp[0]=11,
// so it should show bank 0 split 0's key range (42-66). Prior to the fix
// fzfinfo read bank[disk.BankKeyLowOffset+11], which is well past bank 0's
// 11 splits and lands on the velocity-low array's slot 11 byte (=0). The
// snapshot used to show key_low=0, key_high=0 for this voice.
func TestInfoTechnoBank1OnlyVoicesShowCorrectMetadata(t *testing.T) {
	t.Parallel()
	const technoImg = "../../testdata/synthetic/TECHNO.img"
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "techno.fzf")
	if err := diskget.Get(technoImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	info, err := Parse(fzfPath)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Find voice slot 11 (index 12 in 1-based UI numbering): NASTY BASS.
	var nastyBass *VoiceEntry
	for i := range info.Voices {
		if info.Voices[i].Name == "NASTY BASS" {
			nastyBass = &info.Voices[i]
			break
		}
	}
	if nastyBass == nil {
		t.Fatal("NASTY BASS voice not found in TECHNO.img info")
	}
	// Bank 0 vp[0] = 11 (NASTY BASS), with KeyLow=42 KeyHigh=66.
	if nastyBass.KeyLow != 42 || nastyBass.KeyHigh != 66 {
		t.Errorf("NASTY BASS key range: got [%d, %d], want [42, 66] (from bank 0 split 0)",
			nastyBass.KeyLow, nastyBass.KeyHigh)
	}
	const wantOutput = "all"
	if nastyBass.Output != wantOutput {
		t.Errorf("NASTY BASS output: got %q, want %q (bank 0 split 0 gchn=0xff)",
			nastyBass.Output, wantOutput)
	}
}

func TestInfoChanColumnAlwaysShown(t *testing.T) {
	t.Parallel()
	// Chan column is always shown, even when all voices are on channel 1.
	fzfPath := buildFZF(t, 2)
	var buf bytes.Buffer
	if err := Info(fzfPath, &buf, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Chan") {
		t.Errorf("Chan column should always be shown:\n%s", buf.String())
	}
}

func TestInfoChanColumnShowsCorrectValue(t *testing.T) {
	t.Parallel()
	fzfPath := buildFZF(t, 2)
	// Patch bank sector: set voice 1 (index 0) to channel 2 (stored as 1).
	data, err := os.ReadFile(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	data[disk.BankMIDIRecvChanOffset+0] = 1                   // channel 2 (0-indexed)
	if err := os.WriteFile(fzfPath, data, 0644); err != nil { //nolint:gosec
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := Info(fzfPath, &buf, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Chan") {
		t.Errorf("Chan column should appear when voices have different channels:\n%s", buf.String())
	}
	// Should show "2" for the first voice and "1" for the second.
	if !strings.Contains(buf.String(), "2") {
		t.Errorf("expected channel 2 in output:\n%s", buf.String())
	}
}

func TestInfoOutColumnAlwaysShown(t *testing.T) {
	t.Parallel()
	fzfPath := buildFZF(t, 2)
	var buf bytes.Buffer
	if err := Info(fzfPath, &buf, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Out") {
		t.Errorf("Out column should always be shown:\n%s", out)
	}
	if !strings.Contains(out, "all") {
		t.Errorf("default output should show 'all':\n%s", out)
	}
}

func TestInfoOutColumnValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		gchn uint8
		want string
	}{
		{"single", 0x04, "3"},
		{"multi", 0x05, "1,3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fzfPath := buildFZF(t, 2)
			data, err := os.ReadFile(fzfPath)
			if err != nil {
				t.Fatal(err)
			}
			data[disk.BankAudioOutOffset+0] = tt.gchn
			if err := os.WriteFile(fzfPath, data, 0644); err != nil { //nolint:gosec
				t.Fatal(err)
			}
			info, err := Parse(fzfPath)
			if err != nil {
				t.Fatal(err)
			}
			if info.Voices[0].Output != tt.want {
				t.Errorf("voice 0 output: got %q, want %q", info.Voices[0].Output, tt.want)
			}
			if info.Voices[1].Output != "all" {
				t.Errorf("voice 1 output: got %q, want %q (unchanged voice)", info.Voices[1].Output, "all")
			}
		})
	}
}

func TestInfoOutColumnJSON(t *testing.T) {
	t.Parallel()
	fzfPath := buildFZF(t, 2)
	var buf bytes.Buffer
	if err := RenderJSON(&buf, mustParse(t, fzfPath)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"output"`) {
		t.Errorf("JSON should contain output field:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), `"all"`) {
		t.Errorf("JSON should contain 'all' output:\n%s", buf.String())
	}
}

func mustParse(t *testing.T, path string) *FullDump {
	t.Helper()
	info, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestInfoSplitDisk1HeaderShown(t *testing.T) {
	t.Parallel()
	disk1Img, _ := buildSplitIMGs(t)
	fzfPath := extractFZF(t, disk1Img)
	var buf bytes.Buffer
	if err := Info(fzfPath, &buf, nil); err != nil {
		t.Fatalf("Info: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Disk:      1 of 2") {
		t.Errorf("expected 'Disk:      1 of 2' in disk 1 output:\n%s", out)
	}
	// Disk must appear before Memory, Memory before Voices.
	diskIdx := strings.Index(out, "Disk:")
	memIdx := strings.Index(out, "Memory:")
	voicesIdx := strings.Index(out, "Voices:")
	if diskIdx >= memIdx || memIdx >= voicesIdx {
		t.Errorf("expected order: Disk, Memory, Voices:\n%s", out)
	}
}

func TestInfoSplitShowsAllVoices(t *testing.T) {
	t.Parallel()
	disk1Img, _ := buildSplitIMGs(t)
	fzfPath := extractFZF(t, disk1Img)
	var buf bytes.Buffer
	if err := Info(fzfPath, &buf, nil); err != nil {
		t.Fatalf("Info: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "ALPHA") {
		t.Errorf("voice ALPHA must appear in disk 1 table:\n%s", out)
	}
	if !strings.Contains(out, "BETA") {
		t.Errorf("continuation voice BETA must appear in disk 1 table (all voice headers are on disk 1):\n%s", out)
	}
}

func TestInfoSplitMemoryIsLocal(t *testing.T) {
	t.Parallel()
	disk1Img, _ := buildSplitIMGs(t)
	fzfPath := extractFZF(t, disk1Img)
	var buf bytes.Buffer
	if err := Info(fzfPath, &buf, nil); err != nil {
		t.Fatalf("Info: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Memory:") {
		t.Errorf("expected Memory line in output:\n%s", out)
	}
	if strings.Contains(out, "on disk 1") || strings.Contains(out, "on disk 2") {
		t.Errorf("Memory line should show local size only, not per-disk breakdown:\n%s", out)
	}
}

func TestInfoSplitDisk2Unaffected(t *testing.T) {
	t.Parallel()
	_, disk2Img := buildSplitIMGs(t)
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "disk2.fzf")
	if err := diskget.Get(disk2Img, disk.FullDumpName, fzfPath); err != nil {
		t.Fatalf("diskget.Get: %v", err)
	}
	_, err := Parse(fzfPath)
	if err == nil {
		t.Fatal("expected Parse to fail on disk 2 (pure audio continuation)")
	}
}

func TestInfoNonSplitNoAnnotation(t *testing.T) {
	t.Parallel()
	fzfPath := buildFZF(t, 3)
	var buf bytes.Buffer
	if err := Info(fzfPath, &buf, nil); err != nil {
		t.Fatalf("Info: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "1 of 2") {
		t.Errorf("single-disk FZF should not show split lines:\n%s", out)
	}
}

func TestInfoHighlightedRowMarked(t *testing.T) {
	t.Parallel()
	fzfPath := buildFZF(t, 3)
	highlighted := map[int]bool{2: true} // mark voice 2

	var buf bytes.Buffer
	if err := Info(fzfPath, &buf, highlighted); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// Voice 2's row should contain "*2".
	if !strings.Contains(out, "*2") {
		t.Errorf("highlighted voice 2 should have *2 prefix:\n%s", out)
	}
	// Voice 1 and 3 should not be marked.
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		if strings.Contains(line, "*1") || strings.Contains(line, "*3") {
			t.Errorf("non-highlighted voices should not have * prefix:\n%s", line)
		}
	}
}

func TestParseWrongFileType(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	fzv := make([]byte, disk.SectorSize*2)
	copy(fzv[disk.VoiceNameOffset:], "MYKICK      ")
	fzvPath := filepath.Join(dir, "voice.fzv")
	if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Parse(fzvPath)
	if err == nil {
		t.Fatal("expected error when parsing a voice file as FZF")
	}
}

func TestParseVoiceFileSuggestsFzvInfo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	fzv := make([]byte, disk.SectorSize*2)
	copy(fzv[disk.VoiceNameOffset:], "MYKICK      ")
	fzvPath := filepath.Join(dir, "voice.fzv")
	if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Parse(fzvPath)
	if err == nil {
		t.Fatal("expected error when parsing a voice file as FZF")
	}
	if !strings.Contains(err.Error(), "fzv info") {
		t.Errorf("error should suggest 'fzv info', got: %v", err)
	}
}

func TestParseDoesNotMisclassifyTextAsVoice(t *testing.T) {
	t.Parallel()
	data := make([]byte, disk.SectorSize*2)
	copy(data[disk.VoiceNameOffset:], "from the lat")
	data[disk.VoiceSampOffset] = 'e'

	p := filepath.Join(t.TempDir(), "text.dat")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Parse(p)
	if err == nil {
		t.Fatal("expected error for text-like data")
	}
	if strings.Contains(err.Error(), "looks like a voice file") {
		t.Errorf("text file should not be misclassified as voice: %v", err)
	}
}

func TestParseVoiceCount(t *testing.T) {
	t.Parallel()
	fzfPath := buildFZF(t, 3)
	info, err := Parse(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Voices) != 3 {
		t.Errorf("got %d voices, want 3", len(info.Voices))
	}
	if info.VoiceCount != 3 {
		t.Errorf("VoiceCount = %d, want 3", info.VoiceCount)
	}
}

func TestParseFilename(t *testing.T) {
	t.Parallel()
	fzfPath := buildFZF(t, 1)
	info, err := Parse(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Filename != "test.fzf" {
		t.Errorf("Filename = %q, want test.fzf", info.Filename)
	}
}

func TestParseSplitDisk(t *testing.T) {
	t.Parallel()
	disk1Img, _ := buildSplitIMGs(t)
	fzfPath := extractFZF(t, disk1Img)
	info, err := Parse(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsSplit {
		t.Error("expected IsSplit=true for disk 1")
	}
	if info.DiskNumber != 1 {
		t.Errorf("DiskNumber = %d, want 1", info.DiskNumber)
	}
}

func TestParseNonSplit(t *testing.T) {
	t.Parallel()
	fzfPath := buildFZF(t, 3)
	info, err := Parse(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.IsSplit {
		t.Error("expected IsSplit=false for single-disk FZF")
	}
}

// TestParseGarbageTotalWaveMarkerNotSplit guards against a regression where a
// garbage value at BankTotalWaveOffset would cause a single-disk FZF to be
// misreported as is_split=true. Real-world dumps (e.g. CASIO084.FZF,
// CASIO097.FZF from the FZ-1 Shareware Library) carry uninitialised bytes at
// offset 0x290, and only non-plausible (all-zero) voice slots happen to
// satisfy the inner boundary test. Detection must require corroborating
// evidence: a plausible voice whose wavst points past local audio.
func TestParseGarbageTotalWaveMarkerNotSplit(t *testing.T) {
	t.Parallel()
	fzfPath := buildFZF(t, 2)
	data, err := os.ReadFile(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	// Stamp a garbage totalWaveMarker that vastly exceeds the local audio
	// area, mimicking the field's contents in CASIO084.FZF and CASIO097.FZF.
	binary.LittleEndian.PutUint32(data[disk.BankTotalWaveOffset:], 0xCAFEBABE)
	// Build a second FZF where, on top of the garbage marker, one voice slot
	// is zeroed out so its (garbage) wavst would have satisfied the old
	// boundary test had the slot not been rejected for implausibility.
	voiceAreaStart := disk.SectorSize
	voiceSlot := voiceAreaStart + disk.VoiceSlotOffset(0, 1)
	for i := range disk.VoiceHeaderUsed {
		data[voiceSlot+i] = 0
	}
	// Place a clearly out-of-bounds wavst on the zeroed slot.
	binary.LittleEndian.PutUint32(data[voiceSlot+disk.VoiceWaveStartOffset:], 0x7FFFFFFF)
	patched := filepath.Join(t.TempDir(), "garbage-marker.fzf")
	if err := os.WriteFile(patched, data, 0644); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	info, err := Parse(patched)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.IsSplit {
		t.Errorf("garbage totalWaveMarker with no plausible boundary voice must report IsSplit=false, got IsSplit=true (disk %d of %d, local_voices=%d)",
			info.DiskNumber, info.TotalDisks, info.LocalVoices)
	}
}

func TestParseVoiceFields(t *testing.T) {
	t.Parallel()
	fzfPath := buildFZF(t, 1)
	info, err := Parse(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Voices) != 1 {
		t.Fatalf("expected 1 voice, got %d", len(info.Voices))
	}
	v := info.Voices[0]
	if v.Name == "" {
		t.Error("voice name should not be empty")
	}
	if v.Index != 1 {
		t.Errorf("Index = %d, want 1", v.Index)
	}
}

func TestRenderJSONVoiceCount(t *testing.T) {
	t.Parallel()
	fzfPath := buildFZF(t, 3)
	info, err := Parse(fzfPath)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RenderJSON(&buf, info); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, `"voice_count": 3`) {
		t.Errorf("expected voice_count 3 in JSON:\n%s", out)
	}
	if !strings.Contains(out, `"voices"`) {
		t.Errorf("expected voices key in JSON:\n%s", out)
	}
}

func TestRenderJSONIsValidJSON(t *testing.T) {
	t.Parallel()
	fzfPath := buildFZF(t, 2)
	info, err := Parse(fzfPath)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RenderJSON(&buf, info); err != nil {
		t.Fatal(err)
	}

	var decoded FullDump
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON output is not valid: %v\n%s", err, buf.String())
	}
	if decoded.VoiceCount != 2 {
		t.Errorf("decoded VoiceCount = %d, want 2", decoded.VoiceCount)
	}
	if len(decoded.Voices) != 2 {
		t.Errorf("decoded voices count = %d, want 2", len(decoded.Voices))
	}
	for _, v := range decoded.Voices {
		if v.Name == "" {
			t.Error("decoded voice has empty name")
		}
		if v.MIDIChannel < 1 {
			t.Errorf("decoded voice MIDI channel = %d, want >= 1", v.MIDIChannel)
		}
	}
}

func TestRenderJSONExcludesShowVelocity(t *testing.T) {
	t.Parallel()
	fzfPath := buildFZF(t, 1)
	info, err := Parse(fzfPath)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RenderJSON(&buf, info); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(buf.String(), "show_velocity") {
		t.Error("JSON should not contain show_velocity (it is a display hint)")
	}
}

// TestParseAcceptsNormalVariantWithWarn is a regression test for the
// undocumented playback mode 0x0157 observed in the FZ-1 Factory Library's
// Clarinet.fzf. The file uses 0x0157 for every "first voice of each bank"
// position; the bit pattern differs from NORMAL (0x01D7) by exactly one
// cleared bit (bit 7 of the low byte). We treat it as a NORMAL variant so
// the file parses, and emit a WARN log so the undocumented value remains
// visible to anyone scanning logs.
func TestParseAcceptsNormalVariantWithWarn(t *testing.T) {
	buf := testutil.CaptureLog(t)

	data := make([]byte, disk.SectorSize*2+disk.SectorSize)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], 1)
	copy(data[disk.BankNameOffset:], "Variant Bank ")

	voiceArea := disk.SectorSize
	off := disk.VoiceSlotOffset(voiceArea, 0)
	binary.LittleEndian.PutUint16(data[off+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormalVariant)
	padded := disk.PadLabel("CLRNT1 F3")
	copy(data[off+disk.VoiceNameOffset:], padded[:])

	fzfPath := filepath.Join(t.TempDir(), "variant.fzf")
	if err := os.WriteFile(fzfPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	info, err := Parse(fzfPath)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(info.Voices) != 1 {
		t.Fatalf("expected 1 voice, got %d", len(info.Voices))
	}
	if got := info.Voices[0].PlaybackMode; got != disk.PlaybackModeNameNormalVariant {
		t.Errorf("PlaybackMode = %q, want %q", got, disk.PlaybackModeNameNormalVariant)
	}
	if !testutil.BufHasWarnContaining(buf, "0x0157") {
		t.Errorf("expected WARN naming the undocumented mode 0x0157, got:\n%s", buf.String())
	}
}

// TestParseIncludesNoSoundSlots verifies that the parsed Voices array has
// one entry per voice slot (matching voice_count == vn from the spec), with
// PlaybackModeNoSound placeholder slots represented by entries carrying
// PlaybackMode == "no_sound" rather than being filtered out. This keeps
// slot indices aligned with the bank's vp[] references and lets consumers
// filter to audible voices via PlaybackMode.
func TestParseIncludesNoSoundSlots(t *testing.T) {
	t.Parallel()
	// Construct a synthetic FZF with bstep=5: two leading NoSound slots
	// then three plausible Normal voices, mirroring CASIO139.FZF's shape.
	const noSound = 2
	const active = 3
	const total = noSound + active
	voiceAreaSectors := (total + 3) / 4
	data := make([]byte, disk.SectorSize+voiceAreaSectors*disk.SectorSize+disk.SectorSize)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], uint16(total))
	copy(data[disk.BankNameOffset:], "Mock NoSound ")

	voiceArea := disk.SectorSize
	// NoSound slots: loop mode 0 (default), no other fields set.
	// Active slots: printable name and Normal playback mode.
	for i := noSound; i < total; i++ {
		off := disk.VoiceSlotOffset(voiceArea, i)
		binary.LittleEndian.PutUint16(data[off+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
		name := fmt.Sprintf("V%d", i)
		padded := disk.PadLabel(name)
		copy(data[off+disk.VoiceNameOffset:], padded[:])
	}

	fzfPath := filepath.Join(t.TempDir(), "no-sound.fzf")
	if err := os.WriteFile(fzfPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	info, err := Parse(fzfPath)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.VoiceCount != total {
		t.Errorf("VoiceCount = %d, want %d (spec's vn slot count)", info.VoiceCount, total)
	}
	if len(info.Voices) != total {
		t.Fatalf("len(Voices) = %d, want %d (array length must match voice_count)", len(info.Voices), total)
	}
	for i := range noSound {
		if got := info.Voices[i].PlaybackMode; got != disk.PlaybackModeNameNoSound {
			t.Errorf("Voices[%d].PlaybackMode = %q, want %q", i, got, disk.PlaybackModeNameNoSound)
		}
		if got := info.Voices[i].Index; got != i+1 {
			t.Errorf("Voices[%d].Index = %d, want %d (slot indices must be contiguous)", i, got, i+1)
		}
	}
	for i := noSound; i < total; i++ {
		if got := info.Voices[i].PlaybackMode; got != disk.PlaybackModeNameNormal {
			t.Errorf("Voices[%d].PlaybackMode = %q, want %q", i, got, disk.PlaybackModeNameNormal)
		}
	}
	// LocalVoices excludes NoSound placeholders.
	if info.LocalVoices != active {
		t.Errorf("LocalVoices = %d, want %d (audible voices only)", info.LocalVoices, active)
	}
}

// TestRenderSkipsNoSoundRows verifies the human-readable table omits
// PlaybackModeNoSound rows (they carry no audible payload) while the JSON
// representation continues to include them for slot-index correspondence.
func TestRenderSkipsNoSoundRows(t *testing.T) {
	t.Parallel()
	info := &FullDump{
		Filename:   "synthetic.fzf",
		VoiceCount: 3,
		Voices: []VoiceEntry{
			{VoiceEntry: fzutil.VoiceEntry{Index: 1, PlaybackMode: disk.PlaybackModeNameNoSound}},
			{VoiceEntry: fzutil.VoiceEntry{Index: 2, Name: "ACTIVE", PlaybackMode: disk.PlaybackModeNameNormal, KeyLow: 60, KeyHigh: 60, RootNote: 60, MIDIChannel: 1, Output: "all"}},
			{VoiceEntry: fzutil.VoiceEntry{Index: 3, PlaybackMode: disk.PlaybackModeNameNoSound}},
		},
	}
	var buf bytes.Buffer
	Render(&buf, info, nil)
	out := buf.String()
	if !strings.Contains(out, "ACTIVE") {
		t.Errorf("expected ACTIVE row in output:\n%s", out)
	}
	if strings.Contains(out, "no_sound") {
		t.Errorf("table should not surface raw no_sound label:\n%s", out)
	}
	// Row count check: only the ACTIVE voice should produce a data row.
	rowMarkers := strings.Count(out, "│ 1 ") + strings.Count(out, "│ 3 ")
	if rowMarkers != 0 {
		t.Errorf("NoSound slot indices 1 and 3 should not appear as rows:\n%s", out)
	}
}

// TestMemoryBytesClampedToAudioArea is a regression test for the
// memory_bytes overflow bug. Some real-world FZFs (e.g. Drums.fzf) carry a
// garbage waveEnd in the last voice header that, multiplied by 2 bytes per
// sample, reports ~4 GB of memory for a sub-MB file. MemoryBytes must be
// clamped to the audio actually present in the file.
func TestMemoryBytesClampedToAudioArea(t *testing.T) {
	t.Parallel()

	// Build a minimal FZF: 1 bank sector, 1 voice slot, 1 sector of audio.
	const audioSectors = 1
	data := make([]byte, disk.SectorSize+disk.SectorSize+audioSectors*disk.SectorSize)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], 1)
	copy(data[disk.BankNameOffset:], "Garbage Bank ")

	voiceArea := disk.SectorSize
	slot := disk.VoiceSlotOffset(voiceArea, 0)
	binary.LittleEndian.PutUint16(data[slot+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	// Sane waveStart, but waveEnd that, when multiplied by BytesPerSample,
	// would imply ~8 GB of audio. Pre-fix this leaked into MemoryBytes.
	binary.LittleEndian.PutUint32(data[slot+disk.VoiceWaveStartOffset:], 0)
	binary.LittleEndian.PutUint32(data[slot+disk.VoiceWaveEndOffset:], 0xFFFFFFFF)

	p := filepath.Join(t.TempDir(), "garbage.fzf")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}

	info, err := Parse(p)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	localAudioBytes := audioSectors * disk.SectorSize
	if info.MemoryBytes > localAudioBytes {
		t.Errorf("MemoryBytes = %d exceeds audio area size %d (clamp missing)",
			info.MemoryBytes, localAudioBytes)
	}
	if info.MemoryBytes < 0 {
		t.Errorf("MemoryBytes = %d, should be non-negative", info.MemoryBytes)
	}
}
