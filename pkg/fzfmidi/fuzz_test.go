package fzfmidi

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzfinfo"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
)

func FuzzFZFMidiChain(f *testing.F) {
	f.Add([]byte{3, 0, 5, 1, 10, 2, 15})
	f.Add([]byte{0, 0, 1, 3, 8, 5, 16, 7, 12})
	f.Add([]byte{6, 2, 3, 4, 4, 0, 0, 1, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 1 {
			return
		}
		nVoices := int(data[0])%7 + 2
		names := make([]string, nVoices)
		for i := range nVoices {
			names[i] = fmt.Sprintf("V%d", i+1)
		}
		_, path := fzfbuilder.MakeTestFZF(t, names)

		rest := data[1:]
		iterations := len(rest) / 2
		if iterations > 100 {
			iterations = 100
		}
		var lastVoiceIdx int
		var lastChannel uint8
		for i := range iterations {
			voiceIdx := int(rest[i*2]) % nVoices
			channel := rest[i*2+1]%16 + 1

			_, err := Set(path, []string{names[voiceIdx]}, false, channel)
			if err != nil {
				t.Fatalf("Set(%q, channel=%d): %v", names[voiceIdx], channel, err)
			}

			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			got := raw[disk.BankMIDIRecvChanOffset+voiceIdx]
			if got != channel-1 {
				t.Fatalf("voice %d raw byte: got %d, want %d", voiceIdx, got, channel-1)
			}

			lastVoiceIdx, lastChannel = voiceIdx, channel
		}

		if _, err := fzfinfo.Parse(path); err != nil {
			t.Fatalf("fzfinfo.Parse after mutations: %v", err)
		}

		if iterations > 0 {
			snap1, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile snap1: %v", err)
			}
			if _, err := Set(path, []string{names[lastVoiceIdx]}, false, lastChannel); err != nil {
				t.Fatalf("idempotency Set(%q, channel=%d): %v", names[lastVoiceIdx], lastChannel, err)
			}
			snap2, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile snap2: %v", err)
			}
			if !bytes.Equal(snap1, snap2) {
				t.Fatalf("idempotency violation: re-applying Set(%q, channel=%d) changed file bytes", names[lastVoiceIdx], lastChannel)
			}
		}
	})
}
