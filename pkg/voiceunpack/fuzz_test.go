package voiceunpack

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
)

func FuzzUnpack(f *testing.F) {
	f.Add([]byte{})

	v := make([]byte, disk.SectorSize+512*2)
	padded := disk.PadLabel("VOICE")
	copy(v[disk.VoiceNameOffset:], padded[:])
	binary.LittleEndian.PutUint32(v[disk.VoiceWaveStartOffset:], 0)
	binary.LittleEndian.PutUint32(v[disk.VoiceWaveEndOffset:], 512)
	binary.LittleEndian.PutUint32(v[disk.VoiceGenStartOffset:], 0)
	binary.LittleEndian.PutUint32(v[disk.VoiceGenEndOffset:], 512)
	binary.LittleEndian.PutUint16(v[disk.VoiceLoopModeOffset:], disk.PlaybackModeNormal)
	note := uint8(disk.FirstMIDINote)
	kg := voicebuild.Keygroup{
		KeyLow: note, KeyHigh: note, VelLow: disk.DefaultVelLow, VelHigh: disk.DefaultVelHigh,
		KeyCentre: note, AudioOut: disk.PolyphonicAudioOut,
	}
	validFZF, err := voicebuild.AssembleWithKeygroups([][]byte{v}, []voicebuild.Keygroup{kg})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(validFZF)

	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		fzfPath := filepath.Join(dir, "test.fzf")
		os.WriteFile(fzfPath, data, 0644) //nolint:errcheck
		outDir := filepath.Join(dir, "out")
		if err := Unpack(fzfPath, outDir); err != nil {
			return
		}
		// If Unpack succeeded, every output file must look like a usable
		// FZV: large enough to hold the header sector, with the .fzv
		// extension the rest of the toolchain expects.
		entries, err := os.ReadDir(outDir)
		if err != nil {
			t.Fatalf("ReadDir after Unpack: %v", err)
		}
		for _, e := range entries {
			body, err := os.ReadFile(filepath.Join(outDir, e.Name()))
			if err != nil {
				t.Fatalf("ReadFile %s: %v", e.Name(), err)
			}
			if len(body) < disk.SectorSize {
				t.Fatalf("%s: unpacked size %d < SectorSize", e.Name(), len(body))
			}
			if !strings.HasSuffix(e.Name(), ".fzv") {
				t.Fatalf("%s: missing .fzv suffix", e.Name())
			}
		}
	})
}

