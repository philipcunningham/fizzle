package wav_test

import (
	"bytes"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/wav"
)

func ExampleFile_Duration() {
	f := &wav.File{
		SampleRate: 44100,
		Samples:    make([]int16, 44100),
	}
	fmt.Printf("%.1f seconds\n", f.Duration())
	// Output:
	// 1.0 seconds
}

func ExampleWrite() {
	samples := []int16{0, 16383, 32767, 16383, 0, -16384, -32768, -16384}
	f := &wav.File{
		SampleRate: 8000,
		Samples:    samples,
		LoopStart:  -1,
		LoopEnd:    -1,
	}
	var buf bytes.Buffer
	if err := wav.Write(&buf, f); err != nil {
		panic(err)
	}
	fmt.Printf("wrote %d bytes\n", buf.Len())
	// Output:
	// wrote 60 bytes
}
