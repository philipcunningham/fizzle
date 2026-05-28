package sfzconvert

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
	"github.com/philipcunningham/fizzle/pkg/sfz"
	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

const junglismSFZ = "../../testdata/synthetic/JUNGLISM.sfz"
const expectedJunglismVoices = 26
const testWAVPad = "pad.wav"

func TestConvertUnsupportedRate(t *testing.T) {
	t.Parallel()
	err := Convert(context.Background(), "x.sfz", "x.fzf", 48000, false)
	if err == nil {
		t.Error("expected error for unsupported rate")
	}
}

// TestConvertContextCancelled verifies Convert aborts when ctx is already
// cancelled, before any WAV decoding occurs.
func TestConvertContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir := t.TempDir()
	err := Convert(ctx, junglismSFZ, filepath.Join(dir, "out.fzf"), 36000, false)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestConvertMultiDiskUnsupportedRate(t *testing.T) {
	t.Parallel()
	err := ConvertMultiDisk(context.Background(), "x.sfz", "x", 48000)
	if err == nil {
		t.Error("expected error for unsupported rate in ConvertMultiDisk")
	}
}

func TestResampleSameRate(t *testing.T) {
	t.Parallel()
	f := &wav.File{SampleRate: 36000, Samples: []int16{100, 200, -100, -200}}
	out, err := fzutil.Resample(f, 36000)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(f.Samples) {
		t.Errorf("same-rate resample: len %d, want %d", len(out), len(f.Samples))
	}
	for i, want := range f.Samples {
		if out[i] != want {
			t.Errorf("sample %d: got %d, want %d", i, out[i], want)
		}
	}
}

func TestResampleHalvesLength(t *testing.T) {
	t.Parallel()
	// Resampling from 36kHz to 18kHz should produce roughly half the samples.
	n := 3600
	samples := make([]int16, n)
	for i := range samples {
		samples[i] = int16(i % 1000)
	}
	f := &wav.File{SampleRate: 36000, Samples: samples}
	out, err := fzutil.Resample(f, 18000)
	if err != nil {
		t.Fatal(err)
	}
	want := 1800
	if out == nil || len(out) != want {
		t.Errorf("resample 36k->18k: len %d, want %d", len(out), want)
	}
}

func TestResampleEmptyErrors(t *testing.T) {
	t.Parallel()
	f := &wav.File{SampleRate: 36000, Samples: nil}
	_, err := fzutil.Resample(f, 9000)
	if err == nil {
		t.Error("expected error for empty WAV")
	}
}

func TestConvertMissingSFZ(t *testing.T) {
	t.Parallel()
	err := Convert(context.Background(), "doesnotexist.sfz", "out.fzf", 36000, false)
	if err == nil {
		t.Error("expected error for missing SFZ")
	}
}

func TestConvertJungle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "jungle.fzf")

	if err := Convert(context.Background(), junglismSFZ, out, 36000, false); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Error("output file is empty")
	}
	if info.Size()%int64(disk.SectorSize) != 0 {
		t.Errorf("output size %d is not a multiple of sector size %d", info.Size(), disk.SectorSize)
	}
}

func TestConvertJungleVoiceCount(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "jungle.fzf")

	if err := Convert(context.Background(), junglismSFZ, out, 36000, false); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// The first 2 bytes of the FZF bank sector hold the voice count.
	voiceCount := binary.LittleEndian.Uint16(data[0:2])
	if voiceCount != expectedJunglismVoices {
		t.Errorf("voice count: got %d, want %d", voiceCount, expectedJunglismVoices)
	}
}

func TestConvertJunglismKeygroups(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "junglism.fzf")

	if err := Convert(context.Background(), junglismSFZ, out, 36000, false); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	// Region 0 (amen 01) should have lokey=hikey=0.
	if data[disk.BankKeyHighOffset] != 0 || data[disk.BankKeyLowOffset] != 0 {
		t.Errorf("region 0 keys: high=%d low=%d, want both 0", data[disk.BankKeyHighOffset], data[disk.BankKeyLowOffset])
	}

	// Region 17 (808) should span 24-35.
	if data[disk.BankKeyHighOffset+17] != 35 || data[disk.BankKeyLowOffset+17] != 24 {
		t.Errorf("808 keys: high=%d low=%d, want 35 24", data[disk.BankKeyHighOffset+17], data[disk.BankKeyLowOffset+17])
	}

	// Region 18 (reese) should span 36-59.
	if data[disk.BankKeyHighOffset+18] != 59 || data[disk.BankKeyLowOffset+18] != 36 {
		t.Errorf("reese keys: high=%d low=%d, want 59 36", data[disk.BankKeyHighOffset+18], data[disk.BankKeyLowOffset+18])
	}
}

func TestConvertRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "jungle.fzf")
	unpackDir := filepath.Join(dir, "voices")

	if err := Convert(context.Background(), junglismSFZ, fzfPath, 36000, false); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != expectedJunglismVoices {
		t.Errorf("unpacked voice count: got %d, want %d", len(entries), expectedJunglismVoices)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".fzv") {
			t.Errorf("unexpected file extension: %q", e.Name())
		}
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() < int64(disk.SectorSize) {
			t.Errorf("%s: too small (%d bytes)", e.Name(), info.Size())
		}
	}
}

