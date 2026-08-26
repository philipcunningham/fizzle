//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
