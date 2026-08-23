package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
)

type layoutManifest struct {
	Version      int                `json:"version"`
	Collections  []layoutCollection `json:"collections"`
	DiskFixtures []diskFixture      `json:"diskFixtures"`
}

type layoutCollection struct {
	ID                string `json:"id"`
	Root              string `json:"root"`
	Source            string `json:"source"`
	License           string `json:"license"`
	EvidenceTier      string `json:"evidenceTier"`
	Format            string `json:"format"`
	ParseContext      string `json:"parseContext"`
	ExpectedAuthority string `json:"expectedAuthority"`
	ExpectedFiles     int    `json:"expectedFiles"`
	LayoutDigest      string `json:"layoutDigest"`
}

type diskFixture struct {
	ID                    string          `json:"id"`
	Path                  string          `json:"path"`
	SourceSHA256          string          `json:"sourceSHA256"`
	EvidenceTier          string          `json:"evidenceTier"`
	Tags                  []string        `json:"tags"`
	ExpectedDISVoiceCount int             `json:"expectedDISVoiceCount"`
	Layout                *expectedLayout `json:"layout,omitempty"`
}

type expectedLayout struct {
	Authority  string `json:"authority"`
	Banks      int    `json:"banks"`
	Voices     int    `json:"voices"`
	VoiceStart int    `json:"voiceStart"`
	AudioStart int    `json:"audioStart"`
}

type layoutRecord struct {
	Path       string `json:"path"`
	Source     string `json:"source"`
	Banks      int    `json:"banks"`
	Voices     int    `json:"voices"`
	VoiceStart int    `json:"voiceStart"`
	AudioStart int    `json:"audioStart"`
}

func readLayoutManifest(t *testing.T) layoutManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "layout-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest layoutManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 {
		t.Fatalf("manifest version = %d, want 1", manifest.Version)
	}
	return manifest
}

func voiceCountSourceName(source fzutil.VoiceCountSource) string {
	switch source {
	case fzutil.VoiceCountWalk:
		return "walk"
	case fzutil.VoiceCountDIS:
		return "dis"
	case fzutil.VoiceCountMarker:
		return "marker"
	default:
		return "unknown"
	}
}

func recordLayout(path string, header *fzutil.FZFHeader, source fzutil.VoiceCountSource) layoutRecord {
	return layoutRecord{
		Path:       filepath.ToSlash(path),
		Source:     voiceCountSourceName(source),
		Banks:      header.NBankSectors,
		Voices:     header.NVoice,
		VoiceStart: header.VoiceAreaStart,
		AudioStart: header.VoiceAreaStart + disk.VoiceAreaSectors(header.NVoice)*disk.SectorSize,
	}
}

func TestStandaloneCorpusLayoutManifest(t *testing.T) {
	skipShort(t)
	manifest := readLayoutManifest(t)
	coveredRoots := make(map[string]bool, len(manifest.Collections))
	for _, collection := range manifest.Collections {
		collection := collection
		t.Run(collection.ID, func(t *testing.T) {
			if collection.Source == "" || collection.License == "" || collection.EvidenceTier == "" {
				t.Fatal("collection is missing provenance metadata")
			}
			if collection.Format != "fzf" || collection.ParseContext != "standalone" {
				t.Fatalf("unsupported format/context %q/%q", collection.Format, collection.ParseContext)
			}
			coveredRoots[collection.Root] = true
			root := filepath.Join("..", "..", "testdata", filepath.FromSlash(collection.Root))
			var records []layoutRecord
			err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
				if err != nil || entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".fzf") {
					return err
				}
				data, err := os.ReadFile(path) //nolint:gosec // path comes from the repository fixture walk
				if err != nil {
					return err
				}
				header, source, err := fzutil.ResolveStandaloneFZF(data)
				if err != nil {
					t.Fatalf("resolve %s: %v", path, err)
				}
				rel, err := filepath.Rel(filepath.Join("..", "..", "testdata"), path)
				if err != nil {
					return err
				}
				record := recordLayout(rel, header, source)
				if record.Source != collection.ExpectedAuthority {
					t.Fatalf("%s authority = %s, want %s", rel, record.Source, collection.ExpectedAuthority)
				}
				records = append(records, record)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path })
			if len(records) != collection.ExpectedFiles {
				t.Fatalf("files = %d, want %d", len(records), collection.ExpectedFiles)
			}
			encoded, err := json.Marshal(records)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(encoded)
			if got := hex.EncodeToString(digest[:]); got != collection.LayoutDigest {
				t.Fatalf("layout digest = %s, want %s", got, collection.LayoutDigest)
			}
		})
	}

	entries, err := os.ReadDir(filepath.Join("..", "..", "testdata", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() && !coveredRoots[filepath.ToSlash(filepath.Join("corpus", entry.Name()))] {
			t.Errorf("corpus collection %q is missing from the layout manifest", entry.Name())
		}
	}
}