// Not parallel: CaptureLog redirects the global logger.
func TestConvertNoSpuriousWarnings(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "junglism.fzf")

	buf := testutil.CaptureLog(t)

	if err := Convert(context.Background(), junglismSFZ, out, 36000, false); err != nil {
		t.Fatal(err)
	}
	// JUNGLISM has all pitch_keycenter set, so no default warnings are expected.
	if testutil.BufHasWarnContaining(buf, "pitch_keycenter") {
		t.Error("unexpected pitch_keycenter default warning: all JUNGLISM regions have pitch_keycenter set")
	}
}

// gchn and mutegroup tests.

// TestConvertMuteGroupToGCHN verifies that SFZ mutegroup opcodes are correctly
// mapped to FZ-1 gchn values. Regions sharing the same mutegroup get a single
// generator bit (monophonic). Regions without mutegroup get 0xff (polyphonic).
func TestConvertMuteGroupToGCHN(t *testing.T) {
	t.Parallel()
	sfzContent := `
<region>
sample=drum.wav lokey=36 hikey=36 pitch_keycenter=36
mutegroup=0

<region>
sample=drum2.wav lokey=37 hikey=37 pitch_keycenter=37
mutegroup=0

<region>
sample=bass.wav lokey=48 hikey=60 pitch_keycenter=48
mutegroup=1

<region>
sample=pad.wav lokey=72 hikey=84 pitch_keycenter=72
`
	dir := t.TempDir()

	for _, name := range []string{"drum.wav", "drum2.wav", "bass.wav", testWAVPad} {
		wavPath := filepath.Join(dir, name)
		f, err := os.Create(wavPath)
		if err != nil {
			t.Fatal(err)
		}
		samples := make([]int16, 100)
		if err := wav.Write(f, &wav.File{SampleRate: 36000, Samples: samples}); err != nil {
			f.Close() //nolint:errcheck
			t.Fatal(err)
		}
		f.Close() //nolint:errcheck
	}

	sfzPath := filepath.Join(dir, "test.sfz")
	sfzBody := sfzContent
	for _, name := range []string{"drum.wav", "drum2.wav", "bass.wav", testWAVPad} {
		sfzBody = strings.ReplaceAll(sfzBody,
			"sample="+name,
			"sample="+filepath.Join(dir, name))
	}
	if err := os.WriteFile(sfzPath, []byte(sfzBody), 0644); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(dir, "out.fzf")
	if err := Convert(context.Background(), sfzPath, outPath, 9000, false); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	bank := data[:1024]

	gchn0 := bank[disk.BankAudioOutOffset+0]
	gchn1 := bank[disk.BankAudioOutOffset+1]
	gchn2 := bank[disk.BankAudioOutOffset+2]
	gchn3 := bank[disk.BankAudioOutOffset+3]

	// Voices 0+1 share mutegroup=0 and so share a generator bit; both monophonic.
	if gchn0 != gchn1 {
		t.Errorf("voices 0+1 (mutegroup=0) should share gchn: got 0x%02x vs 0x%02x", gchn0, gchn1)
	}
	if gchn0 == 0xff {
		t.Errorf("voice 0 with mutegroup=0 should be mono (not 0xff), got 0x%02x", gchn0)
	}
	// Voice 2 has mutegroup=1 so gets a different generator bit from mutegroup=0.
	if gchn2 == 0xff {
		t.Errorf("voice 2 with mutegroup=1 should be mono (not 0xff), got 0x%02x", gchn2)
	}
	if gchn0 == gchn2 {
		t.Errorf("voices with different mutegroups should have different gchn: both 0x%02x", gchn0)
	}
	// Voice 3 has no mutegroup and is polyphonic.
	if gchn3 != 0xff {
		t.Errorf("voice 3 with no mutegroup should be polyphonic (0xff), got 0x%02x", gchn3)
	}
}

// Size warning tests.

// Not parallel: CaptureLog redirects the global logger.
func TestConvertWarnsSizeExceedsDisk(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "jungle.fzf")

	buf := testutil.CaptureLog(t)

	// JUNGLE.sfz at 36kHz is ~3.5 MB, well over the 1.25 MB disk limit.
	if err := Convert(context.Background(), junglismSFZ, out, 36000, false); err != nil {
		t.Fatal(err)
	}
	if !testutil.BufHasWarnContaining(buf, "exceeds floppy disk capacity") {
		t.Error("expected disk capacity warning for large SFZ at 36kHz")
	}
}

// estimateFZFSize tests.

func makeSyntheticWAVMap(sampleCounts []int) ([]sfz.Region, map[string]*wav.File) {
	regions := make([]sfz.Region, len(sampleCounts))
	wavFiles := make(map[string]*wav.File, len(sampleCounts))
	for i, n := range sampleCounts {
		path := fmt.Sprintf("/fake/voice%d.wav", i)
		r := sfz.NewRegion()
		r.Sample = path
		note := uint8(36 + i)
		r.LoKey = note
		r.HiKey = note
		r.PitchKeycenter = note
		regions[i] = r
		samples := make([]int16, n)
		wavFiles[path] = &wav.File{SampleRate: 36000, Samples: samples}
	}
	return regions, wavFiles
}

