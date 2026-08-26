package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/disklist"

	"github.com/philipcunningham/fizzle/pkg/voiceextract"

	voiceimport "github.com/philipcunningham/fizzle/pkg/voiceimport"
)

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

	fzvData := extractFirstVoiceData(t, hooverImg)

	origRate, origSamples, err := voiceextract.Decode(fzvData)
	if err != nil {
		t.Fatal(err)
	}

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

	reimportedData, err := os.ReadFile(reimportedFZV)
	if err != nil {
		t.Fatal(err)
	}
	_, reimportedSamples, err := voiceextract.Decode(reimportedData)
	if err != nil {
		t.Fatal(err)
	}

	// Importing at the source rate skips resampling, so counts match.
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

	origEntries := readEntries(t, hooverImg)
	if len(origEntries) == 0 {
		t.Fatal("HOOVER.img has no directory entries")
	}
	origEntry := origEntries[0]

	fzvData := extractFirstVoiceData(t, hooverImg)
	fzvPath := filepath.Join(dir, "voice.fzv")
	if err := os.WriteFile(fzvPath, fzvData, 0644); err != nil {
		t.Fatal(err)
	}

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

	// The extent's first sector is the DIS sector itself; the voice header
	// and audio start at the next one.
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

// TestSFZFullPipeline drives the whole workflow: SFZ to FZF to disk
// image, then disk get, unpack, and extract WAV. It converts JUNGLISM.sfz
// at 9kHz, which fits one disk, and checks audio fidelity across sector
// boundaries.
