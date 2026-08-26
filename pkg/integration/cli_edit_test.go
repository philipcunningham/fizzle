//go:build integration

package integration_test

import (
	"path/filepath"
	"strings"
	"testing"
)

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