func TestEstimateFZFSizeScalesWithRate(t *testing.T) {
	t.Parallel()
	regions, wavFiles := makeSyntheticWAVMap([]int{36000, 36000}) // 2 voices, 1s each at 36kHz

	est36 := estimateFZFSize(regions, wavFiles, 36000)
	est18 := estimateFZFSize(regions, wavFiles, 18000)
	est9 := estimateFZFSize(regions, wavFiles, 9000)

	if est18 >= est36 {
		t.Errorf("18kHz estimate (%d) should be smaller than 36kHz (%d)", est18, est36)
	}
	if est9 >= est18 {
		t.Errorf("9kHz estimate (%d) should be smaller than 18kHz (%d)", est9, est18)
	}
	// Halving the rate should roughly halve the audio size.
	if est18 > est36/2+disk.SectorSize*4 {
		t.Errorf("18kHz estimate (%d) unexpectedly large vs 36kHz (%d)", est18, est36)
	}
}

// selectRate and fit-to-disk tests.

func TestSelectRateNoFitToDisk(t *testing.T) {
	t.Parallel()
	regions, wavFiles := makeSyntheticWAVMap([]int{1000})
	rate, err := selectRate(regions, wavFiles, 18000, false)
	if err != nil {
		t.Fatal(err)
	}
	if rate != 18000 {
		t.Errorf("without fit-to-disk, rate should be unchanged: got %d, want 18000", rate)
	}
}

func TestSelectRateAlreadyFits(t *testing.T) {
	// A tiny instrument that should not step down.
	regions, wavFiles := makeSyntheticWAVMap([]int{1000, 500})

	buf := testutil.CaptureLog(t)

	rate, err := selectRate(regions, wavFiles, 36000, true)
	if err != nil {
		t.Fatal(err)
	}
	if rate != 36000 {
		t.Errorf("small instrument should keep 36000 Hz, got %d", rate)
	}
	if testutil.BufHasWarnContaining(buf, "downsampling") {
		t.Error("should not warn about downsampling when instrument already fits")
	}
}

func TestSelectRateStepsDown(t *testing.T) {
	// Create enough samples to exceed disk capacity at 36kHz but fit at 9kHz.
	// disk.UsableDataSize ≈ 1,308,672 bytes = ~654,336 samples at 36kHz.
	// Use 700,000 samples (just over limit at 36k, fine at 9k).
	regions, wavFiles := makeSyntheticWAVMap([]int{700000})

	buf := testutil.CaptureLog(t)

	rate, err := selectRate(regions, wavFiles, 36000, true)
	if err != nil {
		t.Fatal(err)
	}
	if rate == 36000 {
		t.Errorf("expected rate to step down from 36000, got 36000")
	}
	if !testutil.BufHasWarnContaining(buf, "downsampling") {
		t.Error("expected downsampling warning when rate is stepped down")
	}
}

func TestSelectRateRespectsCeiling(t *testing.T) {
	t.Parallel()
	// With a ceiling of 18000, even a small instrument should never use 36000.
	regions, wavFiles := makeSyntheticWAVMap([]int{1000})

	rate, err := selectRate(regions, wavFiles, 18000, true)
	if err != nil {
		t.Fatal(err)
	}
	if rate > 18000 {
		t.Errorf("rate should not exceed ceiling of 18000, got %d", rate)
	}
}

func TestSelectRateTooLargeEvenAt9kHz(t *testing.T) {
	t.Parallel()
	// 64 voices × 700,000 samples would be enormous even at 9kHz.
	counts := make([]int, 64)
	for i := range counts {
		counts[i] = 700000
	}
	regions, wavFiles := makeSyntheticWAVMap(counts)

	_, err := selectRate(regions, wavFiles, 36000, true)
	if err == nil {
		t.Error("expected error when instrument cannot fit at any rate")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error should mention 'too large': %v", err)
	}
}

// TestSelectRateBoundaries covers edge cases around the disk capacity limit:
// the ceiling clamps even when smaller rates would also fit, oversize at the
// max rate triggers downsampling, and oversize at the lowest rate errors.
func TestSelectRateBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		sampleCount int // samples in single voice at 36kHz source rate
		ceilingRate uint32
		fitToDisk   bool
		wantRate    uint32 // 0 means expect an error
		wantErr     bool
	}{
		{
			name:        "ceiling 9kHz forces 9kHz even when 36kHz would fit",
			sampleCount: 1000,
			ceilingRate: 9000,
			fitToDisk:   true,
			wantRate:    9000,
		},
		{
			name:        "ceiling 18kHz never goes above 18kHz",
			sampleCount: 1000,
			ceilingRate: 18000,
			fitToDisk:   true,
			wantRate:    18000,
		},
		{
			name:        "audio fits at 18kHz but ceiling is 9kHz: returns 9kHz",
			sampleCount: 500000, // bigger than disk at 36k, fits at 18k and 9k
			ceilingRate: 9000,
			fitToDisk:   true,
			wantRate:    9000,
		},
		{
			name:        "fit-to-disk false bypasses estimation",
			sampleCount: 10_000_000, // huge, but no fit-to-disk
			ceilingRate: 36000,
			fitToDisk:   false,
			wantRate:    36000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			regions, wavFiles := makeSyntheticWAVMap([]int{tt.sampleCount})
			got, err := selectRate(regions, wavFiles, tt.ceilingRate, tt.fitToDisk)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got rate %d", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantRate {
				t.Errorf("rate: got %d, want %d", got, tt.wantRate)
			}
		})
	}
}

