package fzbinfo

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
)

// FuzzParseFZB feeds arbitrary byte sequences to Parse and asserts the same
// internal-consistency invariants required of any successful parse.
func FuzzParseFZB(f *testing.F) {
	for _, names := range [][]string{
		{"A"},
		{"KICK", "SNARE", "HAT"},
		{"V1", "V2", "V3", "V4", "V5", "V6", "V7"},
	} {
		f.Add(buildFuzzFZB(names))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		path := filepath.Join(dir, "fuzz.fzb")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		info, err := Parse(path)
		if err != nil {
			return
		}
		if info == nil {
			t.Fatal("Parse returned nil info with nil error")
		}
		if info.VoiceCount < 0 || info.VoiceCount > disk.MaxVoices {
			t.Fatalf("VoiceCount %d outside [0, %d]", info.VoiceCount, disk.MaxVoices)
		}
		if len(info.Voices) > info.VoiceCount {
			t.Fatalf("len(Voices)=%d > VoiceCount=%d", len(info.Voices), info.VoiceCount)
		}
		seen := map[int]bool{}
		for _, v := range info.Voices {
			if v.Index < 0 || v.Index >= disk.MaxVoices {
				t.Fatalf("voice Index %d outside [0, %d)", v.Index, disk.MaxVoices)
			}
			if seen[v.Index] {
				t.Fatalf("duplicate voice Index %d", v.Index)
			}
			seen[v.Index] = true
			if v.KeyLow > v.KeyHigh {
				t.Fatalf("voice %d: KeyLow=%d > KeyHigh=%d", v.Index, v.KeyLow, v.KeyHigh)
			}
		}
		var buf bytes.Buffer
		Render(&buf, info)
		buf.Reset()
		if err := RenderJSON(&buf, info); err != nil {
			t.Fatalf("RenderJSON failed: %v", err)
		}
	})
}

// buildFuzzFZB synthesises a minimal valid bank dump for the seed corpus.
func buildFuzzFZB(names []string) []byte {
	n := len(names)
	voiceSectors := disk.VoiceAreaSectors(n)
	size := disk.SectorSize + voiceSectors*disk.SectorSize
	out := make([]byte, size)
	binary.LittleEndian.PutUint16(out[disk.BankVoiceCountOffset:], bitconv.NarrowU16(n))
	for i, name := range names {
		voff := disk.VoiceSlotOffset(disk.SectorSize, i)
		padded := disk.PadLabel(name)
		copy(out[voff+disk.VoiceNameOffset:], padded[:])
		binary.LittleEndian.PutUint16(out[voff+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	}
	return out
}
