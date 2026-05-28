package voiceextract

import (
	"encoding/binary"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

func FuzzDecode(f *testing.F) {
	f.Add(make([]byte, disk.SectorSize))
	f.Add([]byte{})
	f.Add(make([]byte, 512))
	validHeader := make([]byte, disk.SectorSize+100)
	binary.LittleEndian.PutUint16(validHeader[disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	validHeader[disk.VoiceLoopSt0Offset+6] = 0x00
	f.Add(validHeader)
	f.Fuzz(func(t *testing.T, data []byte) {
		rate, samples, err := Decode(data)
		if err != nil {
			return
		}
		// On success the rate must be one of the three FZ-1 rates and the
		// returned slice must be non-nil (an empty slice is OK; a nil slice
		// with nil error would be a contract violation).
		if rate != disk.SampleRates[0] && rate != disk.SampleRates[1] && rate != disk.SampleRates[2] {
			t.Fatalf("Decode returned non-FZ sample rate %d", rate)
		}
		if samples == nil {
			t.Fatal("Decode returned nil samples with nil error")
		}
	})
}
