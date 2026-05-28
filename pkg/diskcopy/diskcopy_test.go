package diskcopy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/disklist"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
)

func TestCopySuccess(t *testing.T) {
	t.Parallel()
	srcImg := testutil.MakeTestDisk(t, "SRC", "MYKICK")
	dstDir := t.TempDir()
	dstImg := filepath.Join(dstDir, "dst.img")
	if err := diskformat.Format(dstImg, "DST"); err != nil {
		t.Fatal(err)
	}

	if err := Copy(srcImg, "MYKICK", dstImg); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	outPath := filepath.Join(t.TempDir(), "out.fzv")
	if err := diskget.Get(dstImg, "MYKICK", outPath); err != nil {
		t.Fatalf("voice not found on destination disk: %v", err)
	}
}

func TestCopyCaseInsensitive(t *testing.T) {
	t.Parallel()
	srcImg := testutil.MakeTestDisk(t, "SRC", "MYKICK")
	dstDir := t.TempDir()
	dstImg := filepath.Join(dstDir, "dst.img")
	if err := diskformat.Format(dstImg, "DST"); err != nil {
		t.Fatal(err)
	}

	if err := Copy(srcImg, "mykick", dstImg); err != nil {
		t.Fatalf("Copy with lowercase name: %v", err)
	}
}

func TestCopyMissingName(t *testing.T) {
	t.Parallel()
	srcImg := testutil.MakeTestDisk(t, "SRC", "MYKICK")
	dstDir := t.TempDir()
	dstImg := filepath.Join(dstDir, "dst.img")
	if err := diskformat.Format(dstImg, "DST"); err != nil {
		t.Fatal(err)
	}

	err := Copy(srcImg, "NOSUCHFILE", dstImg)
	if err == nil {
		t.Error("expected error for missing file name")
	}
}

func TestCopyMissingDestination(t *testing.T) {
	t.Parallel()
	srcImg := testutil.MakeTestDisk(t, "SRC", "MYKICK")
	err := Copy(srcImg, "MYKICK", filepath.Join(t.TempDir(), "nonexistent", "dst.img"))
	if err == nil {
		t.Error("expected error for missing destination image")
	}
}

func TestCopyMissingSource(t *testing.T) {
	t.Parallel()
	dstDir := t.TempDir()
	dstImg := filepath.Join(dstDir, "dst.img")
	if err := diskformat.Format(dstImg, "DST"); err != nil {
		t.Fatal(err)
	}

	err := Copy(filepath.Join(t.TempDir(), "nope.img"), "VOICE", dstImg)
	if err == nil {
		t.Error("expected error for missing source image")
	}
}

func TestCopyDestinationFull(t *testing.T) {
	t.Parallel()
	srcImg := testutil.MakeTestDisk(t, "SRC", "MYKICK")

	dstDir := t.TempDir()
	dstImg := filepath.Join(dstDir, "dst.img")
	if err := diskformat.Format(dstImg, "DST"); err != nil {
		t.Fatal(err)
	}

	big := make([]byte, disk.UsableDataSize-disk.SectorSize)
	padded := disk.PadLabel("BIGFILE")
	copy(big[disk.VoiceNameOffset:], padded[:])
	bigPath := filepath.Join(dstDir, "big.fzv")
	if err := os.WriteFile(bigPath, big, 0644); err != nil {
		t.Fatal(err)
	}
	if err := diskadd.Add(dstImg, bigPath, 0); err != nil {
		t.Fatal(err)
	}

	err := Copy(srcImg, "MYKICK", dstImg)
	if err == nil {
		t.Fatal("expected error when copying to a full destination disk")
	}
}

func TestCopyPreservesExistingFiles(t *testing.T) {
	t.Parallel()
	srcImg := testutil.MakeTestDisk(t, "SRC", "MYKICK")
	dstImg := testutil.MakeTestDisk(t, "DST", "EXISTING")

	if err := Copy(srcImg, "MYKICK", dstImg); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	listing, err := disklist.Parse(dstImg)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	names := make(map[string]bool)
	for _, f := range listing.Entries {
		names[f.Name] = true
	}
	if !names["EXISTING"] {
		t.Error("existing file was removed after copy")
	}
	if !names["MYKICK"] {
		t.Error("copied file not found on destination disk")
	}
}
