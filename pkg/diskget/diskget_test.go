package diskget

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
)

func TestGetExistingFile(t *testing.T) {
	t.Parallel()
	imgPath := testutil.MakeTestDisk(t, "TEST", "MYKICK")
	dir := t.TempDir()
	out := filepath.Join(dir, "out.fzv")

	if err := Get(imgPath, "MYKICK", out); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Error("output file is empty")
	}
}

func TestGetRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	fzvPath := filepath.Join(dir, "voice.fzv")

	fzv := make([]byte, disk.SectorSize*3)
	copy(fzv[disk.VoiceNameOffset:], "HOOVER      ")
	for i := disk.SectorSize; i < len(fzv); i++ {
		fzv[i] = byte(i) //nolint:gosec // G115: test data fill, i < sector size
	}

	if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
		t.Fatal(err)
	}
	if err := diskformat.Format(imgPath, "RT"); err != nil {
		t.Fatal(err)
	}
	if err := diskadd.Add(imgPath, fzvPath, 0); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "out.fzv")
	if err := Get(imgPath, "HOOVER", out); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < len(fzv) {
		t.Fatalf("extracted size %d < original size %d", len(got), len(fzv))
	}
	for i := range len(fzv) {
		if got[i] != fzv[i] {
			t.Errorf("byte %d: got 0x%02x, want 0x%02x", i, got[i], fzv[i])
			break
		}
	}
}

func TestGetNotFound(t *testing.T) {
	t.Parallel()
	imgPath := testutil.MakeTestDisk(t, "TEST", "MYKICK")
	err := Get(imgPath, "NOSUCHFILE", filepath.Join(t.TempDir(), "out.fzv"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, disk.ErrNotFound) {
		t.Errorf("expected error to wrap disk.ErrNotFound, got %v", err)
	}
}

func TestGetCaseInsensitive(t *testing.T) {
	t.Parallel()
	imgPath := testutil.MakeTestDisk(t, "TEST", "MYKICK")
	out := filepath.Join(t.TempDir(), "out.fzv")
	if err := Get(imgPath, "mykick", out); err != nil {
		t.Errorf("expected case-insensitive match: %v", err)
	}
}

func TestExtractFileBytesSkipsDIS(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "TEST"); err != nil {
		t.Fatal(err)
	}

	data := make([]byte, 3*disk.SectorSize)
	for i := range data {
		data[i] = byte(i % 199)
	}
	name := disk.PadLabel("TESTFILE")
	if err := diskadd.AddBytes(imgPath, data, name, disk.TypeVoice, 0, 0, 1, 2); err != nil {
		t.Fatal(err)
	}

	img, err := disk.OpenImage(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := img.Directory()
	disSec, _ := img.Sector(int(entries[0].DisSector))
	dis, _ := disk.DecodeDisSector(disSec)

	got, err := extractFileBytes(img, dis, int(entries[0].DisSector))
	if err != nil {
		t.Fatalf("extractFileBytes: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Error("extracted bytes do not match original data")
	}
}

func TestExtractFileBytesEmptyExtents(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "EMPTY"); err != nil {
		t.Fatal(err)
	}
	img, err := disk.OpenImage(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	dis := disk.DisSector{}
	got, err := extractFileBytes(img, dis, 2)
	if err != nil {
		t.Fatalf("extractFileBytes: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result for no extents, got %d bytes", len(got))
	}
}

// TestGetRejectsCorruptDIS covers two corruption modes:
//  1. The directory entry's DIS sector pointer points at sector 0 (reserved
//     for the disk label). Without bounds-checking, Get would happily read
//     the label bytes as a DIS, return garbage extents, and silently emit a
//     bad file. We now refuse before touching the sector.
//  2. The directory entry's DIS sector pointer is valid, but the sector
//     contents themselves point at reserved sectors. DecodeDisSector must
//     surface this via ErrCorruptDIS rather than reading garbage.
func TestGetRejectsCorruptDIS(t *testing.T) {
	t.Parallel()

	t.Run("DisSector pointer in reserved range", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		imgPath := testutil.MakeTestDisk(t, "TEST", "MYKICK")

		// Point entry 0's DisSector field at sector 0.
		raw, err := os.ReadFile(imgPath)
		if err != nil {
			t.Fatal(err)
		}
		dirOff := disk.SectorSize
		raw[dirOff+disk.LabelSize+2] = 0
		raw[dirOff+disk.LabelSize+3] = 0
		if err := os.WriteFile(imgPath, raw, 0644); err != nil { //nolint:gosec // G703: test image path from MakeTestDisk
			t.Fatal(err)
		}

		err = Get(imgPath, "MYKICK", filepath.Join(dir, "out.fzv"))
		if err == nil {
			t.Fatal("expected error for directory entry pointing at reserved sector")
		}
	})

	t.Run("DIS extents in reserved range", func(t *testing.T) {
		t.Parallel()
		imgPath := testutil.MakeTestDisk(t, "TEST", "MYKICK")
		raw, err := os.ReadFile(imgPath)
		if err != nil {
			t.Fatal(err)
		}
		// Read directory entry 0 to find its (valid) DIS sector, then
		// overwrite that DIS sector's first extent so it points at sector 0.
		e, err := disk.DecodeDirEntry(raw[disk.SectorSize : disk.SectorSize+disk.DirEntrySize])
		if err != nil {
			t.Fatal(err)
		}
		disOff := int(e.DisSector) * disk.SectorSize
		// Set extent 0 = (0, 5), invalid: start in reserved range.
		raw[disOff+0], raw[disOff+1] = 0, 0
		raw[disOff+2], raw[disOff+3] = 5, 0
		if err := os.WriteFile(imgPath, raw, 0644); err != nil { //nolint:gosec // G703: test image path from MakeTestDisk
			t.Fatal(err)
		}

		err = Get(imgPath, "MYKICK", filepath.Join(t.TempDir(), "out.fzv"))
		if err == nil {
			t.Fatal("expected error for DIS extent pointing at reserved sector")
		}
		if !errors.Is(err, disk.ErrCorruptDIS) {
			t.Errorf("expected ErrCorruptDIS, got %v", err)
		}
	})
}

func TestGetLargeFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "BIG"); err != nil {
		t.Fatalf("Format: %v", err)
	}

	data := make([]byte, 10*disk.SectorSize)
	for i := range data {
		data[i] = byte(i % 251)
	}
	name := disk.PadLabel("BIGFILE")
	if err := diskadd.AddBytes(imgPath, data, name, disk.TypeVoice, 0, 0, 1, 9); err != nil {
		t.Fatalf("AddBytes: %v", err)
	}

	outPath := filepath.Join(dir, "got.bin")
	if err := Get(imgPath, "BIGFILE", outPath); err != nil {
		t.Fatalf("Get: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Error("extracted data does not match original")
	}
}