// End-to-end fit-to-disk test with real SFZ.

// Not parallel: CaptureLog redirects the global logger.
func TestFitToDiskConvertsJunglism(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "junglism-fit.fzf")

	buf := testutil.CaptureLog(t)

	// JUNGLISM.sfz at 36kHz exceeds disk capacity; --fit-to-disk should step down.
	if err := Convert(context.Background(), junglismSFZ, out, 36000, true); err != nil {
		t.Fatalf("Convert with fit-to-disk: %v", err)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > int64(disk.UsableDataSize) {
		t.Errorf("output size %d exceeds disk capacity %d", info.Size(), disk.UsableDataSize)
	}
	if !testutil.BufHasWarnContaining(buf, "downsampling") {
		t.Error("expected downsampling warning")
	}
	// Should not also warn about exceeding capacity.
	if testutil.BufHasWarnContaining(buf, "exceeds floppy disk capacity") {
		t.Error("should not warn about capacity when fit-to-disk succeeded")
	}
}

func TestFitToDiskWithRateCeiling(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "junglism-18k.fzf")

	// With ceiling 18000, output should be at most 18kHz.
	if err := Convert(context.Background(), junglismSFZ, out, 18000, true); err != nil {
		t.Fatalf("Convert with fit-to-disk at 18kHz: %v", err)
	}
	// Verify by inspecting the rate byte of the first unpacked voice.
	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(out, unpackDir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(unpackDir)
	if err != nil || len(entries) == 0 {
		t.Fatal("no voices unpacked")
	}
	fzv, err := os.ReadFile(filepath.Join(unpackDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	rateIdx := fzv[disk.VoiceSampOffset]
	// rateIdx 1 = 18kHz, rateIdx 2 = 9kHz; both are ≤ 18kHz ceiling.
	if rateIdx == 0 {
		t.Errorf("rate index 0 (36kHz) found, expected ≤ 18kHz (index ≥ 1)")
	}
}

// ConvertDir tests.

func TestConvertDirHappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.WriteTestWAV(t, filepath.Join(dir, "01-kick.wav"), 36000, 1000)
	testutil.WriteTestWAV(t, filepath.Join(dir, "02-snare.wav"), 36000, 1000)
	testutil.WriteTestWAV(t, filepath.Join(dir, "03-hat.wav"), 36000, 1000)
	out := filepath.Join(t.TempDir(), "out.fzf")
	if err := ConvertDir(context.Background(), dir, out, 36000, false); err != nil {
		t.Fatalf("ConvertDir: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if len(data)%1024 != 0 {
		t.Errorf("output size %d not sector-aligned", len(data))
	}
	hdr, err := fzutil.ParseFZFHeader(data)
	if err != nil {
		t.Fatalf("parsing FZF header: %v", err)
	}
	if hdr.NVoice != 3 {
		t.Errorf("voice count: got %d, want 3", hdr.NVoice)
	}
}

// TestConvertDirLeavesFilterOpen guards against a regression where the
// directory workflow's synthetic regions left Cutoff and Resonance at the
// Go zero value (0), causing regionToFZVFromFile to slam the filter shut
// on every voice.
func TestConvertDirLeavesFilterOpen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.WriteTestWAV(t, filepath.Join(dir, "kick.wav"), 36000, 1000)
	out := filepath.Join(t.TempDir(), "out.fzf")
	if err := ConvertDir(context.Background(), dir, out, 36000, false); err != nil {
		t.Fatalf("ConvertDir: %v", err)
	}

	unpackDir := t.TempDir()
	if err := voiceunpack.Unpack(out, unpackDir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(unpackDir)
	if err != nil || len(entries) == 0 {
		t.Fatal("no voices unpacked")
	}
	fzv, err := os.ReadFile(filepath.Join(unpackDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if fzv[disk.VoiceDCFOffset] != disk.DCFMaxOffset {
		t.Errorf("DCF cutoff: got %d, want %d (filter must stay fully open without an SFZ cutoff opcode)", fzv[disk.VoiceDCFOffset], disk.DCFMaxOffset)
	}
}

func TestConvertDirEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(t.TempDir(), "out.fzf")
	err := ConvertDir(context.Background(), dir, out, 36000, false)
	if err == nil || !strings.Contains(err.Error(), "no WAV") {
		t.Fatalf("expected 'no WAV' error, got %v", err)
	}
}

func TestConvertDirNoWAVs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a wav"), 0644); err != nil {
		t.Fatalf("writing readme: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.fzf")
	err := ConvertDir(context.Background(), dir, out, 36000, false)
	if err == nil || !strings.Contains(err.Error(), "no WAV") {
		t.Fatalf("expected 'no WAV' error, got %v", err)
	}
}

func TestConvertDirUnsupportedRate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.WriteTestWAV(t, filepath.Join(dir, "kick.wav"), 44100, 500)
	out := filepath.Join(t.TempDir(), "out.fzf")
	err := ConvertDir(context.Background(), dir, out, 48000, false)
	if err == nil || !strings.Contains(err.Error(), "unsupported rate") {
		t.Fatalf("expected unsupported rate error, got %v", err)
	}
}

func TestConvertDirFitToDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.WriteTestWAV(t, filepath.Join(dir, "kick.wav"), 36000, 500)
	testutil.WriteTestWAV(t, filepath.Join(dir, "snare.wav"), 36000, 500)
	out := filepath.Join(t.TempDir(), "out.fzf")
	if err := ConvertDir(context.Background(), dir, out, 36000, true); err != nil {
		t.Fatalf("ConvertDir with fit-to-disk: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Size() > int64(disk.UsableDataSize) {
		t.Errorf("output %d exceeds UsableDataSize %d", info.Size(), disk.UsableDataSize)
	}
}

func TestConvertDirBadWAV(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.wav"), []byte("not a wav file"), 0644); err != nil {
		t.Fatalf("writing bad WAV: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.fzf")
	err := ConvertDir(context.Background(), dir, out, 36000, false)
	if err == nil {
		t.Fatal("expected error for bad WAV")
	}
}

// ConvertMultiDisk tests.

func TestConvertMultiDiskHappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sfzDir := t.TempDir()

	for i := range 5 {
		testutil.WriteTestWAV(t, filepath.Join(dir, fmt.Sprintf("voice%02d.wav", i)), 36000, 150000)
	}

	var sfzContent strings.Builder
	for i := range 5 {
		note := 36 + i
		fmt.Fprintf(&sfzContent, "<region>\nsample=%s\nlokey=%d\nhikey=%d\npitch_keycenter=%d\n\n",
			filepath.Join(dir, fmt.Sprintf("voice%02d.wav", i)), note, note, note)
	}
	sfzPath := filepath.Join(sfzDir, "test.sfz")
	if err := os.WriteFile(sfzPath, []byte(sfzContent.String()), 0644); err != nil {
		t.Fatalf("writing SFZ: %v", err)
	}

	outPrefix := filepath.Join(t.TempDir(), "multi")
	err := ConvertMultiDisk(context.Background(), sfzPath, outPrefix, 36000)
	if err != nil {
		t.Fatalf("ConvertMultiDisk: %v", err)
	}

	for _, suffix := range []string{"-1.img", "-2.img"} {
		path := outPrefix + suffix
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected output file %s: %v", path, err)
		}
		if info.Size() != disk.ImageSize {
			t.Errorf("%s: size %d, want %d", suffix, info.Size(), disk.ImageSize)
		}
	}

	img1, err := disk.OpenImage(outPrefix + "-1.img")
	if err != nil {
		t.Fatalf("opening disk 1 image: %v", err)
	}
	entries1, err := img1.Directory()
	if err != nil {
		t.Fatalf("reading disk 1 directory: %v", err)
	}
	if len(entries1) == 0 {
		t.Fatal("disk 1 directory is empty")
	}

	fzfPath := filepath.Join(t.TempDir(), "extracted.fzf")
	if err := diskget.Get(outPrefix+"-1.img", entries1[0].NameString(), fzfPath); err != nil {
		t.Fatalf("extracting FZF from disk 1: %v", err)
	}
	fzfData, err := os.ReadFile(fzfPath)
	if err != nil {
		t.Fatalf("reading extracted FZF: %v", err)
	}
	hdr, err := fzutil.ParseFZFHeader(fzfData)
	if err != nil {
		t.Fatalf("parsing FZF header: %v", err)
	}
	if hdr.NVoice != 5 {
		t.Errorf("voice count: got %d, want 5", hdr.NVoice)
	}

	img2, err := disk.OpenImage(outPrefix + "-2.img")
	if err != nil {
		t.Fatalf("opening disk 2 image: %v", err)
	}
	if img2.FreeSectors() >= disk.SectorCount-2 {
		t.Error("disk 2 has no data sectors allocated")
	}
}

