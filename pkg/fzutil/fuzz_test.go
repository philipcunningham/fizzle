package fzutil

import (
	"math"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

func FuzzResampleIdentity(f *testing.F) {
	f.Add(uint32(36000), []byte{0x00, 0x00, 0x01, 0x00, 0xff, 0x7f, 0x00, 0x80})
	f.Add(uint32(44100), []byte{0x80, 0x00, 0x7f, 0x00})
	f.Fuzz(func(t *testing.T, rate uint32, sampleBytes []byte) {
		if rate == 0 {
			return
		}
		nSamples := len(sampleBytes) / 2
		if nSamples == 0 || nSamples > 2000 {
			return
		}
		samples := make([]int16, nSamples)
		for i := range samples {
			samples[i] = bitconv.ReadInt16LE(sampleBytes[i*2:])
		}
		wf := &wav.File{SampleRate: rate, Samples: samples}
		out, err := Resample(wf, rate)
		if err != nil {
			t.Fatalf("Resample to same rate: %v", err)
		}
		if len(out) != len(samples) {
			t.Fatalf("length: got %d, want %d", len(out), len(samples))
		}
		for i, s := range out {
			if s != samples[i] {
				t.Errorf("sample[%d]: got %d, want %d", i, s, samples[i])
				break
			}
		}
	})
}

func FuzzResampleNeverExtrapolates(f *testing.F) {
	f.Add(uint32(36000), uint32(18000), []byte{0x00, 0x80, 0xff, 0x7f, 0x00, 0x00})
	f.Add(uint32(9000), uint32(36000), []byte{0x00, 0x00, 0x01, 0x00})
	f.Add(uint32(44100), uint32(22050), []byte{0x80, 0x00, 0x7f, 0x00, 0x00, 0x40})
	f.Fuzz(func(t *testing.T, srcRate, dstRate uint32, sampleBytes []byte) {
		if srcRate == 0 || dstRate == 0 {
			return
		}
		nSamples := len(sampleBytes) / 2
		if nSamples == 0 || nSamples > 2000 {
			return
		}
		samples := make([]int16, nSamples)
		lo := int16(math.MaxInt16)
		hi := int16(math.MinInt16)
		for i := range samples {
			samples[i] = bitconv.ReadInt16LE(sampleBytes[i*2:])
			if samples[i] < lo {
				lo = samples[i]
			}
			if samples[i] > hi {
				hi = samples[i]
			}
		}
		wf := &wav.File{SampleRate: srcRate, Samples: samples}
		out, err := Resample(wf, dstRate)
		if err != nil {
			return
		}
		for i, s := range out {
			if s < lo || s > hi {
				t.Errorf("sample[%d] = %d, outside input range [%d, %d]", i, s, lo, hi)
				break
			}
		}
	})
}

func FuzzVoiceNameBounds(f *testing.F) {
	f.Add("kick.wav")
	f.Add("")
	f.Add("../../foo/bar baz_qux-123.WAV")
	f.Add("EXACTLY12CHR.wav")
	f.Add("   ")
	f.Add("日本語.wav")
	f.Fuzz(func(t *testing.T, path string) {
		name := VoiceName(path)
		if len(name) == 0 {
			t.Error("VoiceName returned empty string")
		}
		if len(name) > 12 {
			t.Errorf("VoiceName(%q) = %q, length %d > 12", path, name, len(name))
		}
		for _, r := range name {
			isAlpha := r >= 'A' && r <= 'Z'
			isDigit := r >= '0' && r <= '9'
			if !isAlpha && !isDigit && r != ' ' {
				t.Errorf("VoiceName(%q) = %q, contains invalid rune %q", path, name, string(r))
				break
			}
		}
		if len(name) > 0 && name[len(name)-1] == ' ' {
			t.Errorf("VoiceName(%q) = %q, has trailing space", path, name)
		}
	})
}
