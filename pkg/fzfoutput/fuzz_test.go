package fzfoutput

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzfinfo"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
)

func FuzzParseOutputFlag(f *testing.F) {
	f.Add("all")
	f.Add("1")
	f.Add("1,3,5")
	f.Add("8")
	f.Add("")
	f.Add("0")
	f.Add("9")
	f.Add("1,2,3,4,5,6,7,8")
	f.Add("abc")
	f.Add("-1")
	f.Add("1,,2")
	f.Add(",")
	f.Add("all,1")
	f.Fuzz(func(t *testing.T, s string) {
		gchn, err := ParseOutputFlag(s)
		if err != nil {
			return
		}
		if gchn == 0 {
			t.Errorf("ParseOutputFlag(%q) = 0x00, want non-zero", s)
		}
		formatted := disk.FormatAudioOut(gchn)
		back, err := ParseOutputFlag(formatted)
		if err != nil {
			t.Errorf("re-parse FormatAudioOut(0x%02x) = %q error: %v", gchn, formatted, err)
		}
		if back != gchn {
			t.Errorf("round-trip: 0x%02x -> %q -> 0x%02x", gchn, formatted, back)
		}
	})
}

func FuzzFZFOutputChain(f *testing.F) {
	f.Add([]byte{3, 0, 5, 1, 10, 2, 15})
	f.Add([]byte{0, 0, 1, 3, 8, 5, 9, 7, 12})
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
		var lastGchn uint8
		for i := range iterations {
			voiceIdx := int(rest[i*2]) % nVoices
			b2 := rest[i*2+1]
			var gchn uint8
			if b2 < 8 {
				gchn = 1 << b2
			} else {
				gchn = 0xff
			}

			_, err := Set(path, []string{names[voiceIdx]}, false, gchn)
			if err != nil {
				t.Fatalf("Set(%q, gchn=0x%02x): %v", names[voiceIdx], gchn, err)
			}

			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			got := raw[disk.BankAudioOutOffset+voiceIdx]
			if got != gchn {
				t.Fatalf("voice %d raw byte: got 0x%02x, want 0x%02x", voiceIdx, got, gchn)
			}

			lastVoiceIdx, lastGchn = voiceIdx, gchn
		}

		if _, err := fzfinfo.Parse(path); err != nil {
			t.Fatalf("fzfinfo.Parse after mutations: %v", err)
		}

		if iterations > 0 {
			snap1, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile snap1: %v", err)
			}
			if _, err := Set(path, []string{names[lastVoiceIdx]}, false, lastGchn); err != nil {
				t.Fatalf("idempotency Set(%q, gchn=0x%02x): %v", names[lastVoiceIdx], lastGchn, err)
			}
			snap2, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile snap2: %v", err)
			}
			if !bytes.Equal(snap1, snap2) {
				t.Fatalf("idempotency violation: re-applying Set(%q, gchn=0x%02x) changed file bytes", names[lastVoiceIdx], lastGchn)
			}
		}
	})
}
