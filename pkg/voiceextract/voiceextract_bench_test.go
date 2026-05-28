package voiceextract

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/voiceimport"
)

func benchSamples(n int) []int16 {
	s := make([]int16, n)
	for i := range s {
		s[i] = int16((i * 37) & 0x7fff)
	}
	return s
}

// BenchmarkDecode measures the per-sample int16 read loop over a 1-second
// FZV at 36 kHz.
func BenchmarkDecode(b *testing.B) {
	data := voiceimport.Encode(benchSamples(36000), 0, "BENCH", 0, voiceimport.NoLoop())
	b.SetBytes(int64(36000 * 2))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, _, err := Decode(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecodePlaybackRange measures the trimmed-playback decode used by
// `fzv play` and the studio TUI.
func BenchmarkDecodePlaybackRange(b *testing.B) {
	data := voiceimport.Encode(benchSamples(36000), 0, "BENCH", 0, voiceimport.NoLoop())
	b.SetBytes(int64(36000 * 2))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, _, err := DecodePlaybackRange(data, 0)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkExtract measures the full FZV-to-WAV pipeline: file read, decode,
// WAV encode, and atomic write. One second of audio at 36 kHz.
func BenchmarkExtract(b *testing.B) {
	data := voiceimport.Encode(benchSamples(36000), 0, "BENCH", 0, voiceimport.NoLoop())
	dir := b.TempDir()
	fzvPath := filepath.Join(dir, "bench.fzv")
	wavPath := filepath.Join(dir, "bench.wav")
	if err := os.WriteFile(fzvPath, data, 0o644); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(36000 * 2))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := Extract(fzvPath, wavPath); err != nil {
			b.Fatal(err)
		}
	}
}
