//go:build integration

package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIVersionAndHelp(t *testing.T) {
	t.Parallel()
	t.Run("version", func(t *testing.T) {
		t.Parallel()
		out, _, _ := runFizzle(t, "--version")
		if !strings.Contains(out, "fizzle") {
			t.Errorf("--version output missing 'fizzle': %s", out)
		}
	})
	t.Run("help lists disk", func(t *testing.T) {
		t.Parallel()
		out, _ := mustRun(t, "--help")
		for _, want := range []string{"disk", "fzv", "fzf", "sfz"} {
			if !strings.Contains(out, want) {
				t.Errorf("--help missing %q", want)
			}
		}
	})
	t.Run("disk help shows new", func(t *testing.T) {
		t.Parallel()
		out, _ := mustRun(t, "disk", "--help")
		if !strings.Contains(out, "new") {
			t.Errorf("disk --help missing 'new': %s", out)
		}
	})
	t.Run("fzv help shows extract", func(t *testing.T) {
		t.Parallel()
		out, _ := mustRun(t, "fzv", "--help")
		if !strings.Contains(out, "extract") {
			t.Errorf("fzv --help missing 'extract': %s", out)
		}
	})
	t.Run("unknown command fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "notacommand")
	})
}

func TestCLIDiskNew(t *testing.T) {
	t.Parallel()
	t.Run("creates correct size", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		imgPath := filepath.Join(dir, "test.img")
		mustRun(t, "disk", "new", "TESTLABEL", imgPath)
		info, err := os.Stat(imgPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() != 1310720 {
			t.Errorf("image size = %d, want 1310720", info.Size())
		}
	})
	t.Run("missing arg fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "disk", "new", "ONLYONEARG")
	})
}

func TestCLIDiskLs(t *testing.T) {
	t.Parallel()
	t.Run("HOOVER", func(t *testing.T) {
		t.Parallel()
		out, _ := mustRun(t, "disk", "ls", fixtureImg("HOOVER.img"))
		for _, want := range []string{"HOOVER", "Voice", "KB", "free"} {
			if !strings.Contains(out, want) {
				t.Errorf("disk ls HOOVER.img missing %q:\n%s", want, out)
			}
		}
	})
	t.Run("STAB", func(t *testing.T) {
		t.Parallel()
		out, _ := mustRun(t, "disk", "ls", fixtureImg("STAB.img"))
		if !strings.Contains(out, "STAB") {
			t.Errorf("disk ls STAB.img missing 'STAB':\n%s", out)
		}
	})
	t.Run("TECHNO", func(t *testing.T) {
		t.Parallel()
		out, _ := mustRun(t, "disk", "ls", fixtureImg("TECHNO.img"))
		if !strings.Contains(out, "Techno Split") {
			t.Errorf("missing 'Techno Split':\n%s", out)
		}
		if !strings.Contains(out, "Full Dump") {
			t.Errorf("missing 'Full Dump':\n%s", out)
		}
	})
	t.Run("BRASS", func(t *testing.T) {
		t.Parallel()
		out, _ := mustRun(t, "disk", "ls", fixtureImg("BRASS.img"))
		for _, want := range []string{"Brass Ensemb", "FULL-DATA-FZ", "Full Dump"} {
			if !strings.Contains(out, want) {
				t.Errorf("disk ls BRASS.img missing %q:\n%s", want, out)
			}
		}
	})
	t.Run("BRASS unpack produces 13 voices", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		fzfPath := filepath.Join(dir, "brass.fzf")
		mustRun(t, "disk", "get", fixtureImg("BRASS.img"), "FULL-DATA-FZ", fzfPath)
		voicesDir := filepath.Join(dir, "voices")
		mustRun(t, "fzf", "unpack", fzfPath, voicesDir)
		// BRASS is multi-bank: 13 distinct voice slots across all banks.
		if n := countFiles(t, voicesDir, ".fzv"); n != 13 {
			t.Errorf("BRASS unpack: got %d voices, want 13", n)
		}
	})
	t.Run("TECHNO unpack produces 32 voices", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		fzfPath := filepath.Join(dir, "techno.fzf")
		mustRun(t, "disk", "get", fixtureImg("TECHNO.img"), "FULL-DATA-FZ", fzfPath)
		voicesDir := filepath.Join(dir, "voices")
		mustRun(t, "fzf", "unpack", fzfPath, voicesDir)
		// TECHNO is multi-bank: 32 distinct voice slots across all 8 banks.
		if n := countFiles(t, voicesDir, ".fzv"); n != 32 {
			t.Errorf("TECHNO unpack: got %d voices, want 32", n)
		}
	})
	t.Run("missing file fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "disk", "ls", filepath.Join(t.TempDir(), "nope.img"))
	})
}

