package integration_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/disklist"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"

	"github.com/philipcunningham/fizzle/pkg/sfzconvert"
	"github.com/philipcunningham/fizzle/pkg/voiceextract"

	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

func TestSFZFullPipeline(t *testing.T) {
	skipShort(t)
	t.Parallel()

	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "junglism.fzf")
	imgPath := filepath.Join(dir, "junglism.img")
	unpackDir := filepath.Join(dir, "voices")

	if err := sfzconvert.Convert(context.Background(), junglismSFZ, fzfPath, 9000, false); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	if err := diskformat.Format(imgPath, "JUNGLISM"); err != nil {
		t.Fatal(err)
	}
	if err := diskadd.Add(imgPath, fzfPath, 0); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := disklist.List(imgPath, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "FULL-DATA-FZ") {
		t.Errorf("disk ls missing FULL-DATA-FZ: %s", buf.String())
	}

	// The FZF must come back off the disk byte-identical.
	gotFZFPath := filepath.Join(dir, "got.fzf")
	if err := diskget.Get(imgPath, "FULL-DATA-FZ", gotFZFPath); err != nil {
		t.Fatalf("disk get: %v", err)
	}
	orig, err := os.ReadFile(fzfPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(gotFZFPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(orig, got) {
		t.Errorf("FZF retrieved from disk differs from original (%d vs %d bytes)", len(orig), len(got))
	}

	if err := voiceunpack.Unpack(gotFZFPath, unpackDir); err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	entries, err := os.ReadDir(unpackDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 26 {
		t.Errorf("expected 26 voices, got %d", len(entries))
	}

	// Audio fidelity across sector boundaries, which fall at voice
	// indices 4, 8, 12, 16, 20, and 24.
	checkVoices := []struct {
		fzvName string
		srcWAV  string
	}{
		{"AMEN 01.fzv", "amen 01.wav"},
		{"AMEN 05.fzv", "amen 05.wav"},   // boundary at index 4
		{"THINK 01.fzv", "think 01.wav"}, // boundary at index 8
		{"THINK 05.fzv", "think 05.wav"}, // boundary at index 12
		{"808.fzv", "808.wav"},           // boundary at index 16 (808 is index 17, close enough)
		{"REESE.fzv", "reese.wav"},       // loop voice (verify waveStart=0)
	}

	for _, cv := range checkVoices {
		t.Run(cv.fzvName, func(t *testing.T) {
			t.Parallel()
			fzvPath := filepath.Join(unpackDir, cv.fzvName)
			if _, err := os.Stat(fzvPath); os.IsNotExist(err) {
				t.Fatalf("%s not found in unpack output", cv.fzvName)
			}

			fzvData, err := os.ReadFile(fzvPath)
			if err != nil {
				t.Fatal(err)
			}

			waveStart := binary.LittleEndian.Uint32(fzvData[0x00:0x04])
			if waveStart != 0 {
				t.Errorf("waveStart = %d, want 0 (cumulative offset not subtracted)", waveStart)
			}

			wavPath := filepath.Join(dir, cv.fzvName+".wav")
			if err := voiceextract.Extract(fzvPath, wavPath); err != nil {
				t.Fatalf("Extract: %v", err)
			}

			srcPath := filepath.Join(junglismSamplesDir, cv.srcWAV)
			srcF, err := os.Open(srcPath)
			if err != nil {
				t.Skipf("source WAV not available: %v", err)
			}
			srcWAV, err := wav.Read(srcF)
			srcF.Close() //nolint:errcheck
			if err != nil {
				t.Fatal(err)
			}

			gotF, err := os.Open(wavPath)
			if err != nil {
				t.Fatal(err)
			}
			gotWAV, err := wav.Read(gotF)
			gotF.Close() //nolint:errcheck
			if err != nil {
				t.Fatal(err)
			}

			expected := resampleForIntegration(srcWAV.Samples, srcWAV.SampleRate, 9000)
			corr := correlationForIntegration(expected, gotWAV.Samples)
			if corr < 0.95 {
				t.Errorf("audio mismatch: corr=%.4f (want ≥0.95). Wrong audio block?", corr)
			}
		})
	}

	// DCA/DCF defaults on a generated voice.
	fzvPath := filepath.Join(unpackDir, entries[0].Name())
	vp, err := fzvinfo.Parse(fzvPath)
	if err != nil {
		t.Fatalf("fzvinfo.Parse(%s): %v", entries[0].Name(), err)
	}
	if !vp.DCADefault {
		t.Errorf("voice %s: DCADefault=false, want true", entries[0].Name())
	}
	if !vp.DCFDefault {
		t.Errorf("voice %s: DCFDefault=false, want true", entries[0].Name())
	}

	// reese is one-shot, so it carries no sustain loop.
	reeseFZV, err := os.ReadFile(filepath.Join(unpackDir, "REESE.fzv"))
	if err == nil {
		loopSus := reeseFZV[0x12]
		if loopSus != 8 {
			t.Errorf("reese loop_sus=%d, want 8 (no sustain loop for one_shot)", loopSus)
		}
	}
}

func resampleForIntegration(samples []int16, srcRate, dstRate uint32) []int16 {
	n := int(math.Round(float64(len(samples)) * float64(dstRate) / float64(srcRate)))
	out := make([]int16, n)
	sr := int64(srcRate)
	dr := int64(dstRate)
	srcLen := len(samples)
	for i := range out {
		num := int64(i) * sr
		lo := int(num / dr)
		rem := num % dr
		hi := lo + 1
		if hi >= srcLen {
			hi = srcLen - 1
		}
		a := int64(samples[lo])
		b := int64(samples[hi])
		out[i] = int16(a + (b-a)*rem/dr) //nolint:gosec // G115: test value fits target type
	}
	return out
}

func correlationForIntegration(a, b []int16) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var num, da, db float64
	for i := range n {
		fa, fb := float64(a[i]), float64(b[i])
		num += fa * fb
		da += fa * fa
		db += fb * fb
	}
	denom := math.Sqrt(da) * math.Sqrt(db)
	if denom < 1e-10 {
		return 0
	}
	return num / denom
}

// TECHNO.img tests (real hardware image with multi-bank full dump).

// TestTechnoDiskStructure verifies that a real multi-bank FZF from hardware
// is read correctly, particularly that the 8 bank sectors are detected and
// the voice area is found at the right offset.
