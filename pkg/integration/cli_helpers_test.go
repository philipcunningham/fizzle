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

// assertVoiceOutputs runs `fzf info --json` and checks the named voices
// carry the expected output strings, ignoring the rest. JSON survives
// renderer changes where rendered table glyphs don't.
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

func writeTestWAV(t *testing.T, path string, sampleRate uint32, nSamples int) {
	t.Helper()
	testutil.WriteTestWAV(t, path, sampleRate, nSamples)
}
