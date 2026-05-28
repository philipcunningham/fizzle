package voiceimport

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

// FuzzEncode feeds varied sample counts, rate indices, names, and loop
// configurations into Encode and asserts no panic and a sector-aligned
// output. The encoder must tolerate any rateIdx 0..2 and any sample length.
func FuzzEncode(f *testing.F) {
	// Seeds: minimal, typical, with-loop, with-transpose.
	f.Add(uint16(0), uint8(0), "TEST", int8(0), int32(-1), int32(-1))
	f.Add(uint16(1024), uint8(0), "DRUM", int8(0), int32(-1), int32(-1))
	f.Add(uint16(2048), uint8(1), "PAD", int8(12), int32(100), int32(500))
	f.Add(uint16(64), uint8(2), "", int8(-24), int32(-1), int32(-1))
	f.Add(uint16(8192), uint8(0), "VERY-LONG-NAME-OVERFLOWS", int8(24), int32(0), int32(7000))

	f.Fuzz(func(t *testing.T, nSamples uint16, rateRaw uint8, name string, transposeRaw int8, loopStart, loopEnd int32) {
		// Bound inputs.
		n := int(nSamples) % 16384
		rateIdx := rateRaw % 3
		transpose := int(transposeRaw) // already -128..127, safe for DCP semitone math
		samples := make([]int16, n)
		var loop LoopParams
		if loopStart >= 0 && loopEnd > loopStart && int(loopEnd) <= n {
			loop = LoopParams{LoopStart: int(loopStart), LoopEnd: int(loopEnd)}
		} else {
			loop = NoLoop()
		}

		out := Encode(samples, rateIdx, name, transpose, loop)
		if len(out)%disk.SectorSize != 0 {
			t.Errorf("output not sector-aligned: %d bytes (sector=%d)", len(out), disk.SectorSize)
		}
		// Must always include at least a header sector.
		if len(out) < disk.SectorSize {
			t.Errorf("output too small: %d bytes, want >= %d", len(out), disk.SectorSize)
		}
	})
}

// FuzzImport drives the full I/O path (WAV file -> Import -> FZV file) with
// arbitrary WAV input. This is the same surface where a SMPL-loop bug was
// shipped, missed by fuzzing because the I/O entry point had no fuzz at all
// (only the in-memory Encode helper did). Verifies that any WAV the parser
// accepts can be imported, that the resulting FZV parses back, and that
// SMPL loops in the source survive the rate scaling.
func FuzzImport(f *testing.F) {
	// Seed: minimal valid WAV, looped WAV at source rate, looped WAV that
	// will get resampled.
	f.Add(uint32(36000), uint32(36000), uint16(100), int32(-1), int32(-1))
	f.Add(uint32(36000), uint32(36000), uint16(1000), int32(100), int32(500))
	f.Add(uint32(44100), uint32(36000), uint16(2000), int32(200), int32(1500))
	f.Add(uint32(18000), uint32(18000), uint16(500), int32(50), int32(400))
	f.Add(uint32(9000), uint32(36000), uint16(50), int32(0), int32(40))

	f.Fuzz(func(t *testing.T, srcRate, dstRate uint32, nSamples uint16, loopStart, loopEnd int32) {
		// Bound rates so the writer accepts them and the resampler does not
		// drown the test in arithmetic.
		if srcRate == 0 || srcRate > 96000 {
			return
		}
		if dstRate != 36000 && dstRate != 18000 && dstRate != 9000 {
			return
		}
		n := int(nSamples)%5000 + 1
		samples := make([]int16, n)
		for i := range samples {
			samples[i] = int16(i % 1000)
		}
		wf := &wav.File{
			SampleRate: srcRate,
			Samples:    samples,
			LoopStart:  -1,
			LoopEnd:    -1,
		}
		if loopStart >= 0 && loopEnd > loopStart && int(loopEnd) <= n {
			wf.LoopStart = int(loopStart)
			wf.LoopEnd = int(loopEnd)
		}

		dir := t.TempDir()
		wavPath := filepath.Join(dir, "in.wav")
		var buf bytes.Buffer
		if err := wav.Write(&buf, wf); err != nil {
			return
		}
		if err := os.WriteFile(wavPath, buf.Bytes(), 0644); err != nil {
			t.Fatalf("WriteFile wav: %v", err)
		}
		fzvPath := filepath.Join(dir, "out.fzv")
		if err := Import(wavPath, fzvPath, dstRate); err != nil {
			return
		}
		params, err := fzvinfo.Parse(fzvPath)
		if err != nil {
			t.Fatalf("Import succeeded but fzvinfo.Parse rejected the output: %v", err)
		}
		if params.SampleRate != dstRate {
			t.Errorf("imported rate %d != requested %d", params.SampleRate, dstRate)
		}
		// SMPL-loop propagation: if the source WAV had a meaningful loop,
		// the imported FZV must carry a sustain loop with the rate-scaled
		// loop points. This is the regression guard for the bug fixed in
		// 560e833.
		if wf.LoopStart >= 0 {
			if !params.HasActiveLoop {
				t.Fatalf("source WAV had loop %d..%d, imported FZV has no loop", wf.LoopStart, wf.LoopEnd)
			}
			ratio := float64(dstRate) / float64(srcRate)
			wantStart := bitconv.NarrowU32(int(math.Round(float64(wf.LoopStart) * ratio)))
			wantEnd := bitconv.NarrowU32(int(math.Round(float64(wf.LoopEnd) * ratio)))
			nResampled := bitconv.NarrowU32(int(math.Round(float64(n) * ratio)))
			if wantEnd > nResampled {
				wantEnd = nResampled
			}
			if wantStart >= wantEnd {
				// scaling collapsed the loop; the importer turns it into
				// no-loop, which is a valid fallback.
				return
			}
			if params.LoopStart != wantStart {
				t.Errorf("LoopStart: got %d, want %d (src %d, ratio %f)",
					params.LoopStart, wantStart, wf.LoopStart, ratio)
			}
			if params.LoopEnd != wantEnd {
				t.Errorf("LoopEnd: got %d, want %d (src %d, ratio %f)",
					params.LoopEnd, wantEnd, wf.LoopEnd, ratio)
			}
		}
	})
}