// buildMuteGroupMap tests.

// Not parallel: CaptureLog redirects the global logger.
func TestBuildMuteGroupMapOverflow(t *testing.T) {
	buf := testutil.CaptureLog(t)
	regions := make([]sfz.Region, 10)
	for i := range regions {
		regions[i] = sfz.Region{HasMuteGroup: true, MuteGroup: i}
	}
	m := buildMuteGroupMap(regions)
	if len(m) != 10 {
		t.Errorf("map size: got %d, want 10", len(m))
	}
	if m[8] != 8 || m[9] != 8 {
		t.Errorf("overflow groups should map to generator 8, got m[8]=%d m[9]=%d", m[8], m[9])
	}
	if !testutil.BufHasWarnContaining(buf, "more than 8 mute groups") {
		t.Error("expected overflow warning in log")
	}
}

// loadWAVFiles tests.

func TestLoadWAVFilesDeduplication(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "shared.wav")
	testutil.WriteTestWAV(t, wavPath, 36000, 100)
	regions := []sfz.Region{
		{Sample: wavPath},
		{Sample: wavPath},
	}
	m, err := loadWAVFiles(context.Background(), regions)
	if err != nil {
		t.Fatalf("loadWAVFiles: %v", err)
	}
	if len(m) != 1 {
		t.Errorf("expected 1 unique WAV, got %d", len(m))
	}
}

