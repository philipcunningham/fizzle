package sfzconvert

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

const junglismSamplesDir = "../../testdata/synthetic/JUNGLISM Samples"
const minCorrelation = 0.95

// TestRoundTripAudioFidelityJunglism converts the JUNGLISM SFZ to an FZF,
// unpacks it, and verifies that each voice's audio is a faithful resampled
// version of the source WAV. This is the regression test for wrong audio block
// offsets during unpack (voices at 4-voice sector boundaries).
func TestRoundTripAudioFidelityJunglism(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "junglism.fzf")

	if err := Convert(context.Background(), junglismSFZ, fzfPath, 36000, false); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	// Voices in JUNGLISM order. Sector boundaries at indices 4, 8, 12, 16, 20, 24.
	// We test across all boundaries to catch the audio block offset bug.
	regions := []struct {
		name   string
		srcWAV string
	}{
		{"AMEN 01", "amen 01.wav"},
		{"AMEN 02", "amen 02.wav"},
		{"AMEN 03", "amen 03.wav"},
		{"AMEN 04", "amen 04.wav"},
		{"AMEN 05", "amen 05.wav"}, // boundary at index 4
		{"AMEN 06", "amen 06.wav"},
		{"AMEN 07", "amen 07.wav"},
		{"AMEN 08", "amen 08.wav"},
		{"THINK 01", "think 01.wav"}, // boundary at index 8
		{"THINK 02", "think 02.wav"},
		{"THINK 03", "think 03.wav"},
		{"THINK 04", "think 04.wav"},
		{"THINK 05", "think 05.wav"}, // boundary at index 12
		{"THINK 06", "think 06.wav"},
		{"THINK 07", "think 07.wav"},
		{"THINK 08", "think 08.wav"},
		{"THINK 09", "think 09.wav"}, // boundary at index 16
		{"808", "808.wav"},
		{"REESE", "reese.wav"},
		{"PAD 1", "pad 1.wav"},
		{"PAD 2", "pad 2.wav"}, // boundary at index 20
		{"PIANO", "piano.wav"},
		{"LEAD 1", "lead 1.wav"},
		{"VOX", "vox.wav"},
		{"RAGGA 1", "ragga 1.wav"}, // boundary at index 24
		{"RAGGA 2", "ragga 2.wav"},
	}

	for _, r := range regions {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			srcPath := filepath.Join(junglismSamplesDir, r.srcWAV)
			srcFile, err := os.Open(srcPath)
			if err != nil {
				t.Fatalf("opening source WAV: %v", err)
			}
			srcWAV, err := wav.Read(srcFile)
			srcFile.Close() //nolint:errcheck
			if err != nil {
				t.Fatalf("reading source WAV: %v", err)
			}

			fzvPath := filepath.Join(unpackDir, r.name+".fzv")
			fzvData, err := os.ReadFile(fzvPath)
			if err != nil {
				t.Fatalf("reading FZV %s: %v", r.name, err)
			}
			if len(fzvData) < 1024 {
				t.Fatalf("FZV too small")
			}
			waveEnd := int(binary.LittleEndian.Uint32(fzvData[0x04:0x08]))
			audioBytes := fzvData[1024 : 1024+waveEnd*2]
			got := make([]int16, waveEnd)
			for i := range got {
				got[i] = bitconv.ReadInt16LE(audioBytes[i*2:])
			}

			expected := resampleForTest(srcWAV.Samples, srcWAV.SampleRate, 36000)

			n := min(len(expected), len(got))
			corr := correlation(expected[:n], got[:n])
			if corr < minCorrelation {
				t.Errorf("audio mismatch (corr=%.4f < %.2f): wrong audio block read during unpack", corr, minCorrelation)
			}
		})
	}
}

// TestRoundTripReeseOneShotNoLoop verifies that the reese voice (which has
// loop_mode=ONE_SHOT in the SFZ and no WAV loop points) has no sustain or
// release loop after the convert/unpack cycle.
func TestRoundTripReeseOneShotNoLoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fzfPath := filepath.Join(dir, "junglism.fzf")

	if err := Convert(context.Background(), junglismSFZ, fzfPath, 36000, false); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	unpackDir := filepath.Join(dir, "voices")
	if err := voiceunpack.Unpack(fzfPath, unpackDir); err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	reeseFZV, err := os.ReadFile(filepath.Join(unpackDir, "REESE.fzv"))
	if err != nil {
		t.Fatalf("reading REESE.fzv: %v", err)
	}

	loopSus := reeseFZV[disk.VoiceLoopSusOffset]
	if loopSus != disk.NoSustainLoop {
		t.Errorf("reese loop_sus=%d, want %d (no sustain loop for ONE_SHOT)", loopSus, disk.NoSustainLoop)
	}
	loopEnd := reeseFZV[disk.VoiceLoopEndOffset]
	if loopEnd != disk.NoReleaseLoop {
		t.Errorf("reese loop_end=%d, want %d (no release loop for ONE_SHOT)", loopEnd, disk.NoReleaseLoop)
	}
}

func resampleForTest(samples []int16, srcRate, dstRate uint32) []int16 {
	ratio := float64(dstRate) / float64(srcRate)
	n := int(math.Round(float64(len(samples)) * ratio))
	out := make([]int16, n)
	for i := range out {
		pos := float64(i) / ratio
		lo := int(pos)
		hi := lo + 1
		if hi >= len(samples) {
			hi = len(samples) - 1
		}
		v := float64(samples[lo])*(1-pos+float64(lo)) + float64(samples[hi])*(pos-float64(lo))
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		out[i] = int16(v)
	}
	return out
}

func correlation(a, b []int16) float64 {
	var num, da, db float64
	for i := range a {
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
