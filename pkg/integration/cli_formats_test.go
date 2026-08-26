//go:build integration

package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
