//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
