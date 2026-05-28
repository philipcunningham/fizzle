package voiceimport

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/wav"
)

func benchSamples(n int) []int16 {
	s := make([]int16, n)
	for i := range s {
		s[i] = int16((i * 37) & 0x7fff)
	}
	return s
}

// BenchmarkEncode measures the per-voice encode path: binary.Write reflection
// on the 192-byte voiceHeader struct plus the per-sample int16 write loop.
// One second of audio at 36 kHz; representative of a typical FZ voice.
func BenchmarkEncode(b *testing.B) {
	samples := benchSamples(36000)
	b.SetBytes(int64(len(samples) * 2))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = Encode(samples, 0, "BENCH", 0, NoLoop())
	}
}

// BenchmarkImport measures the full WAV-to-FZV pipeline: file read, resample
// from 44100 to 36000 Hz, encode, and atomic write. One second of audio.
func BenchmarkImport(b *testing.B) {
	samples := benchSamples(44100)
	dir := b.TempDir()
	wavPath := filepath.Join(dir, "bench.wav")
	fzvPath := filepath.Join(dir, "bench.fzv")
	var buf bytes.Buffer
	if err := wav.Write(&buf, &wav.File{SampleRate: 44100, Samples: samples, LoopStart: -1, LoopEnd: -1}); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(wavPath, buf.Bytes(), 0o644); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(samples) * 2))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := Import(wavPath, fzvPath, 36000); err != nil {
			b.Fatal(err)
		}
	}
}