func TestConvertDirSkipsSubdirectories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.WriteTestWAV(t, filepath.Join(dir, "kick.wav"), 36000, 100)
	testutil.WriteTestWAV(t, filepath.Join(dir, "snare.wav"), 36000, 100)

	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	testutil.WriteTestWAV(t, filepath.Join(sub, "hidden.wav"), 36000, 100)

	out := filepath.Join(t.TempDir(), "out.fzf")
	if err := ConvertDir(context.Background(), dir, out, 36000, false); err != nil {
		t.Fatalf("ConvertDir: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	hdr, err := fzutil.ParseFZFHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.NVoice != 2 {
		t.Errorf("got %d voices, want 2 (subdirectory WAV should be excluded)", hdr.NVoice)
	}
}

func TestConvertDirSuggestsSubdirWAVs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	sub := filepath.Join(dir, "samples")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	testutil.WriteTestWAV(t, filepath.Join(sub, "kick.wav"), 36000, 100)

	out := filepath.Join(t.TempDir(), "out.fzf")
	err := ConvertDir(context.Background(), dir, out, 36000, false)
	if err == nil {
		t.Fatal("expected error for empty top-level directory")
	}
	if !strings.Contains(err.Error(), "subdirectories") {
		t.Errorf("error should mention subdirectories, got: %v", err)
	}
}

func TestLoadWAVFilesBadPath(t *testing.T) {
	t.Parallel()
	regions := []sfz.Region{
		{Sample: "/nonexistent/path/bad.wav"},
	}
	_, err := loadWAVFiles(context.Background(), regions)
	if err == nil {
		t.Fatal("expected error for bad WAV path")
	}
	if !strings.Contains(err.Error(), "region 1") {
		t.Errorf("error should mention region number, got: %v", err)
	}
}

func writeWAVWithLoop(t *testing.T, path string, rate uint32, nSamples, loopStart, loopEnd int) {
	t.Helper()
	samples := make([]int16, nSamples)
	for i := range samples {
		samples[i] = int16(i % 1000)
	}
	f := &wav.File{SampleRate: rate, Samples: samples, LoopStart: loopStart, LoopEnd: loopEnd}
	var buf bytes.Buffer
	if err := wav.Write(&buf, f); err != nil {
		t.Fatalf("writing test WAV: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("saving test WAV: %v", err)
	}
}

func TestOneShotSuppressesWAVLoopPoints(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "pad.wav")
	writeWAVWithLoop(t, wavPath, 36000, 5000, 100, 4900)

	rf, err := os.Open(wavPath)
	if err != nil {
		t.Fatal(err)
	}
	w, err := wav.Read(rf)
	rf.Close() //nolint:errcheck
	if err != nil {
		t.Fatal(err)
	}
	if w.LoopStart != 100 || w.LoopEnd != 4900 {
		t.Fatalf("WAV loop points not written: LoopStart=%d LoopEnd=%d", w.LoopStart, w.LoopEnd)
	}

	sfzPath := filepath.Join(dir, "test.sfz")
	sfzContent := `<region>
sample=pad.wav lokey=60 hikey=71 pitch_keycenter=66
loop_mode=one_shot
`
	if err := os.WriteFile(sfzPath, []byte(sfzContent), 0644); err != nil {
		t.Fatal(err)
	}

	fzfPath := filepath.Join(dir, "out.fzf")
	if err := Convert(context.Background(), sfzPath, fzfPath, 36000, false); err != nil {
		t.Fatal(err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatal(err)
	}

	fzvData, err := os.ReadFile(filepath.Join(unpackDir, "PAD.fzv"))
	if err != nil {
		t.Fatal(err)
	}
	loopSus := fzvData[disk.VoiceLoopSusOffset]
	if loopSus != disk.NoSustainLoop {
		t.Errorf("loop_sus=%d, want %d (no sustain loop for one_shot)", loopSus, disk.NoSustainLoop)
	}
	loopEnd := fzvData[disk.VoiceLoopEndOffset]
	if loopEnd != disk.NoReleaseLoop {
		t.Errorf("loop_end=%d, want %d (no release loop for one_shot)", loopEnd, disk.NoReleaseLoop)
	}
}

func TestWAVLoopPreservedWithoutOneShot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "pad.wav")
	writeWAVWithLoop(t, wavPath, 36000, 5000, 100, 4900)

	rf, err := os.Open(wavPath)
	if err != nil {
		t.Fatal(err)
	}
	w, err := wav.Read(rf)
	rf.Close() //nolint:errcheck
	if err != nil {
		t.Fatal(err)
	}
	if w.LoopStart != 100 || w.LoopEnd != 4900 {
		t.Fatalf("WAV loop points not written: LoopStart=%d LoopEnd=%d", w.LoopStart, w.LoopEnd)
	}

	sfzPath := filepath.Join(dir, "test.sfz")
	sfzContent := `<region>
sample=pad.wav lokey=60 hikey=71 pitch_keycenter=66
`
	if err := os.WriteFile(sfzPath, []byte(sfzContent), 0644); err != nil {
		t.Fatal(err)
	}

	fzfPath := filepath.Join(dir, "out.fzf")
	if err := Convert(context.Background(), sfzPath, fzfPath, 36000, false); err != nil {
		t.Fatal(err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatal(err)
	}

	fzvData, err := os.ReadFile(filepath.Join(unpackDir, "PAD.fzv"))
	if err != nil {
		t.Fatal(err)
	}
	loopSus := fzvData[disk.VoiceLoopSusOffset]
	if loopSus == disk.NoSustainLoop {
		t.Error("expected sustain loop to be active when loop_mode is not one_shot")
	}
}

