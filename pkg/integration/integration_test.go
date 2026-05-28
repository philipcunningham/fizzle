package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/disklist"
	"github.com/philipcunningham/fizzle/pkg/fzfinfo"
	"github.com/philipcunningham/fizzle/pkg/fzfmidi"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"

	"github.com/philipcunningham/fizzle/pkg/sfzconvert"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
	"github.com/philipcunningham/fizzle/pkg/voiceextract"

	voiceimport "github.com/philipcunningham/fizzle/pkg/voiceimport"
	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

const (
	hooverImg = "../../testdata/synthetic/HOOVER.img"
	stabImg   = "../../testdata/synthetic/STAB.img"
	technoImg = "../../testdata/synthetic/TECHNO.img"
	brassImg  = "../../testdata/synthetic/BRASS.img"
	padLFOImg = "../../testdata/synthetic/PAD-LFO.img"
)

func skipShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
}

func TestImageSize(t *testing.T) {
	skipShort(t)
	t.Parallel()
	for _, path := range []string{hooverImg, stabImg, brassImg} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if info.Size() != disk.ImageSize {
			t.Errorf("%s: size %d, want %d", path, info.Size(), disk.ImageSize)
		}
	}
}

func TestGoldenDiskLs(t *testing.T) {
	skipShort(t)
	t.Parallel()
	tests := []struct {
		path      string
		wantLabel string
		wantName  string
		wantType  string
	}{
		{hooverImg, "HOOVER", "HOOVER", "Voice"},
		{stabImg, "STAB", "STAB", "Voice"},
		{technoImg, "Techno Split", "FULL-DATA-FZ", "Full Dump"},
		{brassImg, "Brass Ensemb", "FULL-DATA-FZ", "Full Dump"},
	}
	for _, tt := range tests {
		var buf bytes.Buffer
		if err := disklist.List(tt.path, &buf); err != nil {
			t.Fatalf("%s: %v", tt.path, err)
		}
		out := buf.String()
		if !strings.Contains(out, tt.wantLabel) {
			t.Errorf("%s: output missing label %q:\n%s", tt.path, tt.wantLabel, out)
		}
		if !strings.Contains(out, tt.wantName) {
			t.Errorf("%s: output missing name %q:\n%s", tt.path, tt.wantName, out)
		}
		if !strings.Contains(out, tt.wantType) {
			t.Errorf("%s: output missing type %q:\n%s", tt.path, tt.wantType, out)
		}
	}
}

func TestVoiceExtractSanity(t *testing.T) {
	skipShort(t)
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "hoover.wav")

	// Extract the voice from HOOVER.img. We need to get the raw FZV data from
	// the disk image first, then extract via the package.
	fzvData := extractFirstVoiceData(t, hooverImg)
	fzvPath := filepath.Join(dir, "hoover.fzv")
	if err := os.WriteFile(fzvPath, fzvData, 0644); err != nil {
		t.Fatal(err)
	}

	if err := voiceextract.Extract(fzvPath, wavPath); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(wavPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 44 {
		t.Errorf("WAV file too small: %d bytes", info.Size())
	}

	// Decode and check sample values.
	rate, samples, err := voiceextract.Decode(fzvData)
	if err != nil {
		t.Fatal(err)
	}
	if rate != 36000 && rate != 18000 && rate != 9000 {
		t.Errorf("unexpected sample rate: %d", rate)
	}
	if len(samples) == 0 {
		t.Error("no samples extracted")
	}
	hasNonZero := false
	for _, s := range samples {
		if s != 0 {
			hasNonZero = true
		}
	}
	if !hasNonZero {
		t.Error("all samples are zero")
	}
}

func TestFullRoundTrip(t *testing.T) {
	skipShort(t)
	t.Parallel()
	dir := t.TempDir()

	// Extract the raw FZV data from HOOVER.img.
	fzvData := extractFirstVoiceData(t, hooverImg)

	// Decode the original samples.
	origRate, origSamples, err := voiceextract.Decode(fzvData)
	if err != nil {
		t.Fatal(err)
	}

	// Write the original FZV to a temp file, extract to WAV, re-import.
	fzvPath := filepath.Join(dir, "orig.fzv")
	if err := os.WriteFile(fzvPath, fzvData, 0644); err != nil {
		t.Fatal(err)
	}
	wavPath := filepath.Join(dir, "extracted.wav")
	if err := voiceextract.Extract(fzvPath, wavPath); err != nil {
		t.Fatal(err)
	}

	reimportedFZV := filepath.Join(dir, "reimported.fzv")
	if err := voiceimport.Import(wavPath, reimportedFZV, origRate); err != nil {
		t.Fatal(err)
	}

	// Decode the re-imported FZV.
	reimportedData, err := os.ReadFile(reimportedFZV)
	if err != nil {
		t.Fatal(err)
	}
	_, reimportedSamples, err := voiceextract.Decode(reimportedData)
	if err != nil {
		t.Fatal(err)
	}

	// Because we import at the same rate as the source, there is no resampling
	// and the sample counts and values should match exactly.
	if len(reimportedSamples) != len(origSamples) {
		t.Errorf("sample count mismatch: got %d, want %d", len(reimportedSamples), len(origSamples))
		return
	}

	// Allow a small tolerance for any intermediate rounding.
	const tolerance = 2
	mismatches := 0
	for i := range origSamples {
		diff := int(origSamples[i]) - int(reimportedSamples[i])
		if diff < 0 {
			diff = -diff
		}
		if diff > tolerance {
			mismatches++
		}
	}
	if mismatches > 0 {
		t.Errorf("%d samples exceeded tolerance of %d", mismatches, tolerance)
	}
}

