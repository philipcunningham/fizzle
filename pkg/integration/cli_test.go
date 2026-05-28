//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
)

var fizzleBin string

func TestMain(m *testing.M) {
	bin := os.Getenv("FIZZLE_BIN")
	if bin == "" {
		tmp, err := os.MkdirTemp("", "fizzle-cli-test-*")
		if err != nil {
			fmt.Fprintf(os.Stderr, "creating temp dir: %v\n", err)
			os.Exit(1)
		}
		defer os.RemoveAll(tmp)
		bin = filepath.Join(tmp, "fizzle")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		repoRoot := filepath.Join("..", "..")
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/fizzle")
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "build failed: %v\n%s\n", err, out)
			os.Exit(1)
		}
	}
	fizzleBin = bin
	os.Exit(m.Run())
}

func runFizzle(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(fizzleBin, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode = 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("exec error (not ExitError): %v", err)
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

func mustRun(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	out, serr, code := runFizzle(t, args...)
	if code != 0 {
		t.Fatalf("fizzle %s: exit %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), code, out, serr)
	}
	return out, serr
}

func mustFail(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	out, serr, code := runFizzle(t, args...)
	if code == 0 {
		t.Fatalf("fizzle %s: expected non-zero exit\nstdout: %s\nstderr: %s", strings.Join(args, " "), out, serr)
	}
	return out, serr
}

func countFiles(t *testing.T, dir, suffix string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading dir %s: %v", dir, err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) {
			n++
		}
	}
	return n
}

func fixturesDir() string {
	return filepath.Join("..", "..", "testdata", "synthetic")
}

func fixtureImg(name string) string {
	return filepath.Join(fixturesDir(), name)
}

func extractVoiceViaCLI(t *testing.T, imgPath, name string) string {
	t.Helper()
	dir := t.TempDir()
	outPath := filepath.Join(dir, name+".fzv")
	mustRun(t, "disk", "get", imgPath, name, outPath)
	return outPath
}

// assertVoiceOutputs runs `fzf info --json` and verifies that the named
// voices have the expected output strings. Other voices are ignored. This
// is more robust than asserting on rendered table glyphs because the JSON
// format is stable across renderer changes.
func assertVoiceOutputs(t *testing.T, fzfPath string, want map[string]string) {
	t.Helper()
	out, _ := mustRun(t, "fzf", "info", "--json", fzfPath)
	var parsed struct {
		Voices []struct {
			Name   string `json:"name"`
			Output string `json:"output"`
		} `json:"voices"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("parsing fzf info JSON: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, v := range parsed.Voices {
		got[v.Name] = v.Output
	}
	for name, expected := range want {
		if actual, ok := got[name]; !ok {
			t.Errorf("voice %q not found in info output (have %v)", name, got)
		} else if actual != expected {
			t.Errorf("voice %q output: got %q, want %q", name, actual, expected)
		}
	}
}

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

// TestCLIDiskAddProgramRoundTrip exercises the Type-5 "Program" path through
// the full CLI: format a fresh disk, add the DEMO binary as a Program
// file, verify the directory listing, extract it back, and confirm the
// bytes match. Uses the committed testdata/assembly/DEMO.bin fixture so it
// runs in CI without nasm installed.
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

func TestCLIFzvExtract(t *testing.T) {
	t.Parallel()
	fzvPath := extractVoiceViaCLI(t, fixtureImg("HOOVER.img"), "HOOVER")
	dir := t.TempDir()

	t.Run("produces WAV with RIFF header", func(t *testing.T) {
		wavPath := filepath.Join(dir, "out.wav")
		mustRun(t, "fzv", "extract", fzvPath, wavPath)
		data, err := os.ReadFile(wavPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) < 44 {
			t.Fatalf("WAV too small: %d bytes", len(data))
		}
		if string(data[:4]) != "RIFF" {
			t.Errorf("WAV header = %q, want RIFF", data[:4])
		}
	})
	t.Run("wrong arg count fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "fzv", "extract", "onlyonearg")
	})
}

func TestCLIFzvImport(t *testing.T) {
	t.Parallel()
	fzvPath := extractVoiceViaCLI(t, fixtureImg("HOOVER.img"), "HOOVER")
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "hoover.wav")
	mustRun(t, "fzv", "extract", fzvPath, wavPath)

	t.Run("36kHz", func(t *testing.T) {
		t.Parallel()
		mustRun(t, "fzv", "import", "--rate", "36000", wavPath, filepath.Join(t.TempDir(), "out.fzv"))
	})
	t.Run("18kHz", func(t *testing.T) {
		t.Parallel()
		mustRun(t, "fzv", "import", "--rate", "18000", wavPath, filepath.Join(t.TempDir(), "out.fzv"))
	})
	t.Run("9kHz", func(t *testing.T) {
		t.Parallel()
		mustRun(t, "fzv", "import", "--rate", "9000", wavPath, filepath.Join(t.TempDir(), "out.fzv"))
	})
	t.Run("unsupported rate fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "fzv", "import", "--rate", "48000", wavPath, filepath.Join(t.TempDir(), "out.fzv"))
	})
	t.Run("missing WAV fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "fzv", "import", "--rate", "36000", filepath.Join(t.TempDir(), "nope.wav"), filepath.Join(t.TempDir(), "out.fzv"))
	})
}

func TestCLIFzvInfo(t *testing.T) {
	t.Parallel()
	fzvPath := extractVoiceViaCLI(t, fixtureImg("HOOVER.img"), "HOOVER")

	t.Run("shows Hz and Duration and Envelope", func(t *testing.T) {
		out, _ := mustRun(t, "fzv", "info", fzvPath)
		for _, want := range []string{"Hz", "Duration", "Envelope"} {
			if !strings.Contains(out, want) {
				t.Errorf("fzv info missing %q:\n%s", want, out)
			}
		}
	})
	t.Run("missing file fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "fzv", "info", filepath.Join(t.TempDir(), "nope.fzv"))
	})
}

func TestCLIFzfBuildAndUnpack(t *testing.T) {
	t.Parallel()
	fzvPath := extractVoiceViaCLI(t, fixtureImg("HOOVER.img"), "HOOVER")
	dir := t.TempDir()

	t.Run("build produces full dump", func(t *testing.T) {
		fzfPath := filepath.Join(dir, "full.fzf")
		mustRun(t, "fzf", "build", fzfPath, fzvPath, fzvPath)
	})
	t.Run("build no voices fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "fzf", "build", filepath.Join(t.TempDir(), "out.fzf"))
	})
	t.Run("unpack creates fzv files", func(t *testing.T) {
		fzfPath := filepath.Join(dir, "full.fzf")
		mustRun(t, "fzf", "build", fzfPath, fzvPath, fzvPath)
		unpackDir := filepath.Join(dir, "unpacked")
		mustRun(t, "fzf", "unpack", fzfPath, unpackDir)
		if n := countFiles(t, unpackDir, ".fzv"); n == 0 {
			t.Error("fzf unpack produced no .fzv files")
		}
	})
	t.Run("unpack wrong args fails", func(t *testing.T) {
		t.Parallel()
		fzfPath := filepath.Join(dir, "full.fzf")
		mustFail(t, "fzf", "unpack", fzfPath)
	})
	t.Run("unpack missing file fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "fzf", "unpack", filepath.Join(t.TempDir(), "nope.fzf"), t.TempDir())
	})
}

func TestCLIFzfInfo(t *testing.T) {
	t.Parallel()
	fzvPath := extractVoiceViaCLI(t, fixtureImg("HOOVER.img"), "HOOVER")
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "full.fzf")
	mustRun(t, "fzf", "build", fzfPath, fzvPath, fzvPath)

	t.Run("shows Voices and name and Memory", func(t *testing.T) {
		out, _ := mustRun(t, "fzf", "info", fzfPath)
		for _, want := range []string{"Voices", "HOOVER", "Memory"} {
			if !strings.Contains(out, want) {
				t.Errorf("fzf info missing %q:\n%s", want, out)
			}
		}
	})
	t.Run("missing file fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "fzf", "info", filepath.Join(t.TempDir(), "nope.fzf"))
	})
}

func makeTestFZB(t *testing.T, names []string) string {
	t.Helper()
	fzfData, _ := fzfbuilder.MakeTestFZF(t, names)
	voiceSectors := disk.VoiceAreaSectors(len(names))
	fzbEnd := disk.SectorSize + voiceSectors*disk.SectorSize
	if fzbEnd > len(fzfData) {
		t.Fatalf("FZF too small to truncate to FZB: %d < %d", len(fzfData), fzbEnd)
	}
	fzbPath := filepath.Join(t.TempDir(), "test.fzb")
	if err := os.WriteFile(fzbPath, fzfData[:fzbEnd], 0644); err != nil {
		t.Fatal(err)
	}
	return fzbPath
}

func TestCLIFzbInfo(t *testing.T) {
	t.Parallel()
	fzbPath := makeTestFZB(t, []string{"KICK", "SNARE"})

	t.Run("shows voice names", func(t *testing.T) {
		t.Parallel()
		out, _ := mustRun(t, "fzb", "info", fzbPath)
		for _, want := range []string{"KICK", "SNARE"} {
			if !strings.Contains(out, want) {
				t.Errorf("fzb info missing %q:\n%s", want, out)
			}
		}
	})
	t.Run("missing file fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "fzb", "info", filepath.Join(t.TempDir(), "nope.fzb"))
	})
	t.Run("no args fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "fzb", "info")
	})
}

func TestCLIFzbInfoJSON(t *testing.T) {
	t.Parallel()
	fzbPath := makeTestFZB(t, []string{"KICK", "SNARE"})
	out, _ := mustRun(t, "fzb", "info", "--json", fzbPath)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	for _, key := range []string{"filename", "voice_count", "voices"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("JSON output missing top-level key %q: %v", key, parsed)
		}
	}
	if vc, _ := parsed["voice_count"].(float64); int(vc) != 2 {
		t.Errorf("voice_count = %v, want 2", parsed["voice_count"])
	}
}

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

func TestCLIFzfMidi(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fzvHoover := extractVoiceViaCLI(t, fixtureImg("HOOVER.img"), "HOOVER")
	fzvStab := extractVoiceViaCLI(t, fixtureImg("STAB.img"), "STAB")
	fzfPath := filepath.Join(dir, "midi-test.fzf")
	mustRun(t, "fzf", "build", fzfPath, fzvHoover, fzvStab)

	t.Run("set channel 2", func(t *testing.T) {
		mustRun(t, "fzf", "midi", fzfPath, "--voice", "HOOVER", "--channel", "2")
		out, _ := mustRun(t, "fzf", "info", fzfPath)
		if !strings.Contains(out, "Chan") {
			t.Errorf("Chan column missing after midi set:\n%s", out)
		}
		if !strings.Contains(out, "2") {
			t.Errorf("channel 2 missing from info:\n%s", out)
		}
	})
	t.Run("reset all to 1", func(t *testing.T) {
		mustRun(t, "fzf", "midi", fzfPath, "--all", "--channel", "1")
		out, _ := mustRun(t, "fzf", "info", fzfPath)
		if !strings.Contains(out, "Chan") {
			t.Errorf("Chan column should remain after reset:\n%s", out)
		}
	})
	t.Run("unknown voice fails", func(t *testing.T) {
		out, serr := mustFail(t, "fzf", "midi", fzfPath, "--voice", "NOSUCHVOICE", "--channel", "2")
		combined := out + serr
		if !strings.Contains(combined, "HOOVER") {
			t.Errorf("error should list available voices:\n%s", combined)
		}
	})
	t.Run("voice and all mutually exclusive", func(t *testing.T) {
		mustFail(t, "fzf", "midi", fzfPath, "--voice", "HOOVER", "--all", "--channel", "2")
	})
	t.Run("channel out of range", func(t *testing.T) {
		mustFail(t, "fzf", "midi", fzfPath, "--all", "--channel", "17")
	})
	t.Run("missing channel", func(t *testing.T) {
		mustFail(t, "fzf", "midi", fzfPath, "--all")
	})
}

func TestCLIFzfOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fzvHoover := extractVoiceViaCLI(t, fixtureImg("HOOVER.img"), "HOOVER")
	fzvStab := extractVoiceViaCLI(t, fixtureImg("STAB.img"), "STAB")
	fzfPath := filepath.Join(dir, "output-test.fzf")
	mustRun(t, "fzf", "build", fzfPath, fzvHoover, fzvStab)

	t.Run("set single output", func(t *testing.T) {
		mustRun(t, "fzf", "output", fzfPath, "--voice", "HOOVER", "--output", "3")
		assertVoiceOutputs(t, fzfPath, map[string]string{"HOOVER": "3"})
	})
	t.Run("set multiple outputs", func(t *testing.T) {
		mustRun(t, "fzf", "output", fzfPath, "--voice", "STAB", "--output", "1,5")
		assertVoiceOutputs(t, fzfPath, map[string]string{"STAB": "1,5"})
	})
	t.Run("set output to all", func(t *testing.T) {
		mustRun(t, "fzf", "output", fzfPath, "--voice", "HOOVER", "--output", "all")
		assertVoiceOutputs(t, fzfPath, map[string]string{"HOOVER": "all"})
	})
	t.Run("target all voices", func(t *testing.T) {
		mustRun(t, "fzf", "output", fzfPath, "--all", "--output", "2")
		assertVoiceOutputs(t, fzfPath, map[string]string{"HOOVER": "2", "STAB": "2"})
	})
	t.Run("reset all outputs", func(t *testing.T) {
		mustRun(t, "fzf", "output", fzfPath, "--all", "--output", "all")
		assertVoiceOutputs(t, fzfPath, map[string]string{"HOOVER": "all", "STAB": "all"})
	})
	t.Run("unknown voice fails", func(t *testing.T) {
		out, serr := mustFail(t, "fzf", "output", fzfPath, "--voice", "NOSUCHVOICE", "--output", "1")
		combined := out + serr
		if !strings.Contains(combined, "HOOVER") {
			t.Errorf("error should list available voices:\n%s", combined)
		}
	})
	t.Run("voice and all mutually exclusive", func(t *testing.T) {
		mustFail(t, "fzf", "output", fzfPath, "--voice", "HOOVER", "--all", "--output", "1")
	})
	t.Run("invalid output value", func(t *testing.T) {
		mustFail(t, "fzf", "output", fzfPath, "--all", "--output", "9")
	})
	t.Run("invalid output zero", func(t *testing.T) {
		mustFail(t, "fzf", "output", fzfPath, "--all", "--output", "0")
	})
}

func writeTestWAV(t *testing.T, path string, sampleRate uint32, nSamples int) {
	t.Helper()
	testutil.WriteTestWAV(t, path, sampleRate, nSamples)
}

func TestCLIOversizedVoiceWarning(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "long.wav")
	writeTestWAV(t, wavPath, 36000, 700000)
	fzvPath := filepath.Join(dir, "long.fzv")
	_, stderr := mustRun(t, "fzv", "import", "--rate", "36000", wavPath, fzvPath)
	if !strings.Contains(stderr, "exceeds floppy disk capacity") {
		t.Errorf("expected capacity warning on stderr:\n%s", stderr)
	}
	if _, err := os.Stat(fzvPath); err != nil {
		t.Errorf("fzv should still be created despite warning: %v", err)
	}
}

func TestCLIDebugLogging(t *testing.T) {
	t.Parallel()
	sfz := filepath.Join(fixturesDir(), "JUNGLISM.sfz")

	t.Run("debug flag shows DEBUG lines", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		_, stderr := mustRun(t, "--debug", "sfz", "convert", sfz, filepath.Join(dir, "out.fzf"))
		if !strings.Contains(stderr, "DEBUG") {
			t.Errorf("--debug should produce DEBUG lines on stderr:\n%s", stderr)
		}
	})
	t.Run("no debug flag omits DEBUG lines", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		_, stderr := mustRun(t, "sfz", "convert", sfz, filepath.Join(dir, "out.fzf"))
		if strings.Contains(stderr, "DEBUG") {
			t.Errorf("without --debug, stderr should not contain DEBUG:\n%s", stderr)
		}
	})
}

func TestCLIFzvEdit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(fixturesDir(), "JUNGLISM Samples", "amen 01.wav")
	fzvPath := filepath.Join(dir, "edit.fzv")
	mustRun(t, "fzv", "import", wavPath, fzvPath)
	mustRun(t, "fzv", "edit", fzvPath, "--lfo-rate", "30", "--lfo-filter", "50")
	out, _ := mustRun(t, "fzv", "info", fzvPath)
	if !strings.Contains(out, "Rate: 30") {
		t.Errorf("fzv info after edit missing 'Rate: 30':\n%s", out)
	}
	if !strings.Contains(out, "filter=50") {
		t.Errorf("fzv info after edit missing 'filter=50':\n%s", out)
	}
}

func TestCLIFzvEditNoFlags(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(fixturesDir(), "JUNGLISM Samples", "amen 01.wav")
	fzvPath := filepath.Join(dir, "edit.fzv")
	mustRun(t, "fzv", "import", wavPath, fzvPath)
	out, serr := mustFail(t, "fzv", "edit", fzvPath)
	combined := out + serr
	if !strings.Contains(combined, "no edit flags") {
		t.Errorf("expected 'no edit flags' error:\n%s", combined)
	}
}

func TestCLIFzvEditInvalidRate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(fixturesDir(), "JUNGLISM Samples", "amen 01.wav")
	fzvPath := filepath.Join(dir, "edit.fzv")
	mustRun(t, "fzv", "import", wavPath, fzvPath)
	out, serr := mustFail(t, "fzv", "edit", fzvPath, "--lfo-rate", "999")
	combined := out + serr
	if !strings.Contains(combined, "lfo-rate") {
		t.Errorf("expected lfo-rate validation error:\n%s", combined)
	}
}

func TestCLIFzvEditName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(fixturesDir(), "JUNGLISM Samples", "amen 01.wav")
	fzvPath := filepath.Join(dir, "edit.fzv")
	mustRun(t, "fzv", "import", wavPath, fzvPath)
	mustRun(t, "fzv", "edit", fzvPath, "--name", "NEW NAME")
	out, _ := mustRun(t, "fzv", "info", fzvPath)
	if !strings.Contains(out, "NEW NAME") {
		t.Errorf("fzv info after name edit missing 'NEW NAME':\n%s", out)
	}
}

func TestCLIFzfEdit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sfzPath := filepath.Join(fixturesDir(), "JUNGLISM.sfz")
	fzfPath := filepath.Join(dir, "junglism.fzf")
	mustRun(t, "sfz", "convert", sfzPath, fzfPath)
	mustRun(t, "fzf", "edit", fzfPath, "--voice", "REESE", "--cutoff", "50", "--resonance", "5")
	unpackDir := filepath.Join(dir, "unpacked")
	mustRun(t, "fzf", "unpack", fzfPath, unpackDir)
	out, _ := mustRun(t, "fzv", "info", filepath.Join(unpackDir, "REESE.fzv"))
	if !strings.Contains(out, "cutoff=50") {
		t.Errorf("fzv info after fzf edit missing 'cutoff=50':\n%s", out)
	}
	if !strings.Contains(out, "resonance=5") {
		t.Errorf("fzv info after fzf edit missing 'resonance=5':\n%s", out)
	}
}

func TestCLIFzfEditNoVoice(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sfzPath := filepath.Join(fixturesDir(), "JUNGLISM.sfz")
	fzfPath := filepath.Join(dir, "junglism.fzf")
	mustRun(t, "sfz", "convert", sfzPath, fzfPath)
	out, serr := mustFail(t, "fzf", "edit", fzfPath, "--cutoff", "50")
	combined := out + serr
	if !strings.Contains(combined, "voice is required") {
		t.Errorf("expected 'voice is required' error:\n%s", combined)
	}
}

func TestCLIFzfEditVoiceNotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sfzPath := filepath.Join(fixturesDir(), "JUNGLISM.sfz")
	fzfPath := filepath.Join(dir, "junglism.fzf")
	mustRun(t, "sfz", "convert", sfzPath, fzfPath)
	out, serr := mustFail(t, "fzf", "edit", fzfPath, "--voice", "NONEXISTENT", "--cutoff", "50")
	combined := out + serr
	if !strings.Contains(combined, "not found") {
		t.Errorf("expected 'not found' error:\n%s", combined)
	}
}

func TestCLIFzvEditDCA(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(fixturesDir(), "JUNGLISM Samples", "amen 01.wav")
	fzvPath := filepath.Join(dir, "edit-dca.fzv")
	mustRun(t, "fzv", "import", wavPath, fzvPath)
	mustRun(t, "fzv", "edit", fzvPath, "--dca-sustain", "2", "--dca-end", "3", "--dca-rate-1", "99", "--dca-stop-1", "85")
	out, _ := mustRun(t, "fzv", "info", fzvPath)
	if !strings.Contains(out, "Sustain: 2") {
		t.Errorf("fzv info after DCA edit missing 'Sustain: 2':\n%s", out)
	}
	if !strings.Contains(out, "End: 3") {
		t.Errorf("fzv info after DCA edit missing 'End: 3':\n%s", out)
	}
}

func TestCLIFzvEditDCF(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(fixturesDir(), "JUNGLISM Samples", "amen 01.wav")
	fzvPath := filepath.Join(dir, "edit-dcf.fzv")
	mustRun(t, "fzv", "import", wavPath, fzvPath)
	mustRun(t, "fzv", "edit", fzvPath, "--dcf-sustain", "1", "--dcf-end", "2", "--dcf-rate-1", "50", "--dcf-stop-1", "26")
	out, _ := mustRun(t, "fzv", "info", fzvPath)
	if !strings.Contains(out, "Sustain: 1") {
		t.Errorf("fzv info after DCF edit missing 'Sustain: 1':\n%s", out)
	}
	if !strings.Contains(out, "End: 2") {
		t.Errorf("fzv info after DCF edit missing 'End: 2':\n%s", out)
	}
}

func TestCLIFzvEditDCAInvalidRate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(fixturesDir(), "JUNGLISM Samples", "amen 01.wav")
	fzvPath := filepath.Join(dir, "edit-bad.fzv")
	mustRun(t, "fzv", "import", wavPath, fzvPath)
	out, serr := mustFail(t, "fzv", "edit", fzvPath, "--dca-rate-1", "100")
	combined := out + serr
	if !strings.Contains(combined, "dca-rate-1") {
		t.Errorf("expected dca-rate-1 validation error:\n%s", combined)
	}
}

func TestCLIFzfEditDCA(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sfzPath := filepath.Join(fixturesDir(), "JUNGLISM.sfz")
	fzfPath := filepath.Join(dir, "junglism.fzf")
	mustRun(t, "sfz", "convert", sfzPath, fzfPath)
	mustRun(t, "fzf", "edit", fzfPath, "--voice", "REESE", "--dca-sustain", "0", "--dca-end", "7", "--dca-rate-1", "62")
	unpackDir := filepath.Join(dir, "unpacked")
	mustRun(t, "fzf", "unpack", fzfPath, unpackDir)
	out, _ := mustRun(t, "fzv", "info", filepath.Join(unpackDir, "REESE.fzv"))
	if !strings.Contains(out, "Sustain: 0") {
		t.Errorf("fzv info after fzf DCA edit missing 'Sustain: 0':\n%s", out)
	}
	if !strings.Contains(out, "End: 7") {
		t.Errorf("fzv info after fzf DCA edit missing 'End: 7':\n%s", out)
	}
}

func TestCLIWrongFileTypeErrors(t *testing.T) {
	t.Parallel()
	fzvPath := extractVoiceViaCLI(t, fixtureImg("HOOVER.img"), "HOOVER")

	t.Run("fzv info on disk image fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "fzv", "info", fixtureImg("HOOVER.img"))
	})
	t.Run("fzf info on voice file fails", func(t *testing.T) {
		t.Parallel()
		mustFail(t, "fzf", "info", fzvPath)
	})
	t.Run("fzv extract on fzf file fails", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		fzfPath := filepath.Join(dir, "full.fzf")
		mustRun(t, "fzf", "build", fzfPath, fzvPath, fzvPath)
		mustFail(t, "fzv", "extract", fzfPath, filepath.Join(dir, "out.wav"))
	})
}

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

func TestCLIFzvEditLFOWave(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(fixturesDir(), "JUNGLISM Samples", "pad 1.wav")
	fzvPath := filepath.Join(dir, "pad.fzv")
	mustRun(t, "fzv", "import", wavPath, fzvPath)

	waveforms := []struct {
		name     string
		expected string
	}{
		{"sine", "Sine"},
		{"saw-up", "Saw Up"},
		{"saw-down", "Saw Down"},
		{"triangle", "Triangle"},
		{"rectangle", "Rectangle"},
		{"random", "Random"},
	}
	for _, wf := range waveforms {
		t.Run(wf.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "voice.fzv")
			mustRun(t, "fzv", "import", wavPath, p)
			mustRun(t, "fzv", "edit", p, "--lfo-wave", wf.name, "--lfo-rate", "10", "--lfo-filter", "25")
			out, _ := mustRun(t, "fzv", "info", p)
			if !strings.Contains(out, wf.expected) {
				t.Errorf("fzv info after --lfo-wave %s missing %q:\n%s", wf.name, wf.expected, out)
			}
		})
	}
	t.Run("invalid waveform fails", func(t *testing.T) {
		t.Parallel()
		out, serr := mustFail(t, "fzv", "edit", fzvPath, "--lfo-wave", "bogus")
		combined := out + serr
		if !strings.Contains(combined, "unknown waveform") {
			t.Errorf("expected 'unknown waveform' error:\n%s", combined)
		}
	})
}

func TestCLIFzvEditLFOSubFlags(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(fixturesDir(), "JUNGLISM Samples", "pad 1.wav")
	fzvPath := filepath.Join(dir, "pad.fzv")
	mustRun(t, "fzv", "import", wavPath, fzvPath)

	mustRun(t, "fzv", "edit", fzvPath,
		"--lfo-wave", "sine",
		"--lfo-rate", "20",
		"--lfo-delay", "100",
		"--lfo-attack", "64",
		"--lfo-pitch", "30",
		"--lfo-amp", "20",
		"--lfo-filter", "50",
		"--lfo-q", "10",
	)
	out, _ := mustRun(t, "fzv", "info", fzvPath)
	for _, want := range []string{
		"Rate: 20",
		"Attack: 64",
		"Delay: 100",
		"pitch=30",
		"amp=20",
		"filter=50",
		"q=10",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("fzv info after LFO sub-flag edit missing %q:\n%s", want, out)
		}
	}
}

func TestCLIFzvEditModulationKF(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(fixturesDir(), "JUNGLISM Samples", "amen 01.wav")
	fzvPath := filepath.Join(dir, "mod.fzv")
	mustRun(t, "fzv", "import", wavPath, fzvPath)

	mustRun(t, "fzv", "edit", fzvPath,
		"--dca-level-kf", "8",
		"--dca-rate-kf", "4",
		"--dcf-level-kf", "2",
		"--dcf-rate-kf", "1",
		"--vel-dca-kf", "80",
		"--vel-dcf-kf", "40",
	)
	out, _ := mustRun(t, "fzv", "info", fzvPath)
	for _, want := range []string{
		"level KF=+8",
		"rate KF=+4",
		"level KF=+2",
		"rate KF=+1",
		"vel sensitivity=+80",
		"vel sensitivity=+40",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("fzv info after modulation edit missing %q:\n%s", want, out)
		}
	}

	jsonOut, _ := mustRun(t, "fzv", "info", "--json", fzvPath)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &parsed); err != nil {
		t.Fatalf("fzv info --json not valid: %v", err)
	}
	checks := map[string]float64{
		"dca_level_kf": 64,
		"dca_rate_kf":  32,
		"dcf_level_kf": 16,
		"dcf_rate_kf":  8,
		"vel_dca_kf":   80,
		"vel_dcf_kf":   40,
	}
	for key, want := range checks {
		got, ok := parsed[key].(float64)
		if !ok || got != want {
			t.Errorf("JSON %s = %v, want %v", key, parsed[key], want)
		}
	}
}

// TestCLIFzvEditVelModulationSigned verifies the three signed
// initial-touch velocity modulation flags (vel-dcq-kf, vel-dca-rs,
// vel-dcf-rs) round-trip via the `fzv edit` -> `fzv info --json` path.
func TestCLIFzvEditVelModulationSigned(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wavPath := filepath.Join(fixturesDir(), "JUNGLISM Samples", "amen 01.wav")
	fzvPath := filepath.Join(dir, "velmod.fzv")
	mustRun(t, "fzv", "import", wavPath, fzvPath)

	mustRun(t, "fzv", "edit", fzvPath,
		"--vel-dcq-kf", "50",
		"--vel-dca-rs", "-50",
		"--vel-dcf-rs", "127",
	)
	out, _ := mustRun(t, "fzv", "info", fzvPath)
	for _, want := range []string{
		"dcq KF=+50",
		"dca RS=-50",
		"dcf RS=+127",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("fzv info after signed vel modulation edit missing %q:\n%s", want, out)
		}
	}

	jsonOut, _ := mustRun(t, "fzv", "info", "--json", fzvPath)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &parsed); err != nil {
		t.Fatalf("fzv info --json not valid: %v", err)
	}
	checks := map[string]float64{
		"vel_dcq_kf": 50,
		"vel_dca_rs": -50,
		"vel_dcf_rs": 127,
	}
	for key, want := range checks {
		got, ok := parsed[key].(float64)
		if !ok || got != want {
			t.Errorf("JSON %s = %v, want %v", key, parsed[key], want)
		}
	}
}

func TestCLIPadLFOVoiceInfo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "padlfo.fzf")
	mustRun(t, "disk", "get", fixtureImg("PAD-LFO.img"), "FULL-DATA-FZ", fzfPath)
	voicesDir := filepath.Join(dir, "voices")
	mustRun(t, "fzf", "unpack", fzfPath, voicesDir)

	padFZV := filepath.Join(voicesDir, "PAD.fzv")
	out, _ := mustRun(t, "fzv", "info", padFZV)

	for _, want := range []string{"PAD", "18000", "Sine", "Rate: 20", "filter=50"} {
		if !strings.Contains(out, want) {
			t.Errorf("fzv info PAD missing %q:\n%s", want, out)
		}
	}

	t.Run("json", func(t *testing.T) {
		t.Parallel()
		out, _ := mustRun(t, "fzv", "info", "--json", padFZV)
		var parsed map[string]any
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatalf("fzv info --json PAD is not valid JSON: %v\noutput: %s", err, out)
		}
		if rate, ok := parsed["sample_rate"].(float64); !ok || rate != 18000 {
			t.Errorf("expected sample_rate=18000, got %v", parsed["sample_rate"])
		}
	})
}

func TestCLISfzExport(t *testing.T) {
	t.Parallel()
	fzvHoover := extractVoiceViaCLI(t, fixtureImg("HOOVER.img"), "HOOVER")
	fzvStab := extractVoiceViaCLI(t, fixtureImg("STAB.img"), "STAB")
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "export-test.fzf")
	mustRun(t, "fzf", "build", fzfPath, fzvHoover, fzvStab)

	t.Run("produces SFZ and WAVs", func(t *testing.T) {
		outDir := filepath.Join(t.TempDir(), "out")
		mustRun(t, "sfz", "export", fzfPath, outDir)
		entries, _ := os.ReadDir(outDir)
		hasSFZ := false
		wavCount := 0
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".sfz") {
				hasSFZ = true
			}
			if strings.HasSuffix(e.Name(), ".wav") {
				wavCount++
			}
		}
		if !hasSFZ {
			t.Error("no .sfz file produced")
		}
		if wavCount != 2 {
			t.Errorf("expected 2 WAV files, got %d", wavCount)
		}
	})
	t.Run("missing file fails", func(t *testing.T) {
		mustFail(t, "sfz", "export", "/nonexistent.fzf", t.TempDir())
	})
	t.Run("no args fails", func(t *testing.T) {
		mustFail(t, "sfz", "export")
	})
	t.Run("with --name flag", func(t *testing.T) {
		outDir := filepath.Join(t.TempDir(), "named")
		mustRun(t, "sfz", "export", "--name", "mykit", fzfPath, outDir)
		if _, err := os.Stat(filepath.Join(outDir, "mykit.sfz")); err != nil {
			t.Errorf("expected mykit.sfz: %v", err)
		}
	})
}
