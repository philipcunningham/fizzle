package fzutil

import (
	"testing"

	"github.com/philipcunningham/fizzle/pkg/wav"
)

func makeBenchSamples(n int) []int16 {
	s := make([]int16, n)
	for i := range s {
		s[i] = int16((i * 37) & 0x7fff)
	}
	return s
}

// BenchmarkResamplePassthrough measures the hot path when source and target
// rates match: the function still allocates and copies, but no interpolation
// runs. Establishes the floor for Resample.
func BenchmarkResamplePassthrough(b *testing.B) {
	f := &wav.File{SampleRate: 36000, Samples: makeBenchSamples(36000)}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, err := Resample(f, 36000)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkResampleDownsample measures interpolation from 44.1 kHz to 36 kHz,
// the most common ratio for sample-library SFZ files.
func BenchmarkResampleDownsample(b *testing.B) {
	f := &wav.File{SampleRate: 44100, Samples: makeBenchSamples(44100)}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, err := Resample(f, 36000)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkResampleUpsample measures interpolation upward, exercising the
// path where outLen > inputLen.
func BenchmarkResampleUpsample(b *testing.B) {
	f := &wav.File{SampleRate: 22050, Samples: makeBenchSamples(22050)}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, err := Resample(f, 36000)
		if err != nil {
			b.Fatal(err)
		}
	}
}
