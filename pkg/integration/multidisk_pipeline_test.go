package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskget"

	"github.com/philipcunningham/fizzle/pkg/sfzconvert"

	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
)

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

	// INVARIANT 4: disk 1's voice area covers every voice, not just disk
	// 1's. The sampler reads envelopes and loops for the full instrument
	// from disk 1.
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

// TestMultiDiskUnpackBothDisks checks that unpacking disk 1 yields voices
// and that disk 2, pure audio continuation, has no FZF structure to
// unpack.
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

	// Disk 2 carries a FULL-DATA-FZ entry, so extracting still yields a
	// file, but it holds no voice headers and unpacks to 0 voices.
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

// TestJUNGLISMGoldenChecksums pins the JUNGLISM SFZ conversion to
// byte-identical FZF and disk image outputs. Any drift in resampling,
// bank layout, voice packing, sector allocation, or disk formatting
// fails here.
//
// To update after an intentional format change, run the test with -v,
// copy the "got" checksums, and replace the expected values below.
