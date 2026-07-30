package diskformat

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/logger"
)

// captureLog redirects the global logger to a buffer for the duration
// of one test. pkg/internal/testutil offers the same helper, but it
// imports diskformat, so this package rolls its own.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	oldLogger := log.Logger
	oldLevel := zerolog.GlobalLevel()
	logger.InitWithWriter(false, &buf)
	t.Cleanup(func() {
		log.Logger = oldLogger
		zerolog.SetGlobalLevel(oldLevel)
	})
	return &buf
}

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

// A label longer than the 12-byte field, whose only non-ASCII
// character sits past the cut, must still be rejected. Validating
// after the truncation would accept it and write a disk.
func TestFormatRejectsUnicodeLabelPastTruncation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "long-unicode.img")

	err := Format(path, "DRUM KIT ONE (café)")
	if err == nil {
		t.Fatal("expected error for a non-ASCII character past the 12-byte cut")
	}
	if !strings.Contains(err.Error(), "non-ASCII") {
		t.Errorf("unexpected error: %v", err)
	}
	// The offending character is named, not the replacement rune a
	// mid-character cut would leave behind.
	if !strings.Contains(err.Error(), `"é"`) {
		t.Errorf("error should name the offending character; got: %v", err)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("a rejected label still wrote an image")
	}
}

// A cut that lands inside a multi-byte character must report that
// character, not the replacement rune the split produces.
func TestFormatRejectsLabelSplitMidCharacter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "split.img")

	// "é" starts at byte 11, so a 12-byte cut lands inside it.
	err := Format(path, "DRUM KIT ONécafe")
	if err == nil {
		t.Fatal("expected error for unicode label")
	}
	if !strings.Contains(err.Error(), `"é"`) {
		t.Errorf("error should name the offending character; got: %v", err)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("a rejected label still wrote an image")
	}
}

// A rejected label logs nothing: validation runs before the info
// line, so a failure produces one message rather than two.
func TestFormatDoesNotLogBeforeRejecting(t *testing.T) {
	buf := captureLog(t)
	path := filepath.Join(t.TempDir(), "quiet.img")

	if err := Format(path, "café"); err == nil {
		t.Fatal("expected error for unicode label")
	}
	if strings.Contains(buf.String(), "creating disk image") {
		t.Errorf("a rejected label logged a stray line: %q", buf.String())
	}
}

// The empty-label check runs before the directory check, so a caller
// that gets both wrong hears about the label first.
func TestFormatRejectsEmptyLabelBeforeDirectoryPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := Format(dir, "")
	if err == nil {
		t.Fatal("expected error for empty label")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
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

// BuildImage is the pure entry point the web core calls: same image
// bytes as Format, no filesystem, and strict validation (the web
// boundary rejects an over-length label instead of truncating).
func TestBuildImageMatchesFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.img")
	if err := Format(path, "TECHNO"); err != nil {
		t.Fatalf("Format: %v", err)
	}
	fromFile, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	built, err := BuildImage("TECHNO")
	if err != nil {
		t.Fatalf("BuildImage: %v", err)
	}
	if !bytes.Equal(fromFile, built) {
		t.Fatal("BuildImage bytes differ from Format output")
	}
}

func TestBuildImageRejectsBadLabels(t *testing.T) {
	cases := map[string]string{
		"empty":       "",
		"over length": "THIRTEEN CHRS",
		"non ASCII":   "café",
		"control":     "AB\tCD",
	}
	for name, label := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildImage(label); err == nil {
				t.Fatalf("BuildImage(%q) accepted a bad label", label)
			}
		})
	}
}

func TestBuildImageParses(t *testing.T) {
	built, err := BuildImage("MY DISK")
	if err != nil {
		t.Fatalf("BuildImage: %v", err)
	}
	img, err := disk.ReadImage(bytes.NewReader(built))
	if err != nil {
		t.Fatalf("ReadImage rejected a fresh image: %v", err)
	}
	if got := img.Label(); got != "MY DISK" {
		t.Fatalf("label = %q, want MY DISK", got)
	}
}
