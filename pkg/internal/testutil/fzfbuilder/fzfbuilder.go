// Package fzfbuilder provides test helpers for building FZF files.
package fzfbuilder

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
)

// MakeTestFZF assembles a minimal FZF with the given voice names and writes it to a temp file.
func MakeTestFZF(t *testing.T, names []string) ([]byte, string) {
	t.Helper()
	n := len(names)
	voices := make([][]byte, n)
	groups := make([]voicebuild.Keygroup, n)
	for i, name := range names {
		v := make([]byte, disk.SectorSize+512*2)
		padded := disk.PadLabel(name)
		copy(v[disk.VoiceNameOffset:], padded[:])
		binary.LittleEndian.PutUint32(v[disk.VoiceWaveStartOffset:], 0)
		binary.LittleEndian.PutUint32(v[disk.VoiceWaveEndOffset:], 512)
		binary.LittleEndian.PutUint32(v[disk.VoiceGenStartOffset:], 0)
		binary.LittleEndian.PutUint32(v[disk.VoiceGenEndOffset:], 512)
		binary.LittleEndian.PutUint16(v[disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
		voices[i] = v
		note := uint8(disk.FirstMIDINote + i)
		groups[i] = voicebuild.NewKeygroup(note, note, note)
	}
	out, err := voicebuild.AssembleWithKeygroups(voices, groups)
	if err != nil {
		t.Fatal(err)
	}
	fzfPath := filepath.Join(t.TempDir(), "test.fzf")
	if err := os.WriteFile(fzfPath, out, 0644); err != nil {
		t.Fatal(err)
	}
	return out, fzfPath
}
