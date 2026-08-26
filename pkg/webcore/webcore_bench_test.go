package webcore

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskfs"
	"github.com/philipcunningham/fizzle/pkg/logger"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
)

// benchVoices builds n voices of 7000 samples, keyed one per semitone, the
// shape of a fully loaded instrument.
func benchVoices(b *testing.B, n int) ([][]byte, []voicebuild.Keygroup) {
	b.Helper()
	voices := make([][]byte, n)
	groups := make([]voicebuild.Keygroup, n)
	for i := range voices {
		pcm := make([]int16, 7000)
		for j := range pcm {
			pcm[j] = int16(j % 157)
		}
		voices[i] = voiceimport.Encode(pcm, 1, fmt.Sprintf("V%02d", i), 0, voiceimport.NoLoop())
		note := uint8(24 + i) // #nosec G115 -- 24..87 fits a byte
		groups[i] = voicebuild.NewKeygroup(note, note, note)
	}
	return voices, groups
}

// benchSession opens a one disk document holding the assembled dump.
func benchSession(b *testing.B, voices [][]byte, groups []voicebuild.Keygroup) *Session {
	b.Helper()
	fzf, err := voicebuild.AssembleWithKeygroups(voices, groups)
	if err != nil {
		b.Fatal(err)
	}
	blank, err := diskformat.BuildImage("BENCH")
	if err != nil {
		b.Fatal(err)
	}
	img, err := disk.ReadImage(bytes.NewReader(blank))
	if err != nil {
		b.Fatal(err)
	}
	file, err := diskfs.FullDump(fzf, 0, len(voices))
	if err != nil {
		b.Fatal(err)
	}
	if err := diskfs.Add(img, fzf, file); err != nil {
		b.Fatal(err)
	}
	s := NewSession()
	if _, cerr := s.OpenImage(img.Bytes()); cerr != nil {
		b.Fatal(cerr)
	}
	return s
}

// BenchmarkSlotEditFullInstrument measures one scalar edit inside a
// gesture on a 64 voice instrument that nearly fills the disk: the knob
// drag hot path, where the UI fires an edit per pointer move. Each edit
// copies the image and rebuilds the snapshot.
func BenchmarkSlotEditFullInstrument(b *testing.B) {
	voices, groups := benchVoices(b, 64)
	s := benchSession(b, voices, groups)
	s.BeginGesture()

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		if _, cerr := s.SetSlotParamNumber(0, fieldCutoff, 40+i%60); cerr != nil {
			b.Fatal(cerr)
		}
	}
}

// BenchmarkRenameVoiceFullInstrument measures a fixed-size document operation
// on the same 64 voice near-full instrument.
func BenchmarkRenameVoiceFullInstrument(b *testing.B) {
	voices, groups := benchVoices(b, 64)
	s := benchSession(b, voices, groups)
	s.BeginGesture()
	names := [2]string{"RENAMED A", "RENAMED B"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		if _, cerr := s.RenameVoiceSlot(0, names[i%len(names)]); cerr != nil {
			b.Fatal(cerr)
		}
	}
}

// BenchmarkSlotEditSmallInstrument is the same edit on a 4 voice
// instrument: the difference against the 64 voice case is what the
// per-slot re-parse costs, as against the flat cost of copying the
// image.
func BenchmarkSlotEditSmallInstrument(b *testing.B) {
	voices, groups := benchVoices(b, 4)
	s := benchSession(b, voices, groups)
	s.BeginGesture()

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		if _, cerr := s.SetSlotParamNumber(0, fieldCutoff, 40+i%60); cerr != nil {
			b.Fatal(cerr)
		}
	}
}

// BenchmarkSlotEditSplitPair measures the same edit on a two disk
// document, where every edit also re-splits the dump across the pair.
func BenchmarkSlotEditSplitPair(b *testing.B) {
	// The split logs a line per disk; it would otherwise dominate.
	defer logger.Silence()()
	voices := make([][]byte, 3)
	groups := make([]voicebuild.Keygroup, 3)
	for i := range voices {
		pcm := make([]int16, 300000)
		for j := range pcm {
			pcm[j] = int16(j % 157)
		}
		voices[i] = voiceimport.Encode(pcm, 1, fmt.Sprintf("BIG%d", i), 0, voiceimport.NoLoop())
		note := uint8(40 + i) // #nosec G115 -- small test values
		groups[i] = voicebuild.NewKeygroup(note, note, note)
	}
	fzf, err := voicebuild.AssembleWithKeygroups(voices, groups)
	if err != nil {
		b.Fatal(err)
	}
	s := NewSession()
	if _, cerr := s.LoadFZF(fzf); cerr != nil {
		b.Fatal(cerr)
	}
	if s.Snapshot().Disk.Disks != 2 {
		b.Fatal("the fixture did not split")
	}
	s.BeginGesture()

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		if _, cerr := s.SetSlotParamNumber(0, fieldCutoff, 40+i%60); cerr != nil {
			b.Fatal(cerr)
		}
	}
}

// BenchmarkSnapshotFullInstrument isolates the snapshot rebuild from
// the patch: this is what an edit pays on top of the byte write.
func BenchmarkSnapshotFullInstrument(b *testing.B) {
	voices, groups := benchVoices(b, 64)
	s := benchSession(b, voices, groups)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if snap := s.Snapshot(); snap.Disk == nil {
			b.Fatal("no disk")
		}
	}
}
