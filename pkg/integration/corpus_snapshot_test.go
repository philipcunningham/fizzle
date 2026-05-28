// Snapshot tests for the bundled test corpora. Each fixture file in
// testdata/corpus/ and testdata/synthetic/ is exercised through a "read"
// command (info / list / parse), the JSON output marshalled, and the
// result compared against a checked-in snapshot. Run with
// `UPDATE_SNAPS=true` to regenerate snapshots after an intentional change.
package integration_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/disklist"
	"github.com/philipcunningham/fizzle/pkg/fzfinfo"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/sfz"
)

const (
	corpusRoot       = "../../testdata/corpus"
	syntheticRoot    = "../../testdata/synthetic"
	corpusSnapDir    = "../../testdata/snapshots/corpus"
	syntheticSnapDir = "../../testdata/snapshots/synthetic"
)

// walkCorpus invokes fn for every corpus file whose extension (case
// insensitive) matches one of exts. Each file runs as a subtest named with
// its corpus-relative path so failures point at the offending fixture.
func walkCorpus(t *testing.T, exts []string, fn func(t *testing.T, relPath, absPath string)) {
	t.Helper()
	wantExt := make(map[string]bool, len(exts))
	for _, e := range exts {
		wantExt[strings.ToLower(e)] = true
	}
	err := filepath.WalkDir(corpusRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !wantExt[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		rel, err := filepath.Rel(corpusRoot, path)
		if err != nil {
			return err
		}
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			fn(t, rel, path)
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking corpus: %v", err)
	}
}

// matchSnapshot writes payload to the given snapshot location, using the
// suffix to keep multiple snapshots-per-fixture (e.g. fzf-info vs disk-ls)
// from colliding. Snapshot directory mirrors the fixture layout.
//
// Absolute paths to bundled testdata are rewritten to a stable
// <TESTDATA>/... placeholder so snapshots remain identical regardless of
// where the repo is checked out (developer workstation vs CI runner).
func matchSnapshot(t *testing.T, snapRoot, relPath, suffix string, payload []byte) {
	t.Helper()
	snaps.WithConfig(
		snaps.Dir(filepath.Join(snapRoot, filepath.Dir(relPath))),
		snaps.Filename(filepath.Base(relPath)+suffix),
		snaps.Ext(".json"),
	).MatchStandaloneJSON(t, normaliseTestdataPaths(payload))
}

// normaliseTestdataPaths replaces absolute paths pointing into the bundled
// testdata directory with a stable <TESTDATA>/... placeholder. Without this,
// snapshots that capture resolved file paths (e.g. SFZ sample= opcodes) drift
// between checkouts and CI fails on path mismatches alone.
func normaliseTestdataPaths(payload []byte) []byte {
	absTestdata, err := filepath.Abs("../../testdata")
	if err != nil {
		return payload
	}
	return []byte(strings.ReplaceAll(string(payload), filepath.ToSlash(absTestdata), "<TESTDATA>"))
}

func TestCorpusFZFInfoSnapshots(t *testing.T) {
	skipShort(t)
	walkCorpus(t, []string{".fzf"}, func(t *testing.T, rel, abs string) {
		info, err := fzfinfo.Parse(abs)
		if err != nil {
			t.Fatalf("fzfinfo.Parse(%s): %v", rel, err)
		}
		var buf bytes.Buffer
		if err := fzfinfo.RenderJSON(&buf, info); err != nil {
			t.Fatalf("RenderJSON: %v", err)
		}
		matchSnapshot(t, corpusSnapDir, rel, ".fzf-info", buf.Bytes())
	})
}

func TestCorpusFZVInfoSnapshots(t *testing.T) {
	skipShort(t)
	walkCorpus(t, []string{".fzv"}, func(t *testing.T, rel, abs string) {
		info, err := fzvinfo.Parse(abs)
		if err != nil {
			t.Fatalf("fzvinfo.Parse(%s): %v", rel, err)
		}
		var buf bytes.Buffer
		if err := fzvinfo.RenderJSON(&buf, info); err != nil {
			t.Fatalf("RenderJSON: %v", err)
		}
		matchSnapshot(t, corpusSnapDir, rel, ".fzv-info", buf.Bytes())
	})
}

// syntheticImages is the curated list of disk-image fixtures under
// testdata/synthetic/. Hand-listed rather than walked because the dir also
// holds a non-image SFZ fixture exercised by a separate test below.
const typeLabelVoice = "Voice"

var syntheticImages = []string{
	"BRASS.img",
	"HOOVER.img",
	"PAD-LFO.img",
	"STAB.img",
	"TECHNO.img",
}

func TestSyntheticDiskListSnapshots(t *testing.T) {
	skipShort(t)
	t.Parallel()
	for _, name := range syntheticImages {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			listing, err := disklist.Parse(filepath.Join(syntheticRoot, name))
			if err != nil {
				t.Fatalf("disklist.Parse(%s): %v", name, err)
			}
			var buf bytes.Buffer
			if err := disklist.RenderJSON(&buf, listing); err != nil {
				t.Fatalf("RenderJSON: %v", err)
			}
			matchSnapshot(t, syntheticSnapDir, name, ".disk-ls", buf.Bytes())
		})
	}
}

