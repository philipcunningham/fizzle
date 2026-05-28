package wav

import (
	"bytes"
	"io"
	"testing"
)

const benchSampleCount = 36000 // one second at 36 kHz; representative voice block

func benchSamples(n int) []int16 {
	s := make([]int16, n)
	for i := range s {
		s[i] = int16((i * 37) & 0x7fff)
	}
	return s
}

func benchWAV16Bit(n int) []byte {
	var buf bytes.Buffer
	f := &File{SampleRate: 36000, Samples: benchSamples(n), LoopStart: -1, LoopEnd: -1}
	if err := Write(&buf, f); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// BenchmarkRead16Bit measures the per-sample decode loop on a 36 kHz voice.
func BenchmarkRead16Bit(b *testing.B) {
	data := benchWAV16Bit(benchSampleCount)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, err := Read(bytes.NewReader(data))
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWrite measures the reflection-heavy encoder. wav.Write currently
// uses binary.Write on the riffHeader struct and the entire []int16 slice;
// both are reflection-based and dominate this benchmark.
func BenchmarkWrite(b *testing.B) {
	f := &File{SampleRate: 36000, Samples: benchSamples(benchSampleCount), LoopStart: -1, LoopEnd: -1}
	b.SetBytes(int64(benchSampleCount * 2))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := Write(io.Discard, f); err != nil {
			b.Fatal(err)
		}
	}
}
