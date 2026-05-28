package fzvinfo

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
)

// FuzzParseFZV feeds arbitrary bytes to Parse and Render/RenderJSON. The
// goal is to surface header-parser bugs (off-by-one indexing, unhandled
// envelope shapes, etc.) before they reach users.
func FuzzParseFZV(f *testing.F) {
	f.Add(testutil.MakeTestVoice("A", 100))
	f.Add(testutil.MakeTestVoice("LONGNAME", 500))
	f.Add(make([]byte, disk.SectorSize))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		path := filepath.Join(dir, "fuzz.fzv")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		params, err := Parse(path)
		if err != nil {
			return
		}
		if params == nil {
			t.Fatal("Parse returned nil params with nil error")
		}
		if params.SampleRate == 0 {
			t.Fatal("SampleRate is zero after successful Parse")
		}
		if params.KeyLow > params.KeyHigh {
			t.Fatalf("KeyLow=%d > KeyHigh=%d", params.KeyLow, params.KeyHigh)
		}
		if params.DCASustain >= disk.MaxGenerators+1 {
			t.Fatalf("DCASustain=%d outside valid range", params.DCASustain)
		}
		if params.DCFSustain >= disk.MaxGenerators+1 {
			t.Fatalf("DCFSustain=%d outside valid range", params.DCFSustain)
		}
		var buf bytes.Buffer
		Render(&buf, params)
		buf.Reset()
		if err := RenderJSON(&buf, params); err != nil {
			t.Fatalf("RenderJSON failed: %v", err)
		}
	})
}
