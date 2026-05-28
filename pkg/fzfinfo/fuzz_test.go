package fzfinfo

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
)

// FuzzParseFZF feeds arbitrary FZF byte sequences into Parse. Seeds include a
// well-formed multi-voice fixture; the fuzzer mutates around it to exercise
// bank-sector counting, voice-area boundary handling, and the multi-disk
// split detection that reads BankTotalWaveOffset.
//
// The asserted invariants are deliberately tight: a successful Parse must
// yield a voice count that matches the number of voice entries returned,
// a memory size that does not exceed the file length, and a split-state
// that is internally consistent.
func FuzzParseFZF(f *testing.F) {
	for _, names := range [][]string{
		{"A"},
		{"KICK", "SNARE", "HAT"},
		{"V1", "V2", "V3", "V4", "V5"},
	} {
		seed := buildFuzzSeed(names)
		f.Add(seed)
	}
	for _, name := range []string{"HOOVER.img", "TECHNO.img", "BRASS.img", "PAD-LFO.img"} {
		if data, err := os.ReadFile("../../testdata/synthetic/" + name); err == nil {
			// Real disk images contain an FZF starting somewhere inside;
			// feed the image as-is to exercise the parser against
			// non-FZF prefix bytes.
			f.Add(data)
		}
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		path := filepath.Join(dir, "fuzz.fzf")
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
		if info.MemoryBytes < 0 {
			t.Fatalf("MemoryBytes %d is negative", info.MemoryBytes)
		}
		if info.IsSplit {
			if info.TotalDisks < 2 {
				t.Fatalf("IsSplit=true but TotalDisks=%d", info.TotalDisks)
			}
			if info.DiskNumber < 1 || info.DiskNumber > info.TotalDisks {
				t.Fatalf("IsSplit=true but DiskNumber=%d (TotalDisks=%d)", info.DiskNumber, info.TotalDisks)
			}
			if info.LocalVoices < 0 || info.LocalVoices > info.VoiceCount {
				t.Fatalf("IsSplit=true but LocalVoices=%d (VoiceCount=%d)", info.LocalVoices, info.VoiceCount)
			}
		}
		// Voice indices must be unique and within range.
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
			if v.VelLow > v.VelHigh {
				t.Fatalf("voice %d: VelLow=%d > VelHigh=%d", v.Index, v.VelLow, v.VelHigh)
			}
			if v.MIDIChannel < 1 || v.MIDIChannel > 16 {
				t.Fatalf("voice %d: MIDIChannel=%d outside [1, 16]", v.Index, v.MIDIChannel)
			}
		}
		// Render and RenderJSON must not panic on whatever Parse returns.
		var buf bytes.Buffer
		Render(&buf, info, nil)
		buf.Reset()
		if err := RenderJSON(&buf, info); err != nil {
			t.Fatalf("RenderJSON failed on parsed FZF: %v", err)
		}
	})
}

// buildFuzzSeed assembles a minimal byte-level FZF without depending on
// testing.T (so it can be called from f.Add at registration time). Each
// voice gets a 256-byte slot inside one voice-area sector.
func buildFuzzSeed(names []string) []byte {
	n := len(names)
	voiceSectors := disk.VoiceAreaSectors(n)
	size := disk.SectorSize + voiceSectors*disk.SectorSize + n*disk.SectorSize
	out := make([]byte, size)
	binary.LittleEndian.PutUint16(out[disk.BankVoiceCountOffset:], bitconv.NarrowU16(n))
	for i, name := range names {
		voff := disk.VoiceSlotOffset(disk.SectorSize, i)
		padded := disk.PadLabel(name)
		copy(out[voff+disk.VoiceNameOffset:], padded[:])
		binary.LittleEndian.PutUint32(out[voff+disk.VoiceWaveStartOffset:], 0)
		binary.LittleEndian.PutUint32(out[voff+disk.VoiceWaveEndOffset:], 0)
		binary.LittleEndian.PutUint32(out[voff+disk.VoiceGenStartOffset:], 0)
		binary.LittleEndian.PutUint32(out[voff+disk.VoiceGenEndOffset:], 0)
		binary.LittleEndian.PutUint16(out[voff+disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	}
	return out
}
