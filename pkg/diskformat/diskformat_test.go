package diskformat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

func TestFormat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.img")

	if err := Format(path, "TESTLABEL"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != disk.ImageSize {
		t.Errorf("image size: got %d, want %d", info.Size(), disk.ImageSize)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Label at 0x000.
	want := disk.PadLabel("TESTLABEL")
	if [disk.LabelSize]byte(data[0:disk.LabelSize]) != want {
		t.Errorf("label mismatch at 0x000")
	}

	if data[disk.DiskNameTagOffset] != disk.DiskNameTag {
		t.Errorf("name tag: got 0x%02x, want 0x%02x", data[disk.DiskNameTagOffset], disk.DiskNameTag)
	}

	if [disk.LabelSize]byte(data[disk.PasswordOffset:disk.PasswordOffset+disk.LabelSize]) != want {
		t.Errorf("label copy mismatch at 0x%03x", disk.PasswordOffset)
	}

	// CAT byte 0 = 0x03 (clusters 0 and 1 allocated).
	if data[disk.CATOffset] != 0x03 {
		t.Errorf("CAT[0] = 0x%02x, want 0x03", data[disk.CATOffset])
	}

	// CAT byte 1 = 0x00 (clusters 8-15 free).
	if data[disk.CATOffset+1] != 0x00 {
		t.Errorf("CAT[1] = 0x%02x, want 0x00", data[disk.CATOffset+1])
	}

	// Beyond-physical region at 0x120 is all 0xff.
	if data[disk.CATPhysicalEnd] != 0xff {
		t.Errorf("beyond-physical CAT byte = 0x%02x, want 0xff", data[disk.CATPhysicalEnd])
	}

	// Sector 1 (directory) is all zero.
	for i, b := range data[disk.SectorSize : 2*disk.SectorSize] {
		if b != 0 {
			t.Errorf("directory sector byte %d = 0x%02x, want 0x00", i, b)
			break
		}
	}

	// Data sectors filled with 'Z'.
	if data[2*disk.SectorSize] != 'Z' {
		t.Errorf("data sector byte = 0x%02x, want 'Z'", data[2*disk.SectorSize])
	}
}

func TestBuildImageLabel(t *testing.T) {
	t.Parallel()
	img := buildImage("HOOVER")
	want := disk.PadLabel("HOOVER")
	if [disk.LabelSize]byte(img[0:disk.LabelSize]) != want {
		t.Error("label not set correctly in sector 0")
	}
}

func TestFormatRejectsDirectoryPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := Format(dir, "LABEL")
	if err == nil {
		t.Fatal("expected error when IMAGE path is a directory")
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("error should name the cause; got: %v", err)
	}
}

func TestFormatRejectsEmptyLabel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.img")

	err := Format(path, "")
	if err == nil {
		t.Fatal("expected error for empty label")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFormatRejectsUnicodeLabel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "emoji.img")

	err := Format(path, "DRUMS\U0001F3B5")
	if err == nil {
		t.Fatal("expected error for unicode label")
	}
	if !strings.Contains(err.Error(), "non-ASCII") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFormatRejectsControlChars(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ctrl.img")

	err := Format(path, "DRUMS\x01")
	if err == nil {
		t.Fatal("expected error for control character in label")
	}
	if !strings.Contains(err.Error(), "non-ASCII") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFormatMaxLengthLabel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "max.img")

	if err := Format(path, "ABCDEFGHIJKL"); err != nil {
		t.Fatal(err)
	}
}

func TestFormatOverlengthLabel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "long.img")

	if err := Format(path, "ABCDEFGHIJKLMNOP"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	want := disk.PadLabel("ABCDEFGHIJKL")
	if [disk.LabelSize]byte(data[0:disk.LabelSize]) != want {
		t.Errorf("label not truncated: got %q, want %q", data[0:disk.LabelSize], want)
	}
}

func TestFormatReadImageRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "roundtrip.img")
	if err := Format(path, "ROUND TRIP"); err != nil {
		t.Fatalf("Format: %v", err)
	}
	img, err := disk.OpenImage(path)
	if err != nil {
		t.Fatalf("OpenImage: %v", err)
	}
	if got := img.Label(); got != "ROUND TRIP" {
		t.Errorf("label: got %q, want %q", got, "ROUND TRIP")
	}
	free := img.FreeSectors()
	if free != disk.SectorCount-disk.ReservedSectors {
		t.Errorf("free sectors: got %d, want %d", free, disk.SectorCount-disk.ReservedSectors)
	}
}