// TestSyntheticDiskContentSnapshots walks each disk-image fixture's
// catalog, extracts every entry, and snapshots the appropriate per-entry
// info output (fzf info for full dumps, fzv info for voices). Catches
// drift in the extract-then-parse pipeline across the synthetic fixtures.
func TestSyntheticDiskContentSnapshots(t *testing.T) {
	skipShort(t)
	t.Parallel()
	for _, name := range syntheticImages {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			imgPath := filepath.Join(syntheticRoot, name)
			listing, err := disklist.Parse(imgPath)
			if err != nil {
				t.Fatalf("disklist.Parse: %v", err)
			}
			for _, entry := range listing.Entries {
				t.Run(entry.Name, func(t *testing.T) {
					t.Parallel()
					snapshotDiskEntry(t, imgPath, name, entry)
				})
			}
		})
	}
}

func snapshotDiskEntry(t *testing.T, imgPath, imgName string, entry disklist.FileEntry) {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), entry.Name+typedExt(entry.TypeName))
	if err := diskget.Get(imgPath, entry.Name, tmp); err != nil {
		t.Fatalf("extract %q from %s: %v", entry.Name, imgName, err)
	}

	var (
		payload []byte
		suffix  string
	)
	switch entry.TypeName {
	case disk.TypeFullDumpLabel:
		info, err := fzfinfo.Parse(tmp)
		if err != nil {
			t.Fatalf("fzfinfo.Parse: %v", err)
		}
		var buf bytes.Buffer
		if err := fzfinfo.RenderJSON(&buf, info); err != nil {
			t.Fatalf("RenderJSON: %v", err)
		}
		payload, suffix = buf.Bytes(), ".fzf-info"
	case typeLabelVoice:
		info, err := fzvinfo.Parse(tmp)
		if err != nil {
			t.Fatalf("fzvinfo.Parse: %v", err)
		}
		var buf bytes.Buffer
		if err := fzvinfo.RenderJSON(&buf, info); err != nil {
			t.Fatalf("RenderJSON: %v", err)
		}
		payload, suffix = buf.Bytes(), ".fzv-info"
	default:
		t.Skipf("no snapshot pipeline for type %q", entry.TypeName)
	}
	matchSnapshot(t, syntheticSnapDir, filepath.Join(imgName, entry.Name), suffix, payload)
}

// TestSyntheticSFZParseSnapshot exercises the SFZ parser end-to-end
// against the JUNGLISM round-trip fixture, snapshotting the parsed
// regions and any warnings.
func TestSyntheticSFZParseSnapshot(t *testing.T) {
	skipShort(t)
	regions, warnings, err := sfz.Parse(filepath.Join(syntheticRoot, "JUNGLISM.sfz"))
	if err != nil {
		t.Fatalf("sfz.Parse: %v", err)
	}
	for i := range regions {
		regions[i].Sample = filepath.ToSlash(regions[i].Sample)
	}
	payload, err := json.Marshal(struct {
		Regions  []sfz.Region  `json:"regions"`
		Warnings []sfz.Warning `json:"warnings"`
	}{regions, warnings})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	matchSnapshot(t, syntheticSnapDir, "JUNGLISM.sfz", ".sfz-parse", payload)
}

func typedExt(typeName string) string {
	switch typeName {
	case disk.TypeFullDumpLabel:
		return ".fzf"
	case typeLabelVoice:
		return ".fzv"
	default:
		return ""
	}
}
