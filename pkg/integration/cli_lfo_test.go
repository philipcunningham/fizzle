//go:build integration

package integration_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

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
		"--lfo-sync", "on",
		"--lfo-pitch", "30",
		"--lfo-amp", "20",
		"--lfo-filter", "50",
	)
	out, _ := mustRun(t, "fzv", "info", fzvPath)
	for _, want := range []string{
		"Rate: 20",
		"Delay: 100",
		// The DELAY row writes the attack too: 18 - ceil(100/8) = 5. It
		// has no panel row, so the readout names it apart.
		"No panel row: attack=5",
		"(phase sync)",
		"pitch=30",
		"amp=20",
		"filter=50",
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

// TestCLIFzvEditVelModulationSigned round-trips the three signed
// initial-touch velocity modulation flags (vel-dcq-kf, vel-dca-rs, and
// vel-dcf-rs) from `fzv edit` through `fzv info --json`.
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
		"dcq KF=50",
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
