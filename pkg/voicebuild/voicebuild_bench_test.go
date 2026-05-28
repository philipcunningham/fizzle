package voicebuild

import (
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
)

func benchSamples(n int) []int16 {
	s := make([]int16, n)
	for i := range s {
		s[i] = int16((i * 37) & 0x7fff)
	}
	return s
}

// BenchmarkAssembleWithKeygroups measures the final FZF assembly: bank
// sector + voice headers + per-voice audio blocks. 8 voices × 1s at 36 kHz
// approximates a small-to-mid drum kit.
func BenchmarkAssembleWithKeygroups(b *testing.B) {
	const n = 8
	voices := make([][]byte, n)
	groups := make([]Keygroup, n)
	for i := range n {
		voices[i] = voiceimport.Encode(benchSamples(36000), 0, "V", 0, voiceimport.NoLoop())
		note := uint8(disk.FirstMIDINote + i)
		groups[i] = Keygroup{
			KeyLow: note, KeyHigh: note, VelLow: disk.DefaultVelLow,
			VelHigh: disk.DefaultVelHigh, KeyCentre: note,
			AudioOut: disk.PolyphonicAudioOut,
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, err := AssembleWithKeygroups(voices, groups)
		if err != nil {
			b.Fatal(err)
		}
	}
}
