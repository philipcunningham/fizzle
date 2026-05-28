package sfzconvert

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/logger"
)

// BenchmarkConvertJUNGLISM measures the end-to-end SFZ to FZF conversion of the
// 28-voice JUNGLISM fixture. This is the dominant user-visible workload.
func BenchmarkConvertJUNGLISM(b *testing.B) {
	// Silence INFO/WARN logs that otherwise dominate the benchmark wall-clock.
	defer logger.Silence()()

	sfzPath := filepath.Join("..", "..", "testdata", "synthetic", "JUNGLISM.sfz")
	dir := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		out := filepath.Join(dir, "out.fzf")
		if err := Convert(context.Background(), sfzPath, out, 36000, false); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkConvertJUNGLISMFitToDisk measures conversion with auto-downsample.
// Same input but invokes the rate-selection ladder.
func BenchmarkConvertJUNGLISMFitToDisk(b *testing.B) {
	defer logger.Silence()()

	sfzPath := filepath.Join("..", "..", "testdata", "synthetic", "JUNGLISM.sfz")
	dir := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		out := filepath.Join(dir, "out.fzf")
		if err := Convert(context.Background(), sfzPath, out, 36000, true); err != nil {
			b.Fatal(err)
		}
	}
}
