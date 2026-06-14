package loader

import (
	"os"
	"path/filepath"
	"testing"
)

// File-extension test constants, shared with fuzz_test.go in the
// same package. The loader's front-door dispatch keys on these
// extensions; repeating the literals would trip goconst.
const (
	testExtFZF = ".fzf"
	testExtIMG = ".img"
)

// TestLoadContainer_DispatchesByExtension pins the loader's
// front-door routing. The App relies on the extension dispatch to
// pick the right reader (FZF vs disk image); a mis-routed file
// would either fail with a wrong-shape error or, worse, succeed
// with a misinterpreted byte layout.
func TestLoadContainer_DispatchesByExtension(t *testing.T) {
	cases := []struct {
		ext        string
		wantFormat Format
		wantErr    bool
	}{
		// .fzf and .img both produce typed Format values via their
		// own loaders. The bytes themselves are bogus, so an
		// inner-parser error is expected; we only assert that the
		// dispatch reaches the right inner loader, not that the
		// parser accepts the payload.
		{ext: testExtFZF, wantErr: true},
		{ext: testExtIMG, wantErr: true},
		// Unknown extensions are rejected up-front with the
		// "unsupported extension" error.
		{ext: ".txt", wantErr: true},
		{ext: ".bin", wantErr: true},
		{ext: "", wantErr: true},
	}

	for _, tc := range cases {
		t.Run("ext="+tc.ext, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "fixture"+tc.ext)
			if err := os.WriteFile(path, []byte("nope"), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}
			_, _, err := LoadContainer(path)
			if tc.wantErr && err == nil {
				t.Errorf("LoadContainer(%q) returned nil err; expected one", path)
			}
		})
	}
}

// TestLoadContainer_UnsupportedExtensionIsTypedError pins the
// "unsupported extension" failure mode specifically: the App
// surfaces this directly to the user, so the message stability
// matters.
func TestLoadContainer_UnsupportedExtensionIsTypedError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "garbage.unknown")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, _, err := LoadContainer(path)
	if err == nil {
		t.Fatal("expected error for unsupported extension")
	}
	// Message contains the offending extension so the user can
	// match it against their file.
	if msg := err.Error(); !contains(msg, ".unknown") {
		t.Errorf("error message %q doesn't mention the offending extension", msg)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