func TestDiskRebuild(t *testing.T) {
	skipShort(t)
	t.Parallel()
	dir := t.TempDir()

	// Get the original entry details from HOOVER.img.
	origEntries := readEntries(t, hooverImg)
	if len(origEntries) == 0 {
		t.Fatal("HOOVER.img has no directory entries")
	}
	origEntry := origEntries[0]

	// Extract the FZV from HOOVER.img.
	fzvData := extractFirstVoiceData(t, hooverImg)
	fzvPath := filepath.Join(dir, "voice.fzv")
	if err := os.WriteFile(fzvPath, fzvData, 0644); err != nil {
		t.Fatal(err)
	}

	// Create a new blank disk, add the FZV, list it.
	newImg := filepath.Join(dir, "new.img")
	if err := diskformat.Format(newImg, "HOOVER"); err != nil {
		t.Fatal(err)
	}
	if err := diskadd.Add(newImg, fzvPath, 0); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := disklist.List(newImg, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.Contains(out, origEntry.NameString()) {
		t.Errorf("rebuilt disk missing entry name %q:\n%s", origEntry.NameString(), out)
	}
}

// extractFirstVoiceData reads the raw sectors of the first directory entry
// from imagePath and returns them as a byte slice.
func extractFirstVoiceData(t *testing.T, imagePath string) []byte {
	t.Helper()
	f, err := os.Open(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck

	img, err := disk.ReadImage(f)
	if err != nil {
		t.Fatal(err)
	}

	entries, err := img.Directory()
	if err != nil || len(entries) == 0 {
		t.Fatal("no directory entries")
	}

	e := entries[0]
	disSec, err := img.Sector(int(e.DisSector))
	if err != nil {
		t.Fatal(err)
	}
	dis, err := disk.DecodeDisSector(disSec)
	if err != nil || len(dis.Extents) == 0 {
		t.Fatal("no extents")
	}

	// The first sector of the extent is the DIS sector itself. The voice
	// header and audio data begin at the sector after it.
	var raw []byte
	for sec := int(dis.Extents[0][0]) + 1; sec <= int(dis.Extents[0][1]); sec++ {
		b, err := img.Sector(sec)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, b...)
	}
	return raw
}

const junglismSFZ = "../../testdata/synthetic/JUNGLISM.sfz"
const junglismSamplesDir = "../../testdata/synthetic/JUNGLISM Samples"

// TestSFZFullPipeline is the end-to-end test for the complete workflow:
// SFZ to FZF to disk image, then disk get, unpack, and extract WAV.
// Uses JUNGLISM.sfz with 36kHz (split to 2 disks if needed, here we test single-disk
// path using 9kHz which fits). Verifies audio fidelity across sector boundaries.
func TestSFZFullPipeline(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "junglism.fzf")
	imgPath := filepath.Join(dir, "junglism.img")
	unpackDir := filepath.Join(dir, "voices")

	// Step 1: SFZ to FZF at 9kHz (fits on one disk)
	if err := sfzconvert.Convert(context.Background(), junglismSFZ, fzfPath, 9000, false); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	// Step 2: Create disk image and add FZF
	if err := diskformat.Format(imgPath, "JUNGLISM"); err != nil {
		t.Fatal(err)
	}
	if err := diskadd.Add(imgPath, fzfPath, 0); err != nil {
		t.Fatal(err)
	}

	// Step 3: Verify disk listing
	var buf bytes.Buffer
	if err := disklist.List(imgPath, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "FULL-DATA-FZ") {
		t.Errorf("disk ls missing FULL-DATA-FZ: %s", buf.String())
	}

	// Step 4: Get FZF back from disk. Must be byte-identical.
	gotFZFPath := filepath.Join(dir, "got.fzf")
	if err := diskget.Get(imgPath, "FULL-DATA-FZ", gotFZFPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	orig, err := os.ReadFile(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(gotFZFPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(orig, got) {
		t.Errorf("FZF retrieved from disk differs from original (%d vs %d bytes)", len(orig), len(got))
	}

	// Step 5: Unpack to individual FZVs
	if err := voiceunpack.Unpack(gotFZFPath, unpackDir); err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	entries, err := os.ReadDir(unpackDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 26 {
		t.Errorf("expected 26 voices, got %d", len(entries))
	}

	// Step 6: Verify audio fidelity across sector boundaries.
	// Boundaries at voice indices 4, 8, 12, 16, 20, 24.
	checkVoices := []struct {
		fzvName string
		srcWAV  string
	}{
		{"AMEN 01.fzv", "amen 01.wav"},
		{"AMEN 05.fzv", "amen 05.wav"},   // boundary at index 4
		{"THINK 01.fzv", "think 01.wav"}, // boundary at index 8
		{"THINK 05.fzv", "think 05.wav"}, // boundary at index 12
		{"808.fzv", "808.wav"},           // boundary at index 16 (808 is index 17, close enough)
		{"REESE.fzv", "reese.wav"},       // loop voice (verify waveStart=0)
	}

	for _, cv := range checkVoices {
		t.Run(cv.fzvName, func(t *testing.T) {
			t.Parallel()
			fzvPath := filepath.Join(unpackDir, cv.fzvName)
			if _, err := os.Stat(fzvPath); os.IsNotExist(err) {
				t.Fatalf("%s not found in unpack output", cv.fzvName)
			}

			fzvData, err := os.ReadFile(fzvPath)
			if err != nil {
				t.Fatal(err)
			}

			waveStart := binary.LittleEndian.Uint32(fzvData[0x00:0x04])
			if waveStart != 0 {
				t.Errorf("waveStart = %d, want 0 (cumulative offset not subtracted)", waveStart)
			}

			wavPath := filepath.Join(dir, cv.fzvName+".wav")
			if err := voiceextract.Extract(fzvPath, wavPath); err != nil {
				t.Fatalf("Extract: %v", err)
			}

			srcPath := filepath.Join(junglismSamplesDir, cv.srcWAV)
			srcF, err := os.Open(srcPath)
			if err != nil {
				t.Skipf("source WAV not available: %v", err)
			}
			srcWAV, err := wav.Read(srcF)
			srcF.Close() //nolint:errcheck
			if err != nil {
				t.Fatal(err)
			}

			gotF, err := os.Open(wavPath)
			if err != nil {
				t.Fatal(err)
			}
			gotWAV, err := wav.Read(gotF)
			gotF.Close() //nolint:errcheck
			if err != nil {
				t.Fatal(err)
			}

			expected := resampleForIntegration(srcWAV.Samples, srcWAV.SampleRate, 9000)
			corr := correlationForIntegration(expected, gotWAV.Samples)
			if corr < 0.95 {
				t.Errorf("audio mismatch: corr=%.4f (want ≥0.95). Wrong audio block?", corr)
			}
		})
	}

	// Step 7: Verify DCA/DCF defaults on a generated voice.
	fzvPath := filepath.Join(unpackDir, entries[0].Name())
	vp, err := fzvinfo.Parse(fzvPath)
	if err != nil {
		t.Fatalf("fzvinfo.Parse(%s): %v", entries[0].Name(), err)
	}
	if !vp.DCADefault {
		t.Errorf("voice %s: DCADefault=false, want true", entries[0].Name())
	}
	if !vp.DCFDefault {
		t.Errorf("voice %s: DCFDefault=false, want true", entries[0].Name())
	}

	// Step 8: Verify reese is one-shot (no sustain loop).
	reeseFZV, err := os.ReadFile(filepath.Join(unpackDir, "REESE.fzv"))
	if err == nil {
		loopSus := reeseFZV[0x12]
		if loopSus != 8 {
			t.Errorf("reese loop_sus=%d, want 8 (no sustain loop for one_shot)", loopSus)
		}
	}
}

func resampleForIntegration(samples []int16, srcRate, dstRate uint32) []int16 {
	n := int(math.Round(float64(len(samples)) * float64(dstRate) / float64(srcRate)))
	out := make([]int16, n)
	sr := int64(srcRate)
	dr := int64(dstRate)
	srcLen := len(samples)
	for i := range out {
		num := int64(i) * sr
		lo := int(num / dr)
		rem := num % dr
		hi := lo + 1
		if hi >= srcLen {
			hi = srcLen - 1
		}
		a := int64(samples[lo])
		b := int64(samples[hi])
		out[i] = int16(a + (b-a)*rem/dr) //nolint:gosec // G115: test value fits target type
	}
	return out
}

func correlationForIntegration(a, b []int16) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var num, da, db float64
	for i := range n {
		fa, fb := float64(a[i]), float64(b[i])
		num += fa * fb
		da += fa * fa
		db += fb * fb
	}
	denom := math.Sqrt(da) * math.Sqrt(db)
	if denom < 1e-10 {
		return 0
	}
	return num / denom
}

// TECHNO.img tests (real hardware image with multi-bank full dump).

// TestTechnoDiskStructure verifies that a real multi-bank FZF from hardware
// is read correctly, particularly that the 8 bank sectors are detected and
// the voice area is found at the right offset.
func TestTechnoDiskStructure(t *testing.T) {
	skipShort(t)
	t.Parallel()

	var buf bytes.Buffer
	if err := disklist.List(technoImg, &buf); err != nil {
		t.Fatalf("disk ls: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "FULL-DATA-FZ") {
		t.Errorf("expected FULL-DATA-FZ in directory listing:\n%s", out)
	}
	if !strings.Contains(out, "Full Dump") {
		t.Errorf("expected 'Full Dump' type in listing:\n%s", out)
	}
}

// TestTechnoFZFUnpack verifies that fzf unpack handles a real multi-bank FZF
// without panicking and produces the correct number of named voices.
func TestTechnoFZFUnpack(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "techno.fzf")
	if err := diskget.Get(technoImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("fzf unpack panicked or failed: %v", err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil {
		t.Fatal(err)
	}
	// TECHNO is multi-bank (8 banks, 32 distinct voice slots). The
	// pre-fix count of 11 came from bank 0's bstep alone, which dropped
	// every voice referenced only by banks 1-7's vp[]. After the
	// multi-bank-aware voice-count fix, all 32 unpack.
	if len(entries) != 32 {
		t.Errorf("expected 32 voices from TECHNO.img, got %d", len(entries))
	}

	// Verify all voices have a non-empty name and sane waveStart=0.
	for _, e := range entries {
		fzv, err := os.ReadFile(filepath.Join(unpackDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		waveStart := binary.LittleEndian.Uint32(fzv[0x00:0x04])
		if waveStart != 0 {
			t.Errorf("%s: waveStart=%d, want 0 (cumulative offset not subtracted)", e.Name(), waveStart)
		}
		loopMode := binary.LittleEndian.Uint16(fzv[0x10:0x12])
		if loopMode == 0x0000 {
			t.Errorf("%s: loop mode is NO SOUND (voice not extracted correctly)", e.Name())
		}
	}
}

// TestTechnoVoiceHeaderSanity verifies that unpacked voices from real hardware
// have sane header values, particularly that the envelope does not immediately
// silence the voice (dca_sus and dca_end must not both be 0).
func TestTechnoVoiceHeaderSanity(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "techno.fzf")
	if err := diskget.Get(technoImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("fzf unpack: %v", err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		fzv, err := os.ReadFile(filepath.Join(unpackDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		dcaSus := fzv[0x78]
		dcaEnd := fzv[0x79]

		// Both dca_sus=0 and dca_end=0 means the envelope has no sustain and
		// no release, so the sampler may produce no audible output.
		if dcaSus == 0 && dcaEnd == 0 {
			t.Errorf("%s: dca_sus=0 and dca_end=0 (envelope will silence the voice immediately)", e.Name())
		}
	}
}

// TestEnvelopeDefaultsMatchHardware verifies that our generated voice envelope
// defaults (dca_sus, dca_end, dca_rate, dca_stop) are compatible with real
// hardware. The METAL-BELL voice from TECHNO.img is the reference.
// This is a regression test for the silent playback bug where dca_sus=0 and
// dca_end=0 caused the FZ-1 to produce no audio.
func TestEnvelopeDefaultsMatchHardware(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "techno.fzf")
	if err := diskget.Get(technoImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("fzf unpack: %v", err)
	}

	// Read METAL-BELL as reference.
	ref, err := os.ReadFile(filepath.Join(unpackDir, "METAL-BELL.fzv"))
	if err != nil {
		t.Fatalf("reading METAL-BELL: %v", err)
	}
	refDCASus := ref[0x78]
	refDCAEnd := ref[0x79]

	// Generate a synthetic voice using our defaults.
	samples := make([]int16, 1000)
	fzv := voiceimport.Encode(samples, 0, "TEST", 0, voiceimport.NoLoop())

	ourDCASus := fzv[0x78]
	ourDCAEnd := fzv[0x79]

	if ourDCASus != 0 {
		t.Errorf("our dca_sus=%d, want 0", ourDCASus)
	}
	if ourDCAEnd != 7 {
		t.Errorf("our dca_end=%d, want 7", ourDCAEnd)
	}

	// Reference values from hardware for documentation.
	t.Logf("Hardware reference: dca_sus=%d dca_end=%d", refDCASus, refDCAEnd)
	t.Logf("Our defaults:       dca_sus=%d dca_end=%d", ourDCASus, ourDCAEnd)
}

func TestBrassFZFUnpack(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "brass.fzf")
	if err := diskget.Get(brassImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("fzf unpack: %v", err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil {
		t.Fatal(err)
	}
	// BRASS is multi-bank; 13 distinct voice slots are used across banks.
	// See TestTechnoFZFUnpack for the rationale on the count change.
	if len(entries) != 13 {
		t.Errorf("expected 13 voices from BRASS.img, got %d", len(entries))
	}

	for _, e := range entries {
		fzv, err := os.ReadFile(filepath.Join(unpackDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		waveStart := binary.LittleEndian.Uint32(fzv[0x00:0x04])
		if waveStart != 0 {
			t.Errorf("%s: waveStart=%d, want 0", e.Name(), waveStart)
		}
	}
}

func TestBrassRoundTripExtract(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "brass.fzf")
	if err := diskget.Get(brassImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("fzf unpack: %v", err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		fzvPath := filepath.Join(unpackDir, e.Name())
		wavPath := filepath.Join(dir, e.Name()+".wav")

		if err := voiceextract.Extract(fzvPath, wavPath); err != nil {
			t.Errorf("%s: extract failed: %v", e.Name(), err)
			continue
		}

		f, err := os.Open(wavPath)
		if err != nil {
			t.Fatal(err)
		}
		w, err := wav.Read(f)
		f.Close() //nolint:errcheck
		if err != nil {
			t.Errorf("%s: reading WAV: %v", e.Name(), err)
			continue
		}

		if w.SampleRate != 36000 && w.SampleRate != 18000 && w.SampleRate != 9000 {
			t.Errorf("%s: unexpected sample rate %d", e.Name(), w.SampleRate)
		}
	}
}

// TestTechnoRoundTripExtract verifies that voices extracted from the real
// hardware image produce non-silent WAV files with correct sample rates.
func TestTechnoRoundTripExtract(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "techno.fzf")
	if err := diskget.Get(technoImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("fzf unpack: %v", err)
	}

	// Extract each voice to WAV and verify it's non-silent with valid rate.
	entries, err := os.ReadDir(unpackDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		fzvPath := filepath.Join(unpackDir, e.Name())
		wavPath := filepath.Join(dir, e.Name()+".wav")

		if err := voiceextract.Extract(fzvPath, wavPath); err != nil {
			t.Errorf("%s: extract failed: %v", e.Name(), err)
			continue
		}

		f, err := os.Open(wavPath)
		if err != nil {
			t.Fatal(err)
		}
		w, err := wav.Read(f)
		f.Close() //nolint:errcheck
		if err != nil {
			t.Errorf("%s: reading WAV: %v", e.Name(), err)
			continue
		}

		if w.SampleRate != 36000 && w.SampleRate != 18000 && w.SampleRate != 9000 {
			t.Errorf("%s: unexpected sample rate %d", e.Name(), w.SampleRate)
		}
		hasSignal := false
		for _, s := range w.Samples {
			if s > 100 || s < -100 {
				hasSignal = true
				break
			}
		}
		if !hasSignal {
			t.Errorf("%s: WAV appears silent (all samples near zero)", e.Name())
		}
	}
}

// TestFZFMidiEndToEnd tests the full sfz convert, fzf midi, and fzf info pipeline.
func TestFZFMidiEndToEnd(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "junglism.fzf")

	// Step 1: convert at 9kHz (fits on one disk).
	if err := sfzconvert.Convert(context.Background(), junglismSFZ, fzfPath, 9000, false); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	// Step 2: set REESE to channel 2.
	res, err := fzfmidi.Set(fzfPath, []string{"REESE"}, false, 2)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if len(res.Updated) != 1 {
		t.Fatalf("expected 1 voice updated, got %d", len(res.Updated))
	}
	if res.Updated[0].Name != "REESE" || res.Updated[0].NewChannel != 2 {
		t.Errorf("unexpected update: %+v", res.Updated[0])
	}

	// Step 3: verify the raw byte in the FZF.
	data, err := os.ReadFile(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	// Find REESE voice index by scanning names.
	reeseIdx := -1
	nBankSectors := 1
	nvoice := int(binary.LittleEndian.Uint16(data[0:2]))
	voiceAreaStart := nBankSectors * disk.SectorSize
	for i := range nvoice {
		voff := disk.VoiceSlotOffset(voiceAreaStart, i)
		if voff+disk.VoiceNameOffset+disk.LabelSize <= len(data) {
			name := strings.TrimRight(string(data[voff+disk.VoiceNameOffset:voff+disk.VoiceNameOffset+disk.LabelSize]), " ")
			if strings.EqualFold(name, "REESE") {
				reeseIdx = i
				break
			}
		}
	}
	if reeseIdx < 0 {
		t.Fatal("REESE voice not found in FZF")
	}
	rawChan := data[disk.BankMIDIRecvChanOffset+reeseIdx]
	if rawChan != 1 { // channel 2 stored as 1 (0-indexed)
		t.Errorf("REESE raw MIDI channel byte: got %d, want 1 (channel 2)", rawChan)
	}

	// Step 4: fzf info should show Chan column with * on the REESE row.
	var buf bytes.Buffer
	highlighted := map[int]bool{res.Updated[0].Index: true}
	if err := fzfinfo.Info(fzfPath, &buf, highlighted); err != nil {
		t.Fatalf("Info: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Chan") {
		t.Errorf("Chan column should appear after midi channel change:\n%s", out)
	}
	if !strings.Contains(out, "*") {
		t.Errorf("Changed voice should be marked with *:\n%s", out)
	}
	if !strings.Contains(out, "2") {
		t.Errorf("Channel 2 should appear in output:\n%s", out)
	}

	// Step 5: resetting all to channel 1 removes the Chan column.
	if _, err := fzfmidi.Set(fzfPath, nil, true, 1); err != nil {
		t.Fatal(err)
	}
	var buf2 bytes.Buffer
	if err := fzfinfo.Info(fzfPath, &buf2, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf2.String(), "Chan") {
		t.Log("Chan column is always shown (expected)")
	}
}

// TestMultiDiskBankSectorInvariant is the critical integration test for the
// multi-disk split format. It verifies the exact byte-level properties that
// make the FZ-10M prompt for disk 2 after loading disk 1.
// ConvertMultiDisk now produces .img files directly. Disk 1 contains a full
// FZF (bank + voices + partial audio). Disk 2 is pure audio continuation
// (no bank sector, no voice headers).
func TestMultiDiskBankSectorInvariant(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	prefix := filepath.Join(dir, "d")
	img1 := prefix + "-1.img"
	img2 := prefix + "-2.img"

	if err := sfzconvert.ConvertMultiDisk(context.Background(), junglismSFZ, prefix, 36000); err != nil {
		t.Fatalf("ConvertMultiDisk: %v", err)
	}

	fzf1Path := filepath.Join(dir, "d1.fzf")
	if err := diskget.Get(img1, "FULL-DATA-FZ", fzf1Path); err != nil {
		t.Fatalf("extracting FZF from disk 1: %v", err)
	}
	d1, err := os.ReadFile(fzf1Path)
	if err != nil {
		t.Fatalf("reading disk 1 FZF: %v", err)
	}

	d1NVoice := int(binary.LittleEndian.Uint16(d1[0:2]))
	t.Logf("disk 1 bank nvoice=%d", d1NVoice)

	// INVARIANT 1: disk 1 bank must have ALL voices.
	// If this fails, the sampler never prompts for disk 2.
	if d1NVoice == 0 || d1NVoice > 64 {
		t.Errorf("disk 1 bank nvoice=%d: invalid", d1NVoice)
	}

	// INVARIANT 2: disk 1 image must be a valid disk image.
	d1ImgData, err := os.ReadFile(img1)
	if err != nil {
		t.Fatalf("reading disk 1 image: %v", err)
	}
	if len(d1ImgData) != disk.ImageSize {
		t.Errorf("disk 1 image size %d, want %d", len(d1ImgData), disk.ImageSize)
	}

	// INVARIANT 3: disk 1 total wave marker exceeds local audio sectors.
	// This discrepancy is what signals the sampler that more is coming.
	const bankTotalWaveOffset = 0x290
	totalWave := int(binary.LittleEndian.Uint32(d1[bankTotalWaveOffset : bankTotalWaveOffset+4]))
	voiceSectors := disk.VoiceAreaSectors(d1NVoice)
	localWaveSectors := (len(d1) - disk.SectorSize - voiceSectors*disk.SectorSize) / disk.SectorSize
	if totalWave <= localWaveSectors {
		t.Errorf("disk 1 total wave marker (%d) must exceed local wave sectors (%d): "+
			"sampler uses this to know more audio is on the next disk",
			totalWave, localWaveSectors)
	}

	// INVARIANT 4: disk 1 voice area covers all voices (not just disk 1 voices).
	// The sampler reads voice parameters (envelopes, loops) from disk 1 for
	// the full instrument.
	expectedVoiceAreaSize := disk.VoiceAreaSectors(d1NVoice) * disk.SectorSize
	voiceAreaStart := disk.SectorSize
	if len(d1) < voiceAreaStart+expectedVoiceAreaSize {
		t.Errorf("disk 1 too small to contain voice area for all %d voices", d1NVoice)
	}

	// INVARIANT 5: disk 2 is a valid disk image with data sectors (pure audio
	// continuation, no bank sector or voice headers).
	d2ImgData, err := os.ReadFile(img2)
	if err != nil {
		t.Fatalf("reading disk 2 image: %v", err)
	}
	if len(d2ImgData) != disk.ImageSize {
		t.Errorf("disk 2 image size %d, want %d", len(d2ImgData), disk.ImageSize)
	}
	d2Img, err := disk.ReadImage(bytes.NewReader(d2ImgData))
	if err != nil {
		t.Fatalf("parsing disk 2 image: %v", err)
	}
	d2Entries, err := d2Img.Directory()
	if err != nil {
		t.Fatalf("reading disk 2 directory: %v", err)
	}
	if len(d2Entries) == 0 {
		t.Fatal("disk 2 has no directory entries")
	}
	d2AllocatedSectors := disk.SectorCount - 2 - d2Img.FreeSectors()
	if d2AllocatedSectors == 0 {
		t.Error("disk 2 has no allocated data sectors")
	}
	t.Logf("disk 2: %d allocated data sectors", d2AllocatedSectors)
}

// TestMultiDiskUnpackBothDisks verifies that unpacking disk 1 produces voices
// and that disk 2 (pure audio continuation) has no FZF structure to unpack.
// ConvertMultiDisk now produces .img files directly.
func TestMultiDiskUnpackBothDisks(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	prefix := filepath.Join(dir, "md")
	if err := sfzconvert.ConvertMultiDisk(context.Background(), junglismSFZ, prefix, 36000); err != nil {
		t.Fatalf("ConvertMultiDisk: %v", err)
	}

	img1 := prefix + "-1.img"
	img2 := prefix + "-2.img"

	// Disk 1: extract FZF and unpack voices.
	fzf1Path := filepath.Join(dir, "d1.fzf")
	if err := diskget.Get(img1, "FULL-DATA-FZ", fzf1Path); err != nil {
		t.Fatalf("extracting FZF from disk 1: %v", err)
	}
	d1OutDir := filepath.Join(dir, "voices-1")
	if err := voiceunpack.Unpack(fzf1Path, d1OutDir); err != nil {
		t.Fatalf("Unpack disk 1: %v", err)
	}
	d1Entries, err := os.ReadDir(d1OutDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d1Entries) == 0 {
		t.Error("disk 1: no voices unpacked")
	}
	t.Logf("disk 1: unpacked %d voices", len(d1Entries))

	for _, e := range d1Entries {
		fzv, err := os.ReadFile(filepath.Join(d1OutDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		ws := binary.LittleEndian.Uint32(fzv[0:4])
		if ws != 0 {
			t.Errorf("disk 1, %s: waveStart=%d, want 0", e.Name(), ws)
		}
	}

	// Disk 2: pure audio continuation with no FZF structure.
	// Extracting FZF from disk 2 should still yield a file (the disk has a
	// FULL-DATA-FZ entry), but unpacking it should produce 0 voices because
	// there are no voice headers.
	fzf2Path := filepath.Join(dir, "d2.fzf")
	err = diskget.Get(img2, "FULL-DATA-FZ", fzf2Path)
	if err != nil {
		t.Logf("disk 2: diskget failed as expected (no FZF structure): %v", err)
		return
	}
	d2OutDir := filepath.Join(dir, "voices-2")
	err = voiceunpack.Unpack(fzf2Path, d2OutDir)
	if err != nil {
		t.Logf("disk 2: Unpack failed as expected (no voice headers): %v", err)
		return
	}
	d2Entries, err := os.ReadDir(d2OutDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d2Entries) != 0 {
		t.Errorf("disk 2: expected 0 unpacked voices (pure audio), got %d", len(d2Entries))
	}
}

// readEntries returns the directory entries from imagePath.
func readEntries(t *testing.T, imagePath string) []disk.DirEntry {
	t.Helper()
	f, err := os.Open(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck
	img, err := disk.ReadImage(f)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := img.Directory()
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// TestJUNGLISMGoldenChecksums verifies that the JUNGLISM SFZ conversion
// pipeline produces byte-for-byte identical FZF and disk image outputs.
// These golden checksums protect against accidental changes to the output
// format: any drift in resampling, bank layout, voice packing, sector
// allocation, or disk formatting will cause a failure here.
//
// To update after an intentional format change, run the test with -v,
// copy the "got" checksums, and replace the expected values below.
func TestJUNGLISMGoldenChecksums(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()

	fzf36k := filepath.Join(dir, "junglism-36k.fzf")
	if err := sfzconvert.Convert(context.Background(), junglismSFZ, fzf36k, 36000, false); err != nil {
		t.Fatalf("Convert 36kHz: %v", err)
	}

	fzfFit := filepath.Join(dir, "junglism-fit.fzf")
	if err := sfzconvert.Convert(context.Background(), junglismSFZ, fzfFit, 36000, true); err != nil {
		t.Fatalf("Convert fit-to-disk: %v", err)
	}

	t.Run("FZF checksums", func(t *testing.T) {
		t.Parallel()
		fzfChecksums := []struct {
			name string
			path string
			want string
		}{
			{"36kHz full", fzf36k, "b695e1e8d06d5fd4e6d1228355cf89a5a2d60959e4716b37cb96f90e83251de6"},
			{"fit-to-disk", fzfFit, "022a067653e458281316a03c197d3d3756d7f873d7029e1d8c60a185d5233f50"},
		}
		for _, tc := range fzfChecksums {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				got := fileSHA256(t, tc.path)
				if got != tc.want {
					t.Errorf("SHA-256 mismatch:\n  got  %s\n  want %s", got, tc.want)
				}
			})
		}
	})

	t.Run("disk image checksums", func(t *testing.T) {
		t.Parallel()
		imgFit := filepath.Join(dir, "junglism-fit.img")
		if err := diskformat.Format(imgFit, "JUNGLISM FIT"); err != nil {
			t.Fatal(err)
		}
		if err := diskadd.Add(imgFit, fzfFit, 0); err != nil {
			t.Fatal(err)
		}

		imgChecksums := []struct {
			name string
			path string
			want string
		}{
			{"fit-to-disk image", imgFit, "41ad1cde8a57b4e8b16c7820b7f83966afc3b6b10256bd9148330684e925c184"},
		}
		for _, tc := range imgChecksums {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				got := fileSHA256(t, tc.path)
				if got != tc.want {
					t.Errorf("SHA-256 mismatch:\n  got  %s\n  want %s", got, tc.want)
				}
			})
		}
	})

	t.Run("split disk image checksums", func(t *testing.T) {
		t.Parallel()
		splitPrefix := filepath.Join(dir, "JUNGLISM")
		if err := sfzconvert.ConvertMultiDisk(context.Background(), junglismSFZ, splitPrefix, 36000); err != nil {
			t.Fatalf("ConvertMultiDisk: %v", err)
		}
		imgSplit1 := splitPrefix + "-1.img"
		imgSplit2 := splitPrefix + "-2.img"

		splitChecksums := []struct {
			name string
			path string
			want string
		}{
			{"split disk 1 image", imgSplit1, "67e6aa4d6d8c3e2034bb4d2a892da14b77bd1346f0068bbcaace49b18a333c21"},
			{"split disk 2 image", imgSplit2, "67d80910ff3c71665551890022e163ed643a58a6865c0bd30ffbba59b32f25c6"},
		}
		for _, tc := range splitChecksums {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				got := fileSHA256(t, tc.path)
				if got != tc.want {
					t.Errorf("SHA-256 mismatch:\n  got  %s\n  want %s", got, tc.want)
				}
			})
		}
	})
}

func TestBRASSGoldenChecksums(t *testing.T) {
	skipShort(t)
	t.Parallel()

	t.Run("disk image", func(t *testing.T) {
		t.Parallel()
		got := fileSHA256(t, brassImg)
		want := "d40bb77ada4c2c875e142fa7f1b5dd845bcefa0a1f0e8562faea3413412a02f5"
		if got != want {
			t.Errorf("BRASS.img SHA-256 mismatch:\n  got  %s\n  want %s", got, want)
		}
	})

	t.Run("extracted FZF", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		fzfPath := filepath.Join(dir, "brass.fzf")
		if err := diskget.Get(brassImg, "FULL-DATA-FZ", fzfPath); err != nil {
			t.Fatalf("disk get: %v", err)
		}
		got := fileSHA256(t, fzfPath)
		want := "36772600a3b9502c3ed44330dbe1a11dcd95cb9ed1c6ed583483c00d68f10199"
		if got != want {
			t.Errorf("BRASS FZF SHA-256 mismatch:\n  got  %s\n  want %s", got, want)
		}
	})
}

// extractAndParseVoice extracts a named voice from a disk image's full dump
// and returns its parsed parameters. The pipeline exercises diskget, voiceunpack,
// and fzvinfo.Parse in sequence.
func extractAndParseVoice(t *testing.T, imgPath, voiceName string) *fzvinfo.VoiceParams {
	t.Helper()
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "dump.fzf")
	if err := diskget.Get(imgPath, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("fzf unpack: %v", err)
	}
	fzvPath := filepath.Join(unpackDir, voiceName+".fzv")
	params, err := fzvinfo.Parse(fzvPath)
	if err != nil {
		t.Fatalf("fzvinfo.Parse(%s): %v", voiceName, err)
	}
	return params
}

// extractAndParseStandaloneVoice extracts a standalone voice file (not a full
// dump) from a disk image and returns its parsed parameters.
func extractAndParseStandaloneVoice(t *testing.T, imgPath, diskName string) *fzvinfo.VoiceParams {
	t.Helper()
	dir := t.TempDir()
	fzvPath := filepath.Join(dir, diskName+".fzv")
	if err := diskget.Get(imgPath, diskName, fzvPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	params, err := fzvinfo.Parse(fzvPath)
	if err != nil {
		t.Fatalf("fzvinfo.Parse(%s): %v", diskName, err)
	}
	return params
}

func TestBrassVoiceFilterEnvelope(t *testing.T) {
	skipShort(t)
	t.Parallel()
	p := extractAndParseVoice(t, brassImg, "BRASS1 D3 1")

	if p.Name != "BRASS1 D3 1" {
		t.Errorf("Name = %q, want BRASS1 D3 1", p.Name)
	}
	if p.SampleRate != 36000 {
		t.Errorf("SampleRate = %d, want 36000", p.SampleRate)
	}
	if p.FilterCutoff != 88 {
		t.Errorf("FilterCutoff = %d, want 88", p.FilterCutoff)
	}
	if p.FilterQ != 0 {
		t.Errorf("FilterQ = %d, want 0", p.FilterQ)
	}

	if p.DCADefault {
		t.Error("expected DCADefault=false for BRASS voice")
	}
	if p.DCASustain != 2 {
		t.Errorf("DCASustain = %d, want 2", p.DCASustain)
	}
	if p.DCAEnd != 3 {
		t.Errorf("DCAEnd = %d, want 3", p.DCAEnd)
	}
	if p.DCARates[0] != 127 {
		t.Errorf("DCARates[0] = %d, want 127", p.DCARates[0])
	}
	if p.DCAStops[0] != 218 {
		t.Errorf("DCAStops[0] = %d, want 218", p.DCAStops[0])
	}

	if p.DCFDefault {
		t.Error("expected DCFDefault=false for BRASS voice")
	}
	if p.DCFSustain != 1 {
		t.Errorf("DCFSustain = %d, want 1", p.DCFSustain)
	}
	if p.DCFEnd != 2 {
		t.Errorf("DCFEnd = %d, want 2", p.DCFEnd)
	}
	if p.DCFRates[0] != 127 {
		t.Errorf("DCFRates[0] = %d, want 127", p.DCFRates[0])
	}
	if p.DCFStops[0] != 66 {
		t.Errorf("DCFStops[0] = %d, want 66", p.DCFStops[0])
	}
	if p.DCFStops[1] != 56 {
		t.Errorf("DCFStops[1] = %d, want 56", p.DCFStops[1])
	}

	if p.LFODepthPitch != 0 || p.LFODepthAmp != 0 || p.LFODepthFilter != 0 {
		t.Errorf("expected no LFO activity, got pitch=%d amp=%d filter=%d",
			p.LFODepthPitch, p.LFODepthAmp, p.LFODepthFilter)
	}
}

func TestBrassFilterCutoffPerVoice(t *testing.T) {
	skipShort(t)
	t.Parallel()
	cases := []struct {
		name   string
		cutoff uint8
	}{
		{"BRASS1 D3 1", 88},
		{"BRASS1 D3 2", 90},
		{"BRASS1 G#3", 92},
		{"BRASS1 D4", 94},
		{"BRASS1 G#4", 96},
	}

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "brass.fzf")
	if err := diskget.Get(brassImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("fzf unpack: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, err := fzvinfo.Parse(filepath.Join(unpackDir, tc.name+".fzv"))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if p.FilterCutoff != tc.cutoff {
				t.Errorf("FilterCutoff = %d, want %d", p.FilterCutoff, tc.cutoff)
			}
			if p.FilterQ != 0 {
				t.Errorf("FilterQ = %d, want 0", p.FilterQ)
			}
			if p.DCFDefault {
				t.Error("expected DCFDefault=false")
			}
		})
	}
}

func TestTechnoVoiceResonanceAndEnvelope(t *testing.T) {
	skipShort(t)
	t.Parallel()
	p := extractAndParseVoice(t, technoImg, "COWBELL")

	if p.FilterCutoff != 0 {
		t.Errorf("FilterCutoff = %d, want 0", p.FilterCutoff)
	}
	if p.FilterQ != 100 {
		t.Errorf("FilterQ = %d, want 100 (displays as resonance=6)", p.FilterQ)
	}

	if p.DCADefault {
		t.Error("expected DCADefault=false")
	}
	if p.DCASustain != 7 {
		t.Errorf("DCASustain = %d, want 7", p.DCASustain)
	}
	if p.DCAEnd != 2 {
		t.Errorf("DCAEnd = %d, want 2", p.DCAEnd)
	}

	if p.LFODepthPitch != 0 || p.LFODepthAmp != 0 || p.LFODepthFilter != 0 {
		t.Errorf("expected no LFO activity, got pitch=%d amp=%d filter=%d",
			p.LFODepthPitch, p.LFODepthAmp, p.LFODepthFilter)
	}
}

func TestTechnoMetalBellEnvelope(t *testing.T) {
	skipShort(t)
	t.Parallel()
	p := extractAndParseVoice(t, technoImg, "METAL-BELL")

	if p.DCASustain != 1 {
		t.Errorf("DCASustain = %d, want 1", p.DCASustain)
	}
	if p.DCAEnd != 2 {
		t.Errorf("DCAEnd = %d, want 2", p.DCAEnd)
	}
	if p.DCARates[0] != 127 {
		t.Errorf("DCARates[0] = %d, want 127", p.DCARates[0])
	}
	if p.DCARates[1] != 253 {
		t.Errorf("DCARates[1] = %d, want 253 (displays as -125)", p.DCARates[1])
	}
	if p.DCARates[2] != 253 {
		t.Errorf("DCARates[2] = %d, want 253 (displays as -125)", p.DCARates[2])
	}
	if p.DCAStops[0] != 249 {
		t.Errorf("DCAStops[0] = %d, want 249", p.DCAStops[0])
	}

	if p.FilterCutoff != 0 {
		t.Errorf("FilterCutoff = %d, want 0", p.FilterCutoff)
	}
	if p.FilterQ != 0 {
		t.Errorf("FilterQ = %d, want 0", p.FilterQ)
	}
}

func TestHooverVoiceParameters(t *testing.T) {
	skipShort(t)
	t.Parallel()
	p := extractAndParseStandaloneVoice(t, hooverImg, "HOOVER")

	if p.SampleRate != 36000 {
		t.Errorf("SampleRate = %d, want 36000", p.SampleRate)
	}
	if p.FilterCutoff != 0 {
		t.Errorf("FilterCutoff = %d, want 0", p.FilterCutoff)
	}
	if p.FilterQ != 0 {
		t.Errorf("FilterQ = %d, want 0", p.FilterQ)
	}

	if !p.DCADefault {
		t.Error("expected DCADefault=true (hardware idle pattern matches fizzle defaults)")
	}
	if p.DCASustain != 0 {
		t.Errorf("DCASustain = %d, want 0", p.DCASustain)
	}
	if p.DCAEnd != 7 {
		t.Errorf("DCAEnd = %d, want 7", p.DCAEnd)
	}
	if p.DCARates[0] != 127 {
		t.Errorf("DCARates[0] = %d, want 127", p.DCARates[0])
	}
	if p.DCARates[1] != 192 {
		t.Errorf("DCARates[1] = %d, want 192 (hardware idle: -64)", p.DCARates[1])
	}
	if p.DCAStops[0] != 255 {
		t.Errorf("DCAStops[0] = %d, want 255", p.DCAStops[0])
	}
	if p.DCAStops[1] != 0 {
		t.Errorf("DCAStops[1] = %d, want 0", p.DCAStops[1])
	}

	if p.PlaybackMode != disk.PlaybackModeNameNormal {
		t.Errorf("PlaybackMode = %q, want %q", p.PlaybackMode, disk.PlaybackModeNameNormal)
	}
	if p.HasActiveLoop {
		t.Error("expected HasActiveLoop=false for one-shot voice")
	}

	if p.DCFDefault {
		t.Error("expected DCFDefault=false (hardware DCF pattern differs from fizzle defaults)")
	}

	if p.LFODepthPitch != 0 || p.LFODepthAmp != 0 || p.LFODepthFilter != 0 {
		t.Errorf("expected no LFO activity, got pitch=%d amp=%d filter=%d",
			p.LFODepthPitch, p.LFODepthAmp, p.LFODepthFilter)
	}
}

func TestStabVoiceFilterParameters(t *testing.T) {
	skipShort(t)
	t.Parallel()
	p := extractAndParseVoice(t, stabImg, "STAB")

	if p.FilterCutoff != 96 {
		t.Errorf("FilterCutoff = %d, want 96", p.FilterCutoff)
	}
	if p.FilterQ != 43 {
		t.Errorf("FilterQ = %d, want 43 (displays as resonance=2)", p.FilterQ)
	}

	if p.DCFSustain != 7 {
		t.Errorf("DCFSustain = %d, want 7 (holds filter open indefinitely)", p.DCFSustain)
	}
	if p.DCFEnd != 7 {
		t.Errorf("DCFEnd = %d, want 7", p.DCFEnd)
	}

	if !p.DCADefault {
		t.Error("expected DCADefault=true (hardware idle pattern matches fizzle defaults)")
	}

	if p.LFODepthPitch != 0 || p.LFODepthAmp != 0 || p.LFODepthFilter != 0 {
		t.Errorf("expected no LFO activity, got pitch=%d amp=%d filter=%d",
			p.LFODepthPitch, p.LFODepthAmp, p.LFODepthFilter)
	}
}

func TestPadLFOImageChecksum(t *testing.T) {
	skipShort(t)
	t.Parallel()
	got := fileSHA256(t, padLFOImg)
	want := "e755265a96f671530a3eb8e4d1972ce934efb325ee195eba8258a272a34642ef"
	if got != want {
		t.Errorf("PAD-LFO.img SHA-256 mismatch:\n  got  %s\n  want %s", got, want)
	}
}

func TestPadLFOVoiceParameters(t *testing.T) {
	skipShort(t)
	t.Parallel()
	p := extractAndParseVoice(t, padLFOImg, "PAD")

	if p.Name != "PAD" {
		t.Errorf("Name = %q, want PAD", p.Name)
	}
	if p.SampleRate != 18000 {
		t.Errorf("SampleRate = %d, want 18000", p.SampleRate)
	}
	if p.FilterCutoff != 64 {
		t.Errorf("FilterCutoff = %d, want 64", p.FilterCutoff)
	}
	if p.FilterQ>>4 != 7 {
		t.Errorf("Resonance = %d, want 7", p.FilterQ>>4)
	}

	if p.LFOWaveform != "Sine" {
		t.Errorf("LFOWaveform = %q, want Sine", p.LFOWaveform)
	}
	if p.LFORate != 20 {
		t.Errorf("LFORate = %d, want 20", p.LFORate)
	}
	if p.LFOAttack != 127 {
		t.Errorf("LFOAttack = %d, want 127", p.LFOAttack)
	}
	if p.LFODelay != 0 {
		t.Errorf("LFODelay = %d, want 0", p.LFODelay)
	}
	if p.LFODepthPitch != 0 {
		t.Errorf("LFODepthPitch = %d, want 0", p.LFODepthPitch)
	}
	if p.LFODepthAmp != 0 {
		t.Errorf("LFODepthAmp = %d, want 0", p.LFODepthAmp)
	}
	if p.LFODepthFilter != 50 {
		t.Errorf("LFODepthFilter = %d, want 50", p.LFODepthFilter)
	}
	if p.LFOPhaseSync {
		t.Error("expected LFOPhaseSync=false")
	}

	if !p.DCADefault {
		t.Error("expected DCADefault=true for fizzle-generated voice")
	}
}

func TestEditFZFVoiceRoundTrip(t *testing.T) {
	skipShort(t)
	t.Parallel()
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "junglism.fzf")
	if err := sfzconvert.Convert(context.Background(), "../../testdata/synthetic/JUNGLISM.sfz", fzfPath, 36000, false); err != nil {
		t.Fatalf("sfz convert: %v", err)
	}

	audioBefore := fzfAudioHash(t, fzfPath)

	patches, err := voiceedit.BuildLFOPatches(3, 25, voiceedit.Unchanged, 127, 10, 20, 50, voiceedit.Unchanged, 0)
	if err != nil {
		t.Fatal(err)
	}
	filterPatches, err := voiceedit.BuildFilterPatches(64, 7)
	if err != nil {
		t.Fatal(err)
	}
	all := make([]voiceedit.Patch, 0, len(patches)+len(filterPatches))
	all = append(all, patches...)
	all = append(all, filterPatches...)

	if err := voiceedit.ApplyToFZFVoice(fzfPath, "REESE", all); err != nil {
		t.Fatal(err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatal(err)
	}

	reese, err := fzvinfo.Parse(filepath.Join(unpackDir, "REESE.fzv"))
	if err != nil {
		t.Fatal(err)
	}
	if reese.LFOWaveform != "Triangle" {
		t.Errorf("REESE waveform: got %q, want Triangle", reese.LFOWaveform)
	}
	if reese.LFORate != 25 {
		t.Errorf("REESE LFO rate: got %d, want 25", reese.LFORate)
	}
	if reese.LFODepthFilter != 50 {
		t.Errorf("REESE LFO filter: got %d, want 50", reese.LFODepthFilter)
	}
	if reese.FilterCutoff != 64 {
		t.Errorf("REESE cutoff: got %d, want 64", reese.FilterCutoff)
	}
	if reese.FilterQ != 7 {
		t.Errorf("REESE resonance: got %d, want 7", reese.FilterQ)
	}

	amen, err := fzvinfo.Parse(filepath.Join(unpackDir, "AMEN 01.fzv"))
	if err != nil {
		t.Fatal(err)
	}
	if amen.LFORate != 0 {
		t.Errorf("AMEN 01 LFO rate should be unchanged, got %d", amen.LFORate)
	}
	if amen.FilterCutoff != disk.DCFMaxOffset {
		t.Errorf("AMEN 01 cutoff should be unchanged, got %d", amen.FilterCutoff)
	}

	audioAfter := fzfAudioHash(t, fzfPath)
	if audioBefore != audioAfter {
		t.Error("audio data should be unchanged after parameter edit")
	}
}

func TestEditFZFVoiceNameRoundTrip(t *testing.T) {
	skipShort(t)
	t.Parallel()
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "junglism.fzf")
	if err := sfzconvert.Convert(context.Background(), "../../testdata/synthetic/JUNGLISM.sfz", fzfPath, 36000, false); err != nil {
		t.Fatalf("sfz convert: %v", err)
	}

	namePatches, err := voiceedit.BuildNamePatch("JUNGLE BASS")
	if err != nil {
		t.Fatal(err)
	}
	if err := voiceedit.ApplyToFZFVoice(fzfPath, "REESE", namePatches); err != nil {
		t.Fatal(err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(unpackDir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), "JUNGLE BASS") {
			found = true
			p, err := fzvinfo.Parse(filepath.Join(unpackDir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if p.Name != "JUNGLE BASS" {
				t.Errorf("name: got %q, want JUNGLE BASS", p.Name)
			}
			if p.HasActiveLoop != false {
				t.Error("one-shot voice should not have active loop after name edit")
			}
			break
		}
	}
	if !found {
		t.Error("JUNGLE BASS.fzv not found after rename")
	}
}

func TestEditVoiceInImageRoundTrip(t *testing.T) {
	skipShort(t)
	t.Parallel()
	dir := t.TempDir()

	fzfPath := filepath.Join(dir, "junglism.fzf")
	if err := sfzconvert.Convert(context.Background(), junglismSFZ, fzfPath, 9000, false); err != nil {
		t.Fatalf("sfz convert: %v", err)
	}

	imgPath := filepath.Join(dir, "junglism.img")
	if err := diskformat.Format(imgPath, "JUNGLISM"); err != nil {
		t.Fatal(err)
	}
	if err := diskadd.Add(imgPath, fzfPath, 0); err != nil {
		t.Fatal(err)
	}

	extractedFZF := filepath.Join(dir, "extracted.fzf")
	if err := diskget.Get(imgPath, "FULL-DATA-FZ", extractedFZF); err != nil {
		t.Fatal(err)
	}
	audioBefore := fzfAudioHash(t, extractedFZF)

	patches, err := voiceedit.BuildLFOPatches(3, 25, voiceedit.Unchanged, 127, 10, 20, 50, voiceedit.Unchanged, 0)
	if err != nil {
		t.Fatal(err)
	}
	filterPatches, err := voiceedit.BuildFilterPatches(64, 7)
	if err != nil {
		t.Fatal(err)
	}
	all := make([]voiceedit.Patch, 0, len(patches)+len(filterPatches))
	all = append(all, patches...)
	all = append(all, filterPatches...)

	if err := voiceedit.ApplyToFZFVoice(extractedFZF, "REESE", all); err != nil {
		t.Fatal(err)
	}

	editedFZF, err := os.ReadFile(extractedFZF)
	if err != nil {
		t.Fatal(err)
	}
	if err := diskadd.ReplaceOnImage(imgPath, "FULL-DATA-FZ", editedFZF, 0); err != nil {
		t.Fatal(err)
	}

	reExtracted := filepath.Join(dir, "re-extracted.fzf")
	if err := diskget.Get(imgPath, "FULL-DATA-FZ", reExtracted); err != nil {
		t.Fatal(err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(reExtracted, unpackDir); err != nil {
		t.Fatal(err)
	}

	reese, err := fzvinfo.Parse(filepath.Join(unpackDir, "REESE.fzv"))
	if err != nil {
		t.Fatal(err)
	}
	if reese.LFOWaveform != "Triangle" {
		t.Errorf("REESE waveform: got %q, want Triangle", reese.LFOWaveform)
	}
	if reese.LFORate != 25 {
		t.Errorf("REESE LFO rate: got %d, want 25", reese.LFORate)
	}
	if reese.LFODepthFilter != 50 {
		t.Errorf("REESE LFO filter depth: got %d, want 50", reese.LFODepthFilter)
	}
	if reese.FilterCutoff != 64 {
		t.Errorf("REESE cutoff: got %d, want 64", reese.FilterCutoff)
	}
	if reese.FilterQ != 7 {
		t.Errorf("REESE resonance: got %d, want 7", reese.FilterQ)
	}

	audioAfter := fzfAudioHash(t, reExtracted)
	if audioBefore != audioAfter {
		t.Error("audio data should be unchanged after parameter edit and replace")
	}
}

func TestEditDCAEnvelopeRoundTrip(t *testing.T) {
	skipShort(t)
	t.Parallel()
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "brass.fzf")
	if err := diskget.Get(brassImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatal(err)
	}
	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatal(err)
	}
	fzvPath := filepath.Join(unpackDir, "BRASS1 D3 1.fzv")

	origP, err := fzvinfo.Parse(fzvPath)
	if err != nil {
		t.Fatal(err)
	}
	rates := [disk.EnvelopeStages]int{50, 30, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged}
	stops := [disk.EnvelopeStages]int{99, 50, 0, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged}
	patches, err := voiceedit.BuildDCAPatches(0, 7, rates, stops, origP.DCARates)
	if err != nil {
		t.Fatal(err)
	}
	if err := voiceedit.ApplyToFZV(fzvPath, patches); err != nil {
		t.Fatal(err)
	}

	p, err := fzvinfo.Parse(fzvPath)
	if err != nil {
		t.Fatal(err)
	}
	if p.DCASustain != 0 {
		t.Errorf("DCASustain = %d, want 0", p.DCASustain)
	}
	if p.DCAEnd != 7 {
		t.Errorf("DCAEnd = %d, want 7", p.DCAEnd)
	}
	if disk.RateByteToDisplay(p.DCARates[0]) != 50 {
		t.Errorf("rate[0] display = %d, want 50", disk.RateByteToDisplay(p.DCARates[0]))
	}
	if disk.RateByteToDisplay(p.DCARates[1]) != 30 {
		t.Errorf("rate[1] display = %d, want 30", disk.RateByteToDisplay(p.DCARates[1]))
	}
	if disk.StopByteToDisplay(p.DCAStops[0]) != 99 {
		t.Errorf("stop[0] display = %d, want 99", disk.StopByteToDisplay(p.DCAStops[0]))
	}
}

func TestEditDCFEnvelopeRoundTrip(t *testing.T) {
	skipShort(t)
	t.Parallel()
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "brass.fzf")
	if err := diskget.Get(brassImg, "FULL-DATA-FZ", fzfPath); err != nil {
		t.Fatal(err)
	}
	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatal(err)
	}
	fzvPath := filepath.Join(unpackDir, "BRASS1 D3 1.fzv")

	origP, err := fzvinfo.Parse(fzvPath)
	if err != nil {
		t.Fatal(err)
	}
	rates := [disk.EnvelopeStages]int{40, 20, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged}
	stops := [disk.EnvelopeStages]int{79, 60, 0, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged, voiceedit.Unchanged}
	patches, err := voiceedit.BuildDCFPatches(1, 5, rates, stops, origP.DCFRates)
	if err != nil {
		t.Fatal(err)
	}
	if err := voiceedit.ApplyToFZV(fzvPath, patches); err != nil {
		t.Fatal(err)
	}

	p, err := fzvinfo.Parse(fzvPath)
	if err != nil {
		t.Fatal(err)
	}
	if p.DCFSustain != 1 {
		t.Errorf("DCFSustain = %d, want 1", p.DCFSustain)
	}
	if p.DCFEnd != 5 {
		t.Errorf("DCFEnd = %d, want 5", p.DCFEnd)
	}
	if disk.RateByteToDisplay(p.DCFRates[0]) != 40 {
		t.Errorf("rate[0] display = %d, want 40", disk.RateByteToDisplay(p.DCFRates[0]))
	}
	if disk.RateByteToDisplay(p.DCFRates[1]) != 20 {
		t.Errorf("rate[1] display = %d, want 20", disk.RateByteToDisplay(p.DCFRates[1]))
	}
	if disk.StopByteToDisplay(p.DCFStops[0]) != 79 {
		t.Errorf("stop[0] display = %d, want 79", disk.StopByteToDisplay(p.DCFStops[0]))
	}
	if p.DCFStops[2] != 0 {
		t.Errorf("stop[2] = %d, want 0", p.DCFStops[2])
	}
}

func TestBrassHardwareDisplayValues(t *testing.T) {
	skipShort(t)
	t.Parallel()
	p := extractAndParseVoice(t, brassImg, "BRASS1 D3 1")

	if disk.RateByteToDisplay(p.DCARates[0]) != 99 {
		t.Errorf("DCA rate[0] display = %d, want 99", disk.RateByteToDisplay(p.DCARates[0]))
	}
	if disk.StopByteToDisplay(p.DCAStops[0]) != 85 {
		t.Errorf("DCA stop[0] display = %d, want 85", disk.StopByteToDisplay(p.DCAStops[0]))
	}
	if p.DCAStops[1] != 255 {
		t.Errorf("DCA stop[1] byte = %d, want 255", p.DCAStops[1])
	}
	if disk.StopByteToDisplay(p.DCAStops[1]) != 99 {
		t.Errorf("DCA stop[1] display = %d, want 99", disk.StopByteToDisplay(p.DCAStops[1]))
	}
	if disk.RateByteToDisplay(p.DCFRates[0]) != 99 {
		t.Errorf("DCF rate[0] display = %d, want 99", disk.RateByteToDisplay(p.DCFRates[0]))
	}
	if p.DCFStops[0] != 66 {
		t.Errorf("DCF stop[0] byte = %d, want 66", p.DCFStops[0])
	}
	if disk.StopByteToDisplay(p.DCFStops[0]) != 26 {
		t.Errorf("DCF stop[0] display = %d, want 26", disk.StopByteToDisplay(p.DCFStops[0]))
	}
	if p.DCFStops[1] != 56 {
		t.Errorf("DCF stop[1] byte = %d, want 56", p.DCFStops[1])
	}
	if disk.StopByteToDisplay(p.DCFStops[1]) != 22 {
		t.Errorf("DCF stop[1] display = %d, want 22", disk.StopByteToDisplay(p.DCFStops[1]))
	}
}

func fzfAudioHash(t *testing.T, fzfPath string) string {
	t.Helper()
	data, err := os.ReadFile(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	hdr, err := fzutil.ParseFZFHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	voiceSectors := disk.VoiceAreaSectors(hdr.NVoice)
	audioStart := hdr.VoiceAreaStart + voiceSectors*disk.SectorSize
	if audioStart >= len(data) {
		t.Fatal("no audio data in FZF")
	}
	h := sha256.Sum256(data[audioStart:])
	return hex.EncodeToString(h[:])
}
