//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

func TestCLIRoundTrip(t *testing.T) {
	t.Parallel()
	fzvPath := extractVoiceViaCLI(t, fixtureImg("HOOVER.img"), "HOOVER")
	dir := t.TempDir()
	wav1 := filepath.Join(dir, "rt.wav")
	fzv2 := filepath.Join(dir, "rt.fzv")
	wav2 := filepath.Join(dir, "rt2.wav")
	mustRun(t, "fzv", "extract", fzvPath, wav1)
	mustRun(t, "fzv", "import", "--rate", "36000", wav1, fzv2)
	mustRun(t, "fzv", "extract", fzv2, wav2)
	info, err := os.Stat(wav2)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 44 {
		t.Errorf("round-trip WAV too small: %d", info.Size())
	}
}

func TestCLISfzConvert(t *testing.T) {
	t.Parallel()
	sfz := filepath.Join(fixturesDir(), "JUNGLISM.sfz")

	t.Run("basic conversion", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		fzfPath := filepath.Join(dir, "junglism.fzf")
		mustRun(t, "sfz", "convert", sfz, fzfPath)
		info, err := os.Stat(fzfPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() == 0 {
			t.Error("sfz convert produced empty file")
		}
		if info.Size()%1024 != 0 {
			t.Errorf("sfz convert output not sector-aligned: %d bytes", info.Size())
		}
	})
	t.Run("missing sfz fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "sfz", "convert", filepath.Join(t.TempDir(), "nope.sfz"), filepath.Join(t.TempDir(), "out.fzf"))
	})
	t.Run("unsupported rate fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "sfz", "convert", "--rate", "48000", sfz, filepath.Join(t.TempDir(), "out.fzf"))
	})
	t.Run("wrong arg count fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "sfz", "convert", sfz)
	})
}

func TestCLISfzConvertSizeWarnings(t *testing.T) {
	t.Parallel()
	sfz := filepath.Join(fixturesDir(), "JUNGLISM.sfz")
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "warn.fzf")
	_, stderr := mustRun(t, "sfz", "convert", sfz, fzfPath)
	if !strings.Contains(stderr, "exceeds floppy disk capacity") {
		t.Errorf("expected capacity warning on stderr:\n%s", stderr)
	}
}

func TestCLISfzConvertFitToDisk(t *testing.T) {
	t.Parallel()
	sfz := filepath.Join(fixturesDir(), "JUNGLISM.sfz")

	t.Run("output fits on disk", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		fzfPath := filepath.Join(dir, "fit.fzf")
		mustRun(t, "sfz", "convert", "--fit-to-disk", sfz, fzfPath)
		info, err := os.Stat(fzfPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 1308672 {
			t.Errorf("fit-to-disk output too large: %d > 1308672", info.Size())
		}
	})
	t.Run("warns about downsampling", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		_, stderr := mustRun(t, "sfz", "convert", "--fit-to-disk", sfz, filepath.Join(dir, "fit.fzf"))
		if !strings.Contains(stderr, "downsampling") {
			t.Errorf("expected downsampling warning:\n%s", stderr)
		}
	})
	t.Run("rate ceiling succeeds", func(t *testing.T) {
		t.Parallel()
		mustRun(t, "sfz", "convert", "--rate", "18000", "--fit-to-disk", sfz, filepath.Join(t.TempDir(), "fit.fzf"))
	})
}

func TestCLISfzConvertRoundTrip(t *testing.T) {
	t.Parallel()
	sfz := filepath.Join(fixturesDir(), "JUNGLISM.sfz")
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "junglism.fzf")
	mustRun(t, "sfz", "convert", sfz, fzfPath)

	voicesDir := filepath.Join(dir, "voices")
	mustRun(t, "fzf", "unpack", fzfPath, voicesDir)
	if n := countFiles(t, voicesDir, ".fzv"); n != 26 {
		t.Errorf("expected 26 voices, got %d", n)
	}

	entries, _ := os.ReadDir(voicesDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".fzv") {
			wavPath := filepath.Join(dir, "voice.wav")
			mustRun(t, "fzv", "extract", filepath.Join(voicesDir, e.Name()), wavPath)
			data, err := os.ReadFile(wavPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(data[:4]) != "RIFF" {
				t.Errorf("extracted voice WAV missing RIFF header")
			}
			break
		}
	}
}

