package integration_test

import (
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
)

func manifestCollection(t *testing.T, id string) layoutCollection {
	t.Helper()
	for _, collection := range readLayoutManifest(t).Collections {
		if collection.ID == id {
			return collection
		}
	}
	t.Fatalf("fixture collection %q is not declared in testdata/layout-manifest.json", id)
	return layoutCollection{}
}

func manifestDiskFixture(t *testing.T, id string) diskFixture {
	t.Helper()
	for _, fixture := range readLayoutManifest(t).DiskFixtures {
		if fixture.ID == id {
			return fixture
		}
	}
	t.Fatalf("disk fixture %q is not declared in testdata/layout-manifest.json", id)
	return diskFixture{}
}

func manifestTestdataPath(t *testing.T, rel string) string {
	t.Helper()
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		t.Fatalf("manifest path %q escapes testdata", rel)
	}
	return filepath.Join("..", "..", "testdata", clean)
}

func TestLayoutManifestIntegrity(t *testing.T) {
	manifest := readLayoutManifest(t)
	ids := make(map[string]string)
	paths := make(map[string]string)

	claimID := func(kind, id string) {
		t.Helper()
		if id == "" {
			t.Fatalf("%s has an empty id", kind)
		}
		if previous, found := ids[id]; found {
			t.Fatalf("duplicate manifest id %q on %s and %s", id, previous, kind)
		}
		ids[id] = kind
	}
	claimPath := func(kind, path string) {
		t.Helper()
		manifestTestdataPath(t, path)
		if previous, found := paths[path]; found {
			t.Fatalf("duplicate manifest path %q on %s and %s", path, previous, kind)
		}
		paths[path] = kind
	}

	for _, collection := range manifest.Collections {
		kind := "collection " + collection.ID
		claimID(kind, collection.ID)
		claimPath(kind, collection.Root)
		if collection.ExpectedFiles < 1 {
			t.Errorf("%s expectedFiles = %d, want a positive count", kind, collection.ExpectedFiles)
		}
		if collection.ExpectedAuthority != "walk" && collection.ExpectedAuthority != "marker" {
			t.Errorf("%s expectedAuthority = %q, want walk or marker", kind, collection.ExpectedAuthority)
		}
		if _, err := hex.DecodeString(collection.LayoutDigest); err != nil || len(collection.LayoutDigest) != 64 {
			t.Errorf("%s has invalid SHA-256 layoutDigest %q", kind, collection.LayoutDigest)
		}
	}

	for _, fixture := range manifest.DiskFixtures {
		kind := "disk fixture " + fixture.ID
		claimID(kind, fixture.ID)
		claimPath(kind, fixture.Path)
		if _, err := hex.DecodeString(fixture.SourceSHA256); err != nil || len(fixture.SourceSHA256) != 64 {
			t.Errorf("%s has invalid sourceSHA256 %q", kind, fixture.SourceSHA256)
		}
		if fixture.Layout == nil && fixture.ExpectedDISVoiceCount != 0 {
			t.Errorf("%s has DIS voice count %d without a full-dump layout", kind, fixture.ExpectedDISVoiceCount)
		}
		if fixture.Layout != nil && fixture.ExpectedDISVoiceCount < 1 {
			t.Errorf("%s has a full-dump layout without a positive DIS voice count", kind)
		}
	}

	if got := manifestCollection(t, "casio-fz-1-factory-library").ID; got == "" {
		t.Fatal("collection lookup returned an empty record")
	}
	if got := manifestDiskFixture(t, "prey").ID; got == "" {
		t.Fatal("disk fixture lookup returned an empty record")
	}
}
