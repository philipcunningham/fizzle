package disklist

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
)

const (
	testVoiceKick  = "KICK"
	testVoiceSnare = "SNARE"
)

func TestListEmptyDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.img")

	if err := diskformat.Format(path, "MYTEST"); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := List(path, &buf); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "MYTEST") {
		t.Errorf("output missing disk label: %s", out)
	}
	if !strings.Contains(out, "(empty)") {
		t.Errorf("expected (empty) for blank disk: %s", out)
	}
}

func TestListMissingFile(t *testing.T) {
	t.Parallel()
	err := List(filepath.Join(t.TempDir(), "nonexistent.img"), &bytes.Buffer{})
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestListWrongSize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.img")
	os.WriteFile(path, []byte("not an image"), 0644) //nolint:errcheck
	err := List(path, &bytes.Buffer{})
	if err == nil {
		t.Error("expected error for wrong-size image")
	}
}

func TestListMultipleEntries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "multi.img")
	if err := diskformat.Format(imgPath, "MULTI"); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{testVoiceKick, testVoiceSnare} {
		fzv := make([]byte, disk.SectorSize+512*2)
		padded := disk.PadLabel(name)
		copy(fzv[disk.VoiceNameOffset:], padded[:])
		fzvPath := filepath.Join(dir, name+".fzv")
		if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
			t.Fatal(err)
		}
		if err := diskadd.Add(imgPath, fzvPath, 0); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	if err := List(imgPath, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, testVoiceKick) {
		t.Error("expected KICK in output")
	}
	if !strings.Contains(out, testVoiceSnare) {
		t.Errorf("output missing SNARE:\n%s", out)
	}
}

func TestListShowsHumanReadableSize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "SZTEST"); err != nil {
		t.Fatal(err)
	}

	// Build a voice file of known size: header + 1024 samples = 1024 + 2048 = 3072 bytes.
	nSamples := 1024
	fzv := make([]byte, disk.SectorSize+nSamples*2)
	copy(fzv[disk.VoiceNameOffset:], "SIZETEST    ")
	fzvPath := filepath.Join(dir, "test.fzv")
	if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
		t.Fatal(err)
	}
	if err := diskadd.Add(imgPath, fzvPath, 0); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := List(imgPath, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	// Size should be shown in KB (3 KB), not raw bytes.
	if !strings.Contains(out, "KB") && !strings.Contains(out, "MB") {
		t.Errorf("expected human-readable size (KB/MB) in output:\n%s", out)
	}
	// Should NOT show raw byte count like "3072 bytes".
	if strings.Contains(out, "3072 bytes") {
		t.Errorf("should not show raw byte count:\n%s", out)
	}

	// Free space footer should be present.
	if !strings.Contains(out, "free") {
		t.Errorf("expected free space in output:\n%s", out)
	}

	// Free space should be close to full disk size (one tiny file added).
	if !strings.Contains(out, "1.2 MB") && !strings.Contains(out, "1.3 MB") {
		t.Errorf("free space line: %s", out)
	}
}

func TestParseEmptyDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.img")
	if err := diskformat.Format(path, "MYTEST"); err != nil {
		t.Fatal(err)
	}

	listing, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if listing.Label != "MYTEST" {
		t.Errorf("label = %q, want MYTEST", listing.Label)
	}
	if len(listing.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(listing.Entries))
	}
	if listing.FreeBytes <= 0 {
		t.Errorf("expected positive FreeBytes, got %d", listing.FreeBytes)
	}
}

func TestParseMultipleEntries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "multi.img")
	if err := diskformat.Format(imgPath, "MULTI"); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{testVoiceKick, testVoiceSnare} {
		fzv := make([]byte, disk.SectorSize+512*2)
		padded := disk.PadLabel(name)
		copy(fzv[disk.VoiceNameOffset:], padded[:])
		fzvPath := filepath.Join(dir, name+".fzv")
		if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
			t.Fatal(err)
		}
		if err := diskadd.Add(imgPath, fzvPath, 0); err != nil {
			t.Fatal(err)
		}
	}

	listing, err := Parse(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(listing.Entries))
	}
	if listing.Entries[0].Name != testVoiceKick {
		t.Errorf("entry 0 name = %q, want KICK", listing.Entries[0].Name)
	}
	if listing.Entries[1].Name != testVoiceSnare {
		t.Errorf("entry 1 name = %q, want SNARE", listing.Entries[1].Name)
	}
}

func TestParseFreeSpace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "SZTEST"); err != nil {
		t.Fatal(err)
	}

	listing, err := Parse(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	if listing.TotalBytes <= 0 {
		t.Errorf("expected positive TotalBytes, got %d", listing.TotalBytes)
	}
	if listing.FreeBytes > listing.TotalBytes {
		t.Errorf("FreeBytes %d > TotalBytes %d", listing.FreeBytes, listing.TotalBytes)
	}
	if listing.UsedPct < 0 || listing.UsedPct > 100 {
		t.Errorf("UsedPct = %d, want 0-100", listing.UsedPct)
	}
}

func TestRenderJSONEmptyDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "empty.img")
	if err := diskformat.Format(imgPath, "EMPTY"); err != nil {
		t.Fatal(err)
	}

	listing, err := Parse(imgPath)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RenderJSON(&buf, listing); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, `"label": "EMPTY"`) {
		t.Errorf("expected label in JSON:\n%s", out)
	}
	if !strings.Contains(out, `"entries": null`) && !strings.Contains(out, `"entries": []`) {
		t.Errorf("expected null or empty entries in JSON:\n%s", out)
	}
	if !strings.Contains(out, `"free_bytes"`) {
		t.Errorf("expected free_bytes key in JSON:\n%s", out)
	}
}

func TestRenderJSONWithEntries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "JSON"); err != nil {
		t.Fatal(err)
	}

	fzv := make([]byte, disk.SectorSize+512*2)
	padded := disk.PadLabel(testVoiceKick)
	copy(fzv[disk.VoiceNameOffset:], padded[:])
	fzvPath := filepath.Join(dir, "kick.fzv")
	if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
		t.Fatal(err)
	}
	if err := diskadd.Add(imgPath, fzvPath, 0); err != nil {
		t.Fatal(err)
	}

	listing, err := Parse(imgPath)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RenderJSON(&buf, listing); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, `"name": "`+testVoiceKick+`"`) {
		t.Errorf("expected voice name in JSON:\n%s", out)
	}
	if !strings.Contains(out, `"type": "Voice"`) {
		t.Errorf("expected type in JSON:\n%s", out)
	}
	if !strings.Contains(out, `"size"`) {
		t.Errorf("expected size key in JSON:\n%s", out)
	}
}

// TestParseSkipsCorruptEntry verifies that a single bad directory entry
// (DIS sector pointer out of range, or DIS sector contents undecodable) is
// shown with TypeName="(corrupt)" and Size=0 rather than failing the entire
// listing. `disk ls` is the diagnostic tool used to inspect damaged disks.
func TestParseSkipsCorruptEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "corrupt.img")
	if err := diskformat.Format(imgPath, "CORRUPT"); err != nil {
		t.Fatal(err)
	}

	// Add two real voices so we know the listing has good entries to fall
	// back on after one is corrupted.
	for _, name := range []string{testVoiceKick, testVoiceSnare} {
		fzv := make([]byte, disk.SectorSize+512*2)
		padded := disk.PadLabel(name)
		copy(fzv[disk.VoiceNameOffset:], padded[:])
		fzvPath := filepath.Join(dir, name+".fzv")
		if err := os.WriteFile(fzvPath, fzv, 0644); err != nil {
			t.Fatal(err)
		}
		if err := diskadd.Add(imgPath, fzvPath, 0); err != nil {
			t.Fatal(err)
		}
	}

	// Read the image, point the first directory entry's DIS sector at
	// sector 0 (the reserved label sector) to simulate corruption, and
	// write it back.
	raw, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	// Directory entry 0 lives at offset SectorSize; DisSector is the last
	// 2 bytes of the 16-byte entry.
	dirOff := disk.SectorSize
	raw[dirOff+disk.LabelSize+2] = 0
	raw[dirOff+disk.LabelSize+3] = 0
	if err := os.WriteFile(imgPath, raw, 0644); err != nil { //nolint:gosec // G703: test image path from t.TempDir
		t.Fatal(err)
	}

	listing, err := Parse(imgPath)
	if err != nil {
		t.Fatalf("Parse should not fail on a single corrupt entry: %v", err)
	}
	if len(listing.Entries) != 2 {
		t.Fatalf("expected 2 entries (1 corrupt, 1 good), got %d", len(listing.Entries))
	}
	if listing.Entries[0].TypeName != CorruptTypeName {
		t.Errorf("entry 0 TypeName = %q, want %q", listing.Entries[0].TypeName, CorruptTypeName)
	}
	if listing.Entries[0].Size != 0 {
		t.Errorf("entry 0 Size = %d, want 0 for corrupt entry", listing.Entries[0].Size)
	}
	if listing.Entries[0].Name != testVoiceKick {
		t.Errorf("entry 0 Name = %q, want %q (name should survive corruption)", listing.Entries[0].Name, testVoiceKick)
	}
	if listing.Entries[1].Name != testVoiceSnare {
		t.Errorf("entry 1 Name = %q, want %q (good entry must follow)", listing.Entries[1].Name, testVoiceSnare)
	}
	if listing.Entries[1].TypeName == CorruptTypeName {
		t.Errorf("entry 1 TypeName should not be %q (good entry)", CorruptTypeName)
	}
}

func TestRenderJSONIsValidJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.img")
	if err := diskformat.Format(imgPath, "VALID"); err != nil {
		t.Fatal(err)
	}

	listing, err := Parse(imgPath)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RenderJSON(&buf, listing); err != nil {
		t.Fatal(err)
	}

	var decoded Listing
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON output is not valid: %v\n%s", err, buf.String())
	}
	if decoded.Label != "VALID" {
		t.Errorf("decoded label = %q, want VALID", decoded.Label)
	}
}