func TestCLISfzConvertSplitDisks(t *testing.T) {
	t.Parallel()
	sfz := filepath.Join(fixturesDir(), "JUNGLISM.sfz")
	dir := t.TempDir()
	prefix := filepath.Join(dir, "multi")

	mustRun(t, "sfz", "convert", "--rate", "36000", "--split-disks", sfz, prefix)

	img1 := prefix + "-1.img"
	img2 := prefix + "-2.img"
	if _, err := os.Stat(img1); err != nil {
		t.Fatalf("disk 1 image not produced: %v", err)
	}
	if _, err := os.Stat(img2); err != nil {
		t.Fatalf("disk 2 image not produced: %v", err)
	}

	info1, _ := os.Stat(img1)
	if info1.Size() != disk.ImageSize {
		t.Errorf("disk 1 image size %d, want %d", info1.Size(), disk.ImageSize)
	}

	fzf1 := filepath.Join(dir, "d1.fzf")
	mustRun(t, "disk", "get", img1, disk.FullDumpName, fzf1)

	t.Run("fzf info disk 1", func(t *testing.T) {
		out, _ := mustRun(t, "fzf", "info", fzf1)
		if !strings.Contains(out, "Disk:      1 of 2") {
			t.Errorf("disk 1 info missing 'Disk:      1 of 2':\n%s", out)
		}
		if !strings.Contains(out, "Memory:") {
			t.Errorf("disk 1 info missing Memory:\n%s", out)
		}
	})
	t.Run("mutually exclusive flags", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "sfz", "convert", "--split-disks", "--fit-to-disk", sfz, filepath.Join(t.TempDir(), "bad"))
	})
}

func TestCLIMultiDiskUnpack(t *testing.T) {
	t.Parallel()
	sfzPath := filepath.Join(fixturesDir(), "JUNGLISM.sfz")
	dir := t.TempDir()
	prefix := filepath.Join(dir, "md")
	mustRun(t, "sfz", "convert", "--rate", "36000", "--split-disks", sfzPath, prefix)

	fzf1 := filepath.Join(dir, "d1.fzf")
	mustRun(t, "disk", "get", prefix+"-1.img", disk.FullDumpName, fzf1)

	d1Dir := filepath.Join(dir, "v1")
	mustRun(t, "fzf", "unpack", fzf1, d1Dir)

	d1Count := countFiles(t, d1Dir, ".fzv")
	if d1Count == 0 {
		t.Error("disk 1 unpacked no voices")
	}
}

func TestCLISfzConvertFromDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fzvHoover := extractVoiceViaCLI(t, fixtureImg("HOOVER.img"), "HOOVER")
	fzvStab := extractVoiceViaCLI(t, fixtureImg("STAB.img"), "STAB")

	wavDir := filepath.Join(dir, "wavs")
	if err := os.MkdirAll(wavDir, 0755); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "fzv", "extract", fzvHoover, filepath.Join(wavDir, "01-hoover.wav"))
	mustRun(t, "fzv", "extract", fzvStab, filepath.Join(wavDir, "02-stab.wav"))

	fzfPath := filepath.Join(dir, "from-dir.fzf")
	mustRun(t, "sfz", "convert", wavDir, fzfPath)

	out, _ := mustRun(t, "fzf", "info", fzfPath)
	if !strings.Contains(out, "Voices:") {
		t.Errorf("fzf info missing Voices:\n%s", out)
	}

	t.Run("empty dir fails", func(t *testing.T) {
		t.Parallel()
		emptyDir := filepath.Join(t.TempDir(), "empty")
		os.MkdirAll(emptyDir, 0755)
		mustFail(t, "sfz", "convert", emptyDir, filepath.Join(t.TempDir(), "empty.fzf"))
	})
}