// FuzzBuildUnpackRoundTrip asserts that voices fed through
// voicebuild.AssembleWithKeygroups and then voiceunpack.Unpack come back with
// their stable header fields and audio payloads intact (modulo the
// sector-padding that the unpacker applies). This is the strongest
// invariant the build/unpack pipeline must preserve.
func FuzzBuildUnpackRoundTrip(f *testing.F) {
	// Seeds: three distinct shapes covering 1, 2, and several voices, with
	// varied sample counts and names.
	f.Add([]byte{0x01, 0x40, 0x00, 'A', 'B', 'C'})
	f.Add([]byte{0x02, 0x80, 0x00, 'F', 'O', 'O', 'B', 'A', 'R'})
	f.Add([]byte{0x05, 0xFF, 0x07, 'X', 'Y', 'Z', 'Q', 'R', 'S'})

	f.Fuzz(func(t *testing.T, data []byte) {
		// Decode a voice count N in [1, 8] from the first byte.
		if len(data) == 0 {
			return
		}
		n := int(data[0])%8 + 1

		// Decode per-voice sample count clamped to [64, 2048] from the
		// next two bytes (treated as little-endian uint16, modulo range).
		samples := 64
		if len(data) >= 3 {
			raw := int(binary.LittleEndian.Uint16(data[1:3]))
			samples = 64 + raw%(2048-64+1)
		}

		// Decode per-voice name (3..6 ASCII letters, uppercase to dodge the
		// uppercasing/truncation rules in the build pipeline). Names must be
		// unique so the unpacker does not deduplicate them with "-N"
		// suffixes, which would break our round-trip identity check.
		var nameSeed []byte
		if len(data) > 3 {
			nameSeed = data[3:]
		}
		voices := make([][]byte, n)
		names := make([]string, n)
		groups := make([]voicebuild.Keygroup, n)
		seenName := make(map[string]bool, n)
		for i := range n {
			nm := genName(nameSeed, i)
			// Force uniqueness: append a base-26 letter suffix if needed.
			base := nm
			suffix := 0
			for seenName[nm] {
				nm = base + string(rune('A'+suffix%26))
				suffix++
				if len(nm) > disk.LabelSize {
					nm = nm[:disk.LabelSize]
				}
			}
			seenName[nm] = true
			names[i] = nm
			voices[i] = testutil.MakeTestVoice(nm, samples)
			note := uint8(disk.FirstMIDINote + i)
			groups[i] = voicebuild.NewKeygroup(note, note, note)
		}

		fzf, err := voicebuild.AssembleWithKeygroups(voices, groups)
		if err != nil {
			t.Fatalf("AssembleWithKeygroups: %v", err)
		}

		dir := t.TempDir()
		fzfPath := filepath.Join(dir, "fzf.fzf")
		if err := os.WriteFile(fzfPath, fzf, 0644); err != nil {
			t.Fatalf("write fzf: %v", err)
		}
		outDir := filepath.Join(dir, "out")
		if err := Unpack(fzfPath, outDir); err != nil {
			t.Fatalf("Unpack: %v", err)
		}

		entries, err := os.ReadDir(outDir)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		if len(entries) != n {
			t.Fatalf("unpacked %d files, want %d", len(entries), n)
		}

		// Sort entries by filename so the order is deterministic. Build a
		// lookup from expected name -> unpacked bytes.
		fileNames := make([]string, 0, len(entries))
		for _, e := range entries {
			fileNames = append(fileNames, e.Name())
		}
		sort.Strings(fileNames)

		byName := make(map[string][]byte, n)
		for _, fn := range fileNames {
			b, err := os.ReadFile(filepath.Join(outDir, fn))
			if err != nil {
				t.Fatalf("read %s: %v", fn, err)
			}
			if !strings.HasSuffix(fn, ".fzv") {
				t.Fatalf("unexpected output filename %q (no .fzv suffix)", fn)
			}
			// Pull the name out of the header rather than trusting the
			// filename; that is the invariant we want to check.
			if len(b) < disk.VoiceNameOffset+disk.LabelSize {
				t.Fatalf("%s: unpacked file too small (%d bytes)", fn, len(b))
			}
			gotName := disk.TrimPadded(b[disk.VoiceNameOffset : disk.VoiceNameOffset+disk.LabelSize])
			if _, dup := byName[gotName]; dup {
				t.Fatalf("duplicate unpacked voice name %q", gotName)
			}
			byName[gotName] = b
		}

		for i, want := range names {
			got, ok := byName[want]
			if !ok {
				t.Fatalf("voice %d: no unpacked file with name %q (have %v)", i, want, fileNames)
			}
			orig := voices[i]

			// Sample rate byte should survive untouched.
			if got[disk.VoiceSampOffset] != orig[disk.VoiceSampOffset] {
				t.Errorf("voice %q: sample-rate byte %#x != %#x",
					want, got[disk.VoiceSampOffset], orig[disk.VoiceSampOffset])
			}

			// Playback mode (loop mode) word should survive untouched.
			gotLoop := binary.LittleEndian.Uint16(got[disk.VoiceLoopModeOffset:])
			wantLoop := binary.LittleEndian.Uint16(orig[disk.VoiceLoopModeOffset:])
			if gotLoop != wantLoop {
				t.Errorf("voice %q: loop mode %#x != %#x", want, gotLoop, wantLoop)
			}

			// Wave-start pointer for a single-voice FZV should be zero
			// (the unpacker rewrites pointers as if each voice were alone).
			waveStart := binary.LittleEndian.Uint32(got[disk.VoiceWaveStartOffset:])
			if waveStart != 0 {
				t.Errorf("voice %q: wave start = %d, want 0", want, waveStart)
			}

			// Audio bytes after the header sector must match the original,
			// modulo trailing zero padding up to one sector.
			origAudio := orig[disk.SectorSize:]
			gotAudio := got[disk.SectorSize:]
			if len(gotAudio) < len(origAudio) {
				t.Errorf("voice %q: unpacked audio shorter (%d) than original (%d)",
					want, len(gotAudio), len(origAudio))
				continue
			}
			if len(gotAudio)-len(origAudio) >= disk.SectorSize {
				t.Errorf("voice %q: unpacked audio %d bytes longer than original %d (more than one sector)",
					want, len(gotAudio), len(origAudio))
			}
			if !bytes.Equal(gotAudio[:len(origAudio)], origAudio) {
				t.Errorf("voice %q: audio bytes differ", want)
			}
			// Any extra trailing bytes from sector padding must be zero.
			for j := len(origAudio); j < len(gotAudio); j++ {
				if gotAudio[j] != 0 {
					t.Errorf("voice %q: padding byte at offset %d = %#x, want 0",
						want, disk.SectorSize+j, gotAudio[j])
					break
				}
			}
		}
	})
}

// genName builds a stable 3-6 char uppercase ASCII name from seed bytes and an
// index. Falls back to a deterministic default when the seed is short.
func genName(seed []byte, idx int) string {
	// 3-6 chars, biased by idx so distinct voices get distinct names from
	// identical seeds.
	want := 3 + (idx+len(seed))%4
	out := make([]byte, want)
	for i := range out {
		var b byte
		if len(seed) > 0 {
			b = seed[(i+idx)%len(seed)]
		} else {
			b = byte((i + idx) & 0xff) //nolint:gosec // masked into byte range
		}
		out[i] = 'A' + b%26
	}
	return string(out)
}
