package sfz

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// mapSFZ builds an in-memory filesystem from name to content pairs.
func mapSFZ(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, content := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return fsys
}

func TestParseFSMatchesParse(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"main.sfz": `#include "extra/more.sfz"
<control> default_path=samples/
<group> lovel=1 hivel=64
<region> sample=kick.wav lokey=36 hikey=40 pitch_keycenter=38 transpose=2 tune=-10
<region> sample=sub/snare.wav key=42 mutegroup=1
`,
		"extra/more.sfz": `<region>
sample=../samples/hat.wav lokey=44 hikey=44 pitch_keycenter=44 loop_mode=one_shot
`,
	}

	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	pathRegions, pathWarns, err := Parse(filepath.Join(dir, "main.sfz"))
	if err != nil {
		t.Fatal(err)
	}
	fsRegions, fsWarns, err := ParseFS(mapSFZ(files), "main.sfz")
	if err != nil {
		t.Fatal(err)
	}

	if len(fsRegions) != len(pathRegions) {
		t.Fatalf("region counts differ: fs %d, path %d", len(fsRegions), len(pathRegions))
	}
	for i := range pathRegions {
		want := pathRegions[i]
		got := fsRegions[i]

		// Path mode resolves samples to absolute paths under dir; fs mode
		// keeps them relative to the fs root. Compare after stripping.
		rel, err := filepath.Rel(dir, want.Sample)
		if err != nil {
			t.Fatal(err)
		}
		if got.Sample != filepath.ToSlash(rel) {
			t.Errorf("region %d sample: fs %q, path-relative %q", i, got.Sample, filepath.ToSlash(rel))
		}
		want.Sample = ""
		got.Sample = ""
		if got != want {
			t.Errorf("region %d differs: fs %+v, path %+v", i, got, want)
		}
	}
	if len(fsWarns) != len(pathWarns) {
		t.Fatalf("warning counts differ: fs %v, path %v", fsWarns, pathWarns)
	}
}

func TestParseFSResolvesIncludeAndDefaultPath(t *testing.T) {
	t.Parallel()
	fsys := mapSFZ(map[string]string{
		"kit/main.sfz": `#include "regions.sfz"`,
		"kit/regions.sfz": `<control> default_path=../wavs
<region> sample=kick.wav lokey=36 hikey=36 pitch_keycenter=36
`,
	})
	regions, _, err := ParseFS(fsys, "kit/main.sfz")
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(regions))
	}
	if regions[0].Sample != "wavs/kick.wav" {
		t.Errorf("sample = %q, want wavs/kick.wav", regions[0].Sample)
	}
}

func TestParseFSEscapeWarns(t *testing.T) {
	t.Parallel()
	fsys := mapSFZ(map[string]string{
		"main.sfz": `<region> sample=../outside.wav lokey=36 hikey=36 pitch_keycenter=36
`,
	})
	regions, warns, err := ParseFS(fsys, "main.sfz")
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(regions))
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w.Message, "outside the SFZ root") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an escape warning, got %v", warns)
	}
}

func TestParseFSIncludeCycleWarns(t *testing.T) {
	t.Parallel()
	fsys := mapSFZ(map[string]string{
		"a.sfz": `#include "b.sfz"
<region> sample=kick.wav lokey=36 hikey=36 pitch_keycenter=36
`,
		"b.sfz": `#include "a.sfz"`,
	})
	_, warns, err := ParseFS(fsys, "a.sfz")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w.Message, "repeated #include") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an include-cycle warning, got %v", warns)
	}
}

func TestParseFSMissingFile(t *testing.T) {
	t.Parallel()
	_, _, err := ParseFS(mapSFZ(nil), "absent.sfz")
	if err == nil {
		t.Fatal("expected an error for a missing SFZ file")
	}
	if !strings.Contains(err.Error(), "absent.sfz") {
		t.Errorf("error should name the file: %v", err)
	}
}

// A hostile SFZ must be rejected as it parses, not after allocating a
// region for every line: the cap bounds the work, not just the result.
func TestParseFSRejectsRegionFloodWithoutHoardingThem(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i < 200_000; i++ {
		b.WriteString("<region> sample=s.wav lokey=36 hikey=36 pitch_keycenter=36\n")
	}
	fsys := mapSFZ(map[string]string{"flood.sfz": b.String()})

	regions, warns, err := ParseFS(fsys, "flood.sfz")
	if err == nil {
		t.Fatal("expected an error for a region flood")
	}
	if regions != nil {
		t.Error("a rejected parse returns no regions")
	}
	if len(warns) > maxWarnings {
		t.Errorf("warnings = %d, want at most %d", len(warns), maxWarnings)
	}
}
