//go:build feature

package feature

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var (
	fizzleBin   string // compiled binary under test
	junglismImg string // a realistic disk generated from JUNGLISM.sfz
)

const (
	cols = 140
	rows = 40
)

func repoRoot() string { return filepath.Join("..", "..") }
func sfzPath() string  { return filepath.Join(repoRoot(), "testdata", "synthetic", "JUNGLISM.sfz") }
func sampleWav() string {
	return filepath.Join(repoRoot(), "testdata", "synthetic", "JUNGLISM Samples", "amen 01.wav")
}
func synthDir() string { return filepath.Join(repoRoot(), "testdata", "synthetic") }

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "fizzle-feature-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mktemp:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	if err := buildBinary(tmp); err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
	if err := buildJunglismImg(tmp); err != nil {
		fmt.Fprintln(os.Stderr, "fixture:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func buildBinary(tmp string) error {
	if bin := os.Getenv("FIZZLE_BIN"); bin != "" {
		fizzleBin = bin
		return nil
	}
	bin := filepath.Join(tmp, "fizzle")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/fizzle")
	cmd.Dir = repoRoot()
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	fizzleBin = bin
	return nil
}

// buildJunglismImg generates a single realistic disk from the committed
// JUNGLISM.sfz via the CLI (sfz convert --fit-to-disk -> disk new -> disk add).
func buildJunglismImg(tmp string) error {
	fzf := filepath.Join(tmp, "junglism.fzf")
	junglismImg = filepath.Join(tmp, "junglism.img")
	steps := [][]string{
		{"sfz", "convert", "--fit-to-disk", sfzPath(), fzf},
		{"disk", "new", "JUNGLISM", junglismImg},
		{"disk", "add", junglismImg, fzf},
	}
	for _, args := range steps {
		cmd := exec.Command(fizzleBin, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("fizzle %v: %w\n%s", args, err, out)
		}
	}
	return nil
}

// workspaceWith makes a fresh temp workspace dir containing copies of the
// given files (absolute paths). Returns the workspace dir.
func workspaceWith(t *testing.T, files ...string) string {
	t.Helper()
	ws := t.TempDir()
	for _, src := range files {
		copyInto(t, src, ws)
	}
	return ws
}

func copyInto(t *testing.T, src, destDir string) string {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	defer in.Close()
	dest := filepath.Join(destDir, filepath.Base(src))
	out, err := os.Create(dest)
	if err != nil {
		t.Fatalf("create %s: %v", dest, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy %s: %v", src, err)
	}
	return dest
}

func studio(t *testing.T, ws string) *Driver {
	return Start(t, fizzleBin, cols, rows, "studio", ws)
}

// --- M1: harness smoke -------------------------------------------------------

// TestLaunchAndQuit pins the harness: studio launches on a workspace, renders
// the browser, and quits cleanly on Ctrl-Q.
func TestLaunchAndQuit(t *testing.T) {
	ws := workspaceWith(t, junglismImg)
	d := studio(t, ws)
	d.WaitFor("Workspace")
	d.WaitFor("junglism.img")
	d.Send(kCtrlQ)
	d.WaitExit(3 * time.Second)
}

// assertDiskHasFullDump runs `fizzle disk ls` and checks the disk is non-empty
// with a Full Dump entry (i.e. the imported voice was actually persisted).
func assertDiskHasFullDump(t *testing.T, img string) {
	t.Helper()
	out, err := exec.Command(fizzleBin, "disk", "ls", img).CombinedOutput()
	if err != nil {
		t.Fatalf("disk ls %s: %v\n%s", img, err, out)
	}
	if !strings.Contains(string(out), "Full Dump") {
		t.Fatalf("saved disk has no Full Dump (voice not persisted):\n%s", out)
	}
}
