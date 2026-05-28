package diskformat

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

// FuzzFormat feeds arbitrary label strings to Format and asserts that any
// label the formatter accepts produces a disk image of exactly ImageSize
// bytes whose Directory() decodes without error and whose label trims back
// to a value derivable from the input.
func FuzzFormat(f *testing.F) {
	f.Add("OK")
	f.Add("HOOVER")
	f.Add("EXACTLY12CHR")
	f.Add("TOO LONG TO FIT IN 12 CHARS")
	f.Add("WITH SPACES")
	f.Add("")
	f.Add("\x01\x02\x03")

	f.Fuzz(func(t *testing.T, label string) {
		dir := t.TempDir()
		path := filepath.Join(dir, "fuzz.img")
		if err := Format(path, label); err != nil {
			return
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat after Format: %v", err)
		}
		if info.Size() != int64(disk.ImageSize) {
			t.Fatalf("image size %d != %d", info.Size(), disk.ImageSize)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		img, err := disk.ReadImage(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("ReadImage of formatted disk: %v", err)
		}
		if _, err := img.Directory(); err != nil {
			t.Fatalf("Directory of formatted disk: %v", err)
		}
		got := disk.TrimPadded(data[disk.LabelOffset : disk.LabelOffset+disk.LabelSize])
		// Truncate input to LabelSize then trim trailing spaces, mirroring
		// Format's documented behaviour.
		want := label
		if len(want) > disk.LabelSize {
			want = want[:disk.LabelSize]
		}
		for len(want) > 0 && want[len(want)-1] == ' ' {
			want = want[:len(want)-1]
		}
		if got != want {
			t.Errorf("on-disk label = %q, want %q (from input %q)", got, want, label)
		}
	})
}