func TestTuneOpcodeSetsDCPFineTune(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "tone.wav")
	testutil.WriteTestWAV(t, wavPath, 36000, 1000)

	sfzPath := filepath.Join(dir, "test.sfz")
	sfzContent := fmt.Sprintf(`<region>
sample=%s lokey=60 hikey=60 pitch_keycenter=60
tune=50
`, wavPath)
	if err := os.WriteFile(sfzPath, []byte(sfzContent), 0644); err != nil {
		t.Fatal(err)
	}

	fzfPath := filepath.Join(dir, "out.fzf")
	if err := Convert(context.Background(), sfzPath, fzfPath, 36000, false); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil || len(entries) == 0 {
		t.Fatal("no voices unpacked")
	}
	fzvData, err := os.ReadFile(filepath.Join(unpackDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	dcp := int16(binary.LittleEndian.Uint16(fzvData[disk.VoiceDCPOffset:])) //nolint:gosec // G115: intentional reinterpretation
	wantDCP := int16(math.Round(50.0 * 256.0 / 100.0))
	if dcp != wantDCP {
		t.Errorf("DCP for tune=50: got %d, want %d", dcp, wantDCP)
	}
}

// TestTuneTransposeMaxNoWrap covers the int16 overflow bug where
// transpose=127 + tune=100 used to wrap: currentDCP (127 * 256 = 32512) +
// tuneDCP (256) = 32768, which becomes -32768 in int16 and silently
// flipped the pitch direction. The fix sums in int32 and clamps at
// MaxInt16, so the DCP must stay positive (the request is saturated, not
// inverted).
func TestTuneTransposeMaxNoWrap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "tone.wav")
	testutil.WriteTestWAV(t, wavPath, 36000, 1000)

	sfzPath := filepath.Join(dir, "test.sfz")
	sfzContent := fmt.Sprintf(`<region>
sample=%s lokey=60 hikey=60 pitch_keycenter=60
transpose=127 tune=100
`, wavPath)
	if err := os.WriteFile(sfzPath, []byte(sfzContent), 0644); err != nil {
		t.Fatal(err)
	}

	fzfPath := filepath.Join(dir, "out.fzf")
	if err := Convert(context.Background(), sfzPath, fzfPath, 36000, false); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil || len(entries) == 0 {
		t.Fatal("no voices unpacked")
	}
	fzvData, err := os.ReadFile(filepath.Join(unpackDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	dcp := int16(binary.LittleEndian.Uint16(fzvData[disk.VoiceDCPOffset:])) //nolint:gosec // G115: intentional reinterpretation
	if dcp < 0 {
		t.Errorf("DCP wrapped to negative (%d): transpose=127+tune=100 must clamp at MaxInt16, not flip to MinInt16", dcp)
	}
	if dcp != math.MaxInt16 {
		t.Errorf("DCP = %d, want %d (saturated at int16 max)", dcp, math.MaxInt16)
	}
}

func TestCutoffResonanceOpcodeApplied(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "tone.wav")
	testutil.WriteTestWAV(t, wavPath, 36000, 1000)

	sfzPath := filepath.Join(dir, "test.sfz")
	sfzContent := fmt.Sprintf(`<region>
sample=%s lokey=60 hikey=60 pitch_keycenter=60
cutoff=80 resonance=50
`, wavPath)
	if err := os.WriteFile(sfzPath, []byte(sfzContent), 0644); err != nil {
		t.Fatal(err)
	}

	fzfPath := filepath.Join(dir, "out.fzf")
	if err := Convert(context.Background(), sfzPath, fzfPath, 36000, false); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil || len(entries) == 0 {
		t.Fatal("no voices unpacked")
	}
	fzvData, err := os.ReadFile(filepath.Join(unpackDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	if fzvData[disk.VoiceDCFOffset] != 80 {
		t.Errorf("DCF cutoff: got %d, want 80", fzvData[disk.VoiceDCFOffset])
	}
	if fzvData[disk.VoiceDCQOffset] != 50 {
		t.Errorf("DCQ resonance: got %d, want 50", fzvData[disk.VoiceDCQOffset])
	}
}

func TestCutoffResonanceDefaultUnchanged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "tone.wav")
	testutil.WriteTestWAV(t, wavPath, 36000, 1000)

	sfzPath := filepath.Join(dir, "test.sfz")
	sfzContent := fmt.Sprintf(`<region>
sample=%s lokey=60 hikey=60 pitch_keycenter=60
`, wavPath)
	if err := os.WriteFile(sfzPath, []byte(sfzContent), 0644); err != nil {
		t.Fatal(err)
	}

	fzfPath := filepath.Join(dir, "out.fzf")
	if err := Convert(context.Background(), sfzPath, fzfPath, 36000, false); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil || len(entries) == 0 {
		t.Fatal("no voices unpacked")
	}
	fzvData, err := os.ReadFile(filepath.Join(unpackDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	if fzvData[disk.VoiceDCFOffset] != disk.DCFMaxOffset {
		t.Errorf("DCF cutoff default: got %d, want %d (fully open)", fzvData[disk.VoiceDCFOffset], disk.DCFMaxOffset)
	}
}

func TestLoopStartEndOpcodeOverridesWAV(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "pad.wav")
	writeWAVWithLoop(t, wavPath, 36000, 5000, 100, 4900)

	sfzPath := filepath.Join(dir, "test.sfz")
	sfzContent := fmt.Sprintf(`<region>
sample=%s lokey=60 hikey=71 pitch_keycenter=66
loop_start=200 loop_end=4000
`, wavPath)
	if err := os.WriteFile(sfzPath, []byte(sfzContent), 0644); err != nil {
		t.Fatal(err)
	}

	fzfPath := filepath.Join(dir, "out.fzf")
	if err := Convert(context.Background(), sfzPath, fzfPath, 36000, false); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil || len(entries) == 0 {
		t.Fatal("no voices unpacked")
	}
	fzvData, err := os.ReadFile(filepath.Join(unpackDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	loopSus := fzvData[disk.VoiceLoopSusOffset]
	if loopSus == disk.NoSustainLoop {
		t.Error("expected sustain loop to be active when loop_start/loop_end are set")
	}
}

func TestConvertDirMaxVoicesKeyAssignment(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for i := range disk.MaxVoices {
		testutil.WriteTestWAV(t, filepath.Join(dir, fmt.Sprintf("%02d.wav", i)), 36000, 100)
	}
	out := filepath.Join(t.TempDir(), "out.fzf")
	if err := ConvertDir(context.Background(), dir, out, 36000, false); err != nil {
		t.Fatalf("ConvertDir: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	hdr, err := fzutil.ParseFZFHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.NVoice != disk.MaxVoices {
		t.Fatalf("voice count: got %d, want %d", hdr.NVoice, disk.MaxVoices)
	}
	lastIdx := disk.MaxVoices - 1
	lastKeyHigh := data[disk.BankKeyHighOffset+lastIdx]
	wantKey := uint8(disk.FirstMIDINote + lastIdx)
	if wantKey > disk.MaxMIDINote {
		t.Fatalf("test assumption failed: last key %d exceeds MIDI range", wantKey)
	}
	if lastKeyHigh != wantKey {
		t.Errorf("last voice key high: got %d, want %d", lastKeyHigh, wantKey)
	}
}

// TestPitchKeycenterPatchesVoiceHeaderCent guards finding F11: the SFZ region's
// pitch_keycenter must reach the FZV voice header's cent byte (spec §2-1,
// offset 0xB0). Previously voiceimport.Encode left it at DefaultKeyCentre (72)
// so fzv extract / sfz export rebuilt the wrong root key on round-trip.
func TestPitchKeycenterPatchesVoiceHeaderCent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "tone.wav")
	testutil.WriteTestWAV(t, wavPath, 36000, 1000)

	// Use pitch_keycenter=48 (which differs from DefaultKeyCentre=72) on a
	// region whose key range does not contain 48, so a half-fix that only
	// updates the bank's per-key cent[i] (which buildKeygroup already does)
	// would not also write the voice header at offset 0xB0.
	sfzPath := filepath.Join(dir, "test.sfz")
	sfzContent := fmt.Sprintf(`<region>
sample=%s lokey=60 hikey=72 pitch_keycenter=48
`, wavPath)
	if err := os.WriteFile(sfzPath, []byte(sfzContent), 0644); err != nil {
		t.Fatal(err)
	}

	fzfPath := filepath.Join(dir, "out.fzf")
	if err := Convert(context.Background(), sfzPath, fzfPath, 36000, false); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil || len(entries) == 0 {
		t.Fatal("no voices unpacked")
	}
	fzvData, err := os.ReadFile(filepath.Join(unpackDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	if got := fzvData[disk.VoiceKeyCentOffset]; got != 48 {
		t.Errorf("FZV voice header cent (offset 0x%X): got %d, want 48", disk.VoiceKeyCentOffset, got)
	}
}
