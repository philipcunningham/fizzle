package wav

import (
	"bytes"
	"os"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
)

func FuzzRead(f *testing.F) {
	f.Add([]byte("RIFF\x00\x00\x00\x00WAVE"))
	f.Add([]byte{})
	f.Add([]byte("RIFF\x04\x00\x00\x00WAVE"))
	f.Add([]byte("RIFF\xff\xff\xff\xffWAVE"))
	f.Add([]byte("RIFF\x24\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x80\x8c\x00\x00\x00\x19\x01\x00\x02\x00\x10\x00data\x00\x00\x00\x00"))
	for _, name := range []string{"reese.wav", "amen 01.wav", "808.wav", "pad 1.wav"} {
		if data, err := os.ReadFile("../../testdata/synthetic/JUNGLISM Samples/" + name); err == nil {
			f.Add(data)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		f, err := Read(bytes.NewReader(data))
		if err != nil {
			return
		}
		var buf bytes.Buffer
		if err := Write(&buf, f); err != nil {
			t.Fatalf("Write after successful Read: %v", err)
		}
		back, err := Read(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("Read after Write: %v", err)
		}
		if back.SampleRate != f.SampleRate {
			t.Errorf("SampleRate drift: %d -> %d", f.SampleRate, back.SampleRate)
		}
		if len(back.Samples) != len(f.Samples) {
			t.Fatalf("sample-count drift: %d -> %d", len(f.Samples), len(back.Samples))
		}
		for i := range f.Samples {
			if back.Samples[i] != f.Samples[i] {
				t.Errorf("sample[%d]: %d -> %d", i, f.Samples[i], back.Samples[i])
				break
			}
		}
		if (f.LoopStart < 0) != (back.LoopStart < 0) {
			t.Errorf("loop presence changed: %d -> %d", f.LoopStart, back.LoopStart)
		}
		if f.LoopStart >= 0 && f.LoopStart != back.LoopStart {
			t.Errorf("loop_start drift: %d -> %d", f.LoopStart, back.LoopStart)
		}
		if f.LoopEnd >= 0 && f.LoopEnd != back.LoopEnd {
			t.Errorf("loop_end drift: %d -> %d", f.LoopEnd, back.LoopEnd)
		}
	})
}

// FuzzWriteReadRoundTripWithSMPL exercises the SMPL-chunk path of the
// reader and writer. The earlier asymmetry where Read accepted degenerate
// loops that Write silently dropped landed a real bug in voiceimport.Import;
// fuzzing this combination guards against the same class returning.
func FuzzWriteReadRoundTripWithSMPL(f *testing.F) {
	f.Add(uint32(36000), uint16(100), int32(10), int32(50))
	f.Add(uint32(18000), uint16(50), int32(0), int32(49))
	f.Add(uint32(44100), uint16(2000), int32(100), int32(1900))
	f.Add(uint32(36000), uint16(100), int32(-1), int32(-1))
	f.Fuzz(func(t *testing.T, rate uint32, nSamples uint16, loopStart, loopEnd int32) {
		if rate == 0 || rate > 96000 {
			return
		}
		n := int(nSamples)%5000 + 1
		samples := make([]int16, n)
		for i := range samples {
			samples[i] = int16(i % 1000)
		}
		ls, le := -1, -1
		if loopStart >= 0 && loopEnd > loopStart && int(loopEnd) <= n {
			ls = int(loopStart)
			le = int(loopEnd)
		}
		original := &File{
			SampleRate: rate,
			Samples:    samples,
			LoopStart:  ls,
			LoopEnd:    le,
		}
		var buf bytes.Buffer
		if err := Write(&buf, original); err != nil {
			t.Fatalf("Write: %v", err)
		}
		decoded, err := Read(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("Read after Write: %v", err)
		}
		if decoded.LoopStart != ls {
			t.Errorf("LoopStart: got %d, want %d", decoded.LoopStart, ls)
		}
		if decoded.LoopEnd != le {
			t.Errorf("LoopEnd: got %d, want %d", decoded.LoopEnd, le)
		}
	})
}

func FuzzWriteReadRoundTrip(f *testing.F) {
	f.Add(uint32(44100), []byte{0x00, 0x00, 0x01, 0x00, 0xff, 0x7f})
	f.Add(uint32(36000), []byte{0x80, 0x00})
	f.Add(uint32(9000), []byte{0x00, 0x80, 0xff, 0x7f, 0x00, 0x00, 0x01, 0x00})
	f.Fuzz(func(t *testing.T, rate uint32, sampleBytes []byte) {
		if rate == 0 || rate > 96000 {
			return
		}
		nSamples := len(sampleBytes) / 2
		if nSamples == 0 || nSamples > 5000 {
			return
		}
		samples := make([]int16, nSamples)
		for i := range samples {
			samples[i] = bitconv.ReadInt16LE(sampleBytes[i*2:])
		}
		original := &File{
			SampleRate: rate,
			Samples:    samples,
			LoopStart:  -1,
			LoopEnd:    -1,
		}
		var buf bytes.Buffer
		if err := Write(&buf, original); err != nil {
			return
		}
		decoded, err := Read(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("Read after Write: %v", err)
		}
		if decoded.SampleRate != original.SampleRate {
			t.Errorf("SampleRate: got %d, want %d", decoded.SampleRate, original.SampleRate)
		}
		if len(decoded.Samples) != len(original.Samples) {
			t.Fatalf("Samples length: got %d, want %d", len(decoded.Samples), len(original.Samples))
		}
		for i, s := range decoded.Samples {
			if s != original.Samples[i] {
				t.Errorf("Sample[%d]: got %d, want %d", i, s, original.Samples[i])
				break
			}
		}
	})
}