func TestDiskFixtureLayoutManifest(t *testing.T) {
	manifest := readLayoutManifest(t)
	covered := make(map[string]bool, len(manifest.DiskFixtures))
	for _, fixture := range manifest.DiskFixtures {
		fixture := fixture
		t.Run(fixture.ID, func(t *testing.T) {
			if fixture.EvidenceTier == "" || fixture.SourceSHA256 == "" || len(fixture.Tags) == 0 {
				t.Fatal("disk fixture is missing provenance metadata")
			}
			covered[fixture.Path] = true
			path := filepath.Join("..", "..", "testdata", filepath.FromSlash(fixture.Path))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(data)
			if got := hex.EncodeToString(digest[:]); got != fixture.SourceSHA256 {
				t.Fatalf("source SHA-256 = %s, want %s", got, fixture.SourceSHA256)
			}
			img, err := disk.ReadImage(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			entry, dis, found := fullDumpDIS(t, img)
			if fixture.Layout == nil {
				if found {
					t.Fatalf("unexpected %s entry", entry.NameString())
				}
				return
			}
			if !found {
				t.Fatalf("missing %s entry", disk.FullDumpName)
			}
			if int(dis.VoiceCount) != fixture.ExpectedDISVoiceCount {
				t.Fatalf("DIS voice count = %d, want %d", dis.VoiceCount, fixture.ExpectedDISVoiceCount)
			}
			fzf, err := diskget.FromImage(img, disk.FullDumpName)
			if err != nil {
				t.Fatal(err)
			}
			header, source, err := fzutil.ResolveDiskFZF(fzf, int(dis.VoiceCount))
			if err != nil {
				t.Fatal(err)
			}
			got := recordLayout(fixture.Path, header, source)
			want := fixture.Layout
			if got.Source != want.Authority || got.Banks != want.Banks || got.Voices != want.Voices ||
				got.VoiceStart != want.VoiceStart || got.AudioStart != want.AudioStart {
				t.Fatalf("layout = %+v, want %+v", got, *want)
			}
		})
	}

	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "synthetic", "*.img"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		rel, err := filepath.Rel(filepath.Join("..", "..", "testdata"), path)
		if err != nil {
			t.Fatal(err)
		}
		if !covered[filepath.ToSlash(rel)] {
			t.Errorf("disk fixture %q is missing from the layout manifest", rel)
		}
	}
}

func fullDumpDIS(t *testing.T, img *disk.Image) (disk.DirEntry, disk.DisSector, bool) {
	t.Helper()
	entries, err := img.Directory()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.NameString() != disk.FullDumpName {
			continue
		}
		sector, err := img.SectorRef(int(entry.DisSector))
		if err != nil {
			t.Fatal(err)
		}
		dis, err := disk.DecodeDisSector(sector)
		if err != nil {
			t.Fatal(err)
		}
		return entry, dis, true
	}
	return disk.DirEntry{}, disk.DisSector{}, false
}

func TestGeneratedLayoutAuthorityMatrix(t *testing.T) {
	base := fzfbuilder.MakeBanklessVoiceDump(t)
	walkedVoices := fzfbuilder.BanklessDumpVoices - 1
	tests := []struct {
		name      string
		data      func() []byte
		disVN     int
		disk      bool
		authority fzutil.VoiceCountSource
		voices    int
	}{
		{"standalone walk", func() []byte { return bytes.Clone(base) }, 0, false, fzutil.VoiceCountWalk, walkedVoices},
		{"standalone marker", func() []byte {
			data := bytes.Clone(base)
			fzutil.StampVoiceCountMarker(data, fzfbuilder.BanklessDumpVoices)
			return data
		}, 0, false, fzutil.VoiceCountMarker, fzfbuilder.BanklessDumpVoices},
		{"invalid marker falls back", func() []byte {
			data := bytes.Clone(base)
			fzutil.StampVoiceCountMarker(data, fzfbuilder.BanklessDumpVoices)
			return append(data, 0)
		}, 0, false, fzutil.VoiceCountWalk, walkedVoices},
		{"disk DIS count", func() []byte { return bytes.Clone(base) }, fzfbuilder.BanklessDumpVoices, true, fzutil.VoiceCountDIS, fzfbuilder.BanklessDumpVoices},
		{"disk undercount falls back", func() []byte { return bytes.Clone(base) }, walkedVoices - 1, true, fzutil.VoiceCountWalk, walkedVoices},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				header *fzutil.FZFHeader
				source fzutil.VoiceCountSource
				err    error
			)
			if tt.disk {
				header, source, err = fzutil.ResolveDiskFZF(tt.data(), tt.disVN)
			} else {
				header, source, err = fzutil.ResolveStandaloneFZF(tt.data())
			}
			if err != nil {
				t.Fatal(err)
			}
			if source != tt.authority || header.NVoice != tt.voices {
				t.Fatalf("authority/voices = %s/%d, want %s/%d",
					voiceCountSourceName(source), header.NVoice, voiceCountSourceName(tt.authority), tt.voices)
			}
		})
	}
}

func TestSplitDumpLayoutCharacterization(t *testing.T) {
	voices := make([][]byte, 3)
	groups := make([]voicebuild.Keygroup, len(voices))
	for i := range voices {
		voices[i] = testutil.MakeTestVoice(fmt.Sprintf("SPLIT%d", i), 300000)
		groups[i] = voicebuild.NewKeygroup(uint8(i*24), uint8(i*24+23), uint8(i*24+12))
	}
	result, err := voicebuild.AssembleMultiDisk(voices, groups)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Disks) != 2 {
		t.Fatalf("disks = %d, want 2", len(result.Disks))
	}
	stitched := append(bytes.Clone(result.Disks[0]), result.Disks[1]...)
	header, source, err := fzutil.ResolveDiskFZF(stitched, result.VoiceCount)
	if err != nil {
		t.Fatal(err)
	}
	audioStart := header.VoiceAreaStart + disk.VoiceAreaSectors(header.NVoice)*disk.SectorSize
	boundary := len(result.Disks[0])
	if source != fzutil.VoiceCountWalk || header.NVoice != result.VoiceCount ||
		boundary <= audioStart || boundary >= len(stitched) {
		t.Fatalf("split layout = source %s, voices %d, audio start %d, boundary %d, total %d",
			voiceCountSourceName(source), header.NVoice, audioStart, boundary, len(stitched))
	}
}