func TestCLIDiskAddAndGet(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fzvPath := extractVoiceViaCLI(t, fixtureImg("HOOVER.img"), "HOOVER")

	t.Run("add voice to blank disk", func(t *testing.T) {
		imgPath := filepath.Join(dir, "add.img")
		mustRun(t, "disk", "new", "MYTEST", imgPath)
		mustRun(t, "disk", "add", imgPath, fzvPath)
		out, _ := mustRun(t, "disk", "ls", imgPath)
		if !strings.Contains(out, "Voice") {
			t.Errorf("disk ls after add missing 'Voice':\n%s", out)
		}
	})
	t.Run("wrong arg count fails", func(t *testing.T) {
		t.Parallel()
		imgPath := filepath.Join(dir, "add.img")
		mustFail(t, "disk", "add", imgPath)
	})
}

// TestCLIDiskAddProgramRoundTrip drives the Type-5 "Program" path through
// the full CLI: format a fresh disk, add the DEMO binary as a Program
// file, check the directory listing, extract it back, and compare bytes.
// It uses the committed testdata/assembly/DEMO.bin so CI needs no nasm.
func TestCLIDiskAddProgramRoundTrip(t *testing.T) {
	t.Parallel()
	demoPath := filepath.Join("..", "..", "testdata", "assembly", "DEMO.bin")
	original, err := os.ReadFile(demoPath)
	if err != nil {
		t.Fatalf("reading DEMO fixture (regenerate with 'make demo'): %v", err)
	}

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "demo.img")
	mustRun(t, "disk", "new", "DEMO_TEST", imgPath)
	mustRun(t, "disk", "add", imgPath, demoPath)

	out, _ := mustRun(t, "disk", "ls", imgPath)
	if !strings.Contains(out, "DEMO") {
		t.Errorf("disk ls missing DEMO name:\n%s", out)
	}
	if !strings.Contains(out, "Program") {
		t.Errorf("disk ls missing Program type:\n%s", out)
	}

	getPath := filepath.Join(dir, "DEMO-out.bin")
	mustRun(t, "disk", "get", imgPath, "DEMO", getPath)
	roundTripped, err := os.ReadFile(getPath)
	if err != nil {
		t.Fatalf("reading extracted DEMO: %v", err)
	}
	if !bytes.Equal(original, roundTripped) {
		t.Errorf("round-trip mismatch: original %d bytes, extracted %d bytes", len(original), len(roundTripped))
	}
}

func TestCLIDiskCopy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fzvPath := extractVoiceViaCLI(t, fixtureImg("HOOVER.img"), "HOOVER")

	srcImg := filepath.Join(dir, "src.img")
	dstImg := filepath.Join(dir, "dst.img")
	mustRun(t, "disk", "new", "SRC", srcImg)
	mustRun(t, "disk", "add", srcImg, fzvPath)
	mustRun(t, "disk", "new", "DST", dstImg)

	t.Run("copy succeeds", func(t *testing.T) {
		mustRun(t, "disk", "copy", srcImg, "HOOVER", dstImg)
		out, _ := mustRun(t, "disk", "ls", dstImg)
		if !strings.Contains(out, "HOOVER") {
			t.Errorf("dst disk missing HOOVER:\n%s", out)
		}
	})
	t.Run("missing name fails", func(t *testing.T) {
		mustFail(t, "disk", "copy", srcImg, "NOSUCHFILE", filepath.Join(dir, "dst2.img"))
	})
}
