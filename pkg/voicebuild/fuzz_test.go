package voicebuild

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

// FuzzAssembleWithKeygroups exercises the sector layout math by feeding
// random but well-shaped voice slices into AssembleWithKeygroups. The fuzzer
// is bounded to small inputs (≤8 voices, ≤4 KB audio each); we only assert
// no-panic and that the output, when produced, is sector-aligned.
func FuzzAssembleWithKeygroups(f *testing.F) {
	// Seeds covering: minimum (1 voice), middle (4 voices), and varied sample
	// rate indices. nVoices, perVoiceSamples, rateMask (3 bits to rate index).
	f.Add(uint8(1), uint16(64), uint8(0))
	f.Add(uint8(2), uint16(0), uint8(1))
	f.Add(uint8(4), uint16(512), uint8(2))
	f.Add(uint8(8), uint16(1024), uint8(0))
	f.Add(uint8(1), uint16(4096), uint8(2))

	f.Fuzz(func(t *testing.T, nVoices uint8, samplesPerVoice uint16, rateMask uint8) {
		// Bound inputs to safe ranges; fuzz-injected values must not OOM the test.
		n := int(nVoices)%8 + 1
		samples := int(samplesPerVoice) % 2048
		rateIdx := rateMask % 3

		voices := make([][]byte, n)
		groups := make([]Keygroup, n)
		for i := range n {
			// Each voice is one sector header + samples*2 bytes of audio.
			// Stamp the audio with a per-voice sentinel byte so any
			// cross-voice shuffling in the sector packing is observable
			// after assembly.
			v := make([]byte, disk.SectorSize+samples*disk.BytesPerSample)
			binary.LittleEndian.PutUint32(v[disk.VoiceWaveStartOffset:], 0)
			binary.LittleEndian.PutUint32(v[disk.VoiceWaveEndOffset:], uint32(samples)) //nolint:gosec // bounded by modulo above
			binary.LittleEndian.PutUint32(v[disk.VoiceGenStartOffset:], 0)
			binary.LittleEndian.PutUint32(v[disk.VoiceGenEndOffset:], uint32(samples)) //nolint:gosec // bounded by modulo above
			v[disk.VoiceSampOffset] = rateIdx
			sentinel := byte(0x10 + i) //nolint:gosec // i < 8
			for j := disk.SectorSize; j < len(v); j++ {
				v[j] = sentinel
			}
			voices[i] = v
			//nolint:gosec // i < 8
			note := uint8(disk.FirstMIDINote + i)
			groups[i] = NewKeygroup(note, note, note)
		}

		out, err := AssembleWithKeygroups(voices, groups)
		if err != nil {
			// Errors are acceptable; just confirm no panic.
			return
		}
		if len(out)%disk.SectorSize != 0 {
			t.Errorf("output not sector-aligned: %d bytes (sector=%d)", len(out), disk.SectorSize)
		}
		if samples > 0 {
			want := samples * disk.BytesPerSample
			for i := range n {
				sentinel := byte(0x10 + i) //nolint:gosec // i < 8
				got := bytes.Count(out, []byte{sentinel})
				if got < want {
					t.Errorf("voice %d sentinel 0x%02x: count %d < %d (sample bytes)", i, sentinel, got, want)
				}
			}
		}
	})
}
