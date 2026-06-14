package loader

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzLoadContainer drives LoadContainer with arbitrary bytes saved
// under .fzf and .img extensions. The contract is: never panic.
// Any malformed input must come back as a typed error (or empty
// ContainerInfo); never a runtime panic that would crash the App.
//
// Seed corpus covers the obvious cliff edges: empty file, single
// byte, partial header. Go's fuzzer extends from there.
func FuzzLoadContainer(f *testing.F) {
	f.Add([]byte{}, testExtFZF)
	f.Add([]byte{0}, testExtFZF)
	f.Add([]byte{0xFF, 0xFF, 0xFF}, testExtFZF)
	f.Add(make([]byte, 16), testExtFZF)
	f.Add(make([]byte, 1024), testExtFZF)
	f.Add([]byte{0}, testExtIMG)
	f.Add(make([]byte, 1024), testExtIMG)
	// A near-valid-looking FZF header but truncated short.
	f.Add(append([]byte{0x01, 0x00}, make([]byte, 200)...), testExtFZF)
	// Extension cases.
	f.Add([]byte("hello"), ".txt")
	f.Add([]byte("hello"), "")

	f.Fuzz(func(t *testing.T, data []byte, ext string) {
		// Constrain ext to a small set of meaningful values, prefixed
		// with "." so the path's extension lookup behaves as in
		// production. Anything else is treated as the empty ext (which
		// LoadContainer rejects with an unsupported-extension error).
		switch ext {
		case testExtFZF, testExtIMG, ".txt", "":
			// pass
		default:
			ext = testExtFZF
		}

		dir := t.TempDir()
		target := filepath.Join(dir, "f"+ext)
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatalf("write fuzz input: %v", err)
		}

		// The whole point: LoadContainer must never panic, regardless
		// of input shape. We don't care whether it returns an error or
		// a valid container; only that it returns.
		_, _, _ = LoadContainer(target)
	})
}
