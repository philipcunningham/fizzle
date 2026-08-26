//go:build integration

package integration_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

func TestCLIDiskLsJSON(t *testing.T) {
	t.Parallel()
	t.Run("HOOVER", func(t *testing.T) {
		t.Parallel()
		out, _ := mustRun(t, "disk", "ls", "--json", fixtureImg("HOOVER.img"))
		var parsed map[string]any
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatalf("disk ls --json output is not valid JSON: %v\noutput: %s", err, out)
		}
		if _, ok := parsed["label"]; !ok {
			t.Errorf("JSON output missing 'label' key:\n%s", out)
		}
		if _, ok := parsed["entries"]; !ok {
			t.Errorf("JSON output missing 'entries' key:\n%s", out)
		}
	})
	t.Run("BRASS", func(t *testing.T) {
		t.Parallel()
		out, _ := mustRun(t, "disk", "ls", "--json", fixtureImg("BRASS.img"))
		var parsed map[string]any
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatalf("disk ls --json output is not valid JSON: %v\noutput: %s", err, out)
		}
		entries, ok := parsed["entries"].([]any)
		if !ok {
			t.Fatalf("expected 'entries' to be an array:\n%s", out)
		}
		if len(entries) == 0 {
			t.Errorf("expected at least one entry in JSON output:\n%s", out)
		}
	})
}

func TestCLIFzvInfoJSON(t *testing.T) {
	t.Parallel()
	fzvPath := extractVoiceViaCLI(t, fixtureImg("HOOVER.img"), "HOOVER")

	t.Run("valid JSON with expected fields", func(t *testing.T) {
		out, _ := mustRun(t, "fzv", "info", "--json", fzvPath)
		var parsed map[string]any
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatalf("fzv info --json output is not valid JSON: %v\noutput: %s", err, out)
		}
		for _, key := range []string{"name", "sample_rate", "duration"} {
			if _, ok := parsed[key]; !ok {
				t.Errorf("JSON output missing %q key:\n%s", key, out)
			}
		}
	})
	t.Run("missing file fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "fzv", "info", "--json", filepath.Join(t.TempDir(), "nope.fzv"))
	})
}

func TestCLIFzfInfoJSON(t *testing.T) {
	t.Parallel()
	fzvPath := extractVoiceViaCLI(t, fixtureImg("HOOVER.img"), "HOOVER")
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "full.fzf")
	mustRun(t, "fzf", "build", fzfPath, fzvPath, fzvPath)

	t.Run("valid JSON with expected fields", func(t *testing.T) {
		out, _ := mustRun(t, "fzf", "info", "--json", fzfPath)
		var parsed map[string]any
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatalf("fzf info --json output is not valid JSON: %v\noutput: %s", err, out)
		}
		for _, key := range []string{"voices"} {
			if _, ok := parsed[key]; !ok {
				t.Errorf("JSON output missing %q key:\n%s", key, out)
			}
		}
	})
	t.Run("missing file fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "fzf", "info", "--json", filepath.Join(t.TempDir(), "nope.fzf"))
	})
}

func TestCLIDiskAddDiskNum(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fzvPath := extractVoiceViaCLI(t, fixtureImg("HOOVER.img"), "HOOVER")

	t.Run("disk-num 1 explicit", func(t *testing.T) {
		t.Parallel()
		img := filepath.Join(t.TempDir(), "d1.img")
		mustRun(t, "disk", "new", "DISK1", img)
		mustRun(t, "disk", "add", "--disk-num", "1", img, fzvPath)
		out, _ := mustRun(t, "disk", "ls", img)
		if !strings.Contains(out, "HOOVER") {
			t.Errorf("disk missing HOOVER after --disk-num 1:\n%s", out)
		}
	})
	t.Run("disk-num 0 fails", func(t *testing.T) {
		t.Parallel()
		img := filepath.Join(t.TempDir(), "bad.img")
		mustRun(t, "disk", "new", "BAD", img)
		mustFail(t, "disk", "add", "--disk-num", "0", img, fzvPath)
	})
	t.Run("disk-num 3 fails", func(t *testing.T) {
		t.Parallel()
		img := filepath.Join(dir, "bad.img")
		mustRun(t, "disk", "new", "BAD", img)
		mustFail(t, "disk", "add", "--disk-num", "3", img, fzvPath)
	})
}

func TestCLIFzfUnpackDisk2(t *testing.T) {
	t.Parallel()
	sfzPath := filepath.Join(fixturesDir(), "JUNGLISM.sfz")
	dir := t.TempDir()
	prefix := filepath.Join(dir, "md")
	mustRun(t, "sfz", "convert", "--rate", "36000", "--split-disks", sfzPath, prefix)

	img1 := prefix + "-1.img"
	img2 := prefix + "-2.img"

	t.Run("multi-disk unpack merges all voices", func(t *testing.T) {
		outDir := filepath.Join(dir, "merged")
		mustRun(t, "fzf", "unpack", img1, "--disk2", img2, outDir)
		n := countFiles(t, outDir, ".fzv")
		if n != 26 {
			t.Errorf("multi-disk unpack: got %d voices, want 26", n)
		}
	})
	t.Run("single-disk unpack gets partial voices", func(t *testing.T) {
		t.Parallel()
		fzf1 := filepath.Join(t.TempDir(), "d1.fzf")
		mustRun(t, "disk", "get", img1, disk.FullDumpName, fzf1)
		d1Dir := filepath.Join(t.TempDir(), "d1voices")
		mustRun(t, "fzf", "unpack", fzf1, d1Dir)
		n := countFiles(t, d1Dir, ".fzv")
		if n >= 26 {
			t.Errorf("single-disk unpack should get fewer than 26 voices, got %d", n)
		}
		if n == 0 {
			t.Error("single-disk unpack got 0 voices")
		}
	})
}
