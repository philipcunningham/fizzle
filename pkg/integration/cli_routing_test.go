//go:build integration

package integration_test

import (
	"path/filepath"
	"strings"
	"testing"
)

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
