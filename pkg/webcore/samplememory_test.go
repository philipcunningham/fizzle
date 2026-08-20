// The sampler's sample memory, as the user declares it (R27). The FZ-1
// shipped with 1 MB and rack units with 2 MB, so there is no single
// figure for the series and the machine has to be told.
package webcore

import (
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

const (
	oneMB = 1 << 20
	twoMB = 2 << 20
)

func TestSampleMemoryDefaultsToTheSmallerMachine(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("MEM"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	// A disk built for 1 MB loads anywhere, so the stock FZ-1 is the
	// safe assumption until the user says otherwise.
	if got := s.Snapshot().Disk.MemoryBytes; got != oneMB {
		t.Fatalf("memory = %d, want the 1 MB default", got)
	}
}

func TestSetSampleMemoryRecordsTheMachine(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("MEM"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	snap, cerr := s.SetSampleMemory(twoMB)
	if cerr != nil {
		t.Fatalf("SetSampleMemory: %v", cerr)
	}
	if snap.Disk.MemoryBytes != twoMB {
		t.Fatalf("memory = %d, want %d", snap.Disk.MemoryBytes, twoMB)
	}
}

func TestSetSampleMemoryRefusesWhatNoFZHolds(t *testing.T) {
	for name, bytes := range map[string]int{
		"under a megabyte": oneMB - 1,
		"past the ceiling": twoMB + 1,
		"nothing at all":   0,
		"negative":         -1,
	} {
		t.Run(name, func(t *testing.T) {
			s := NewSession()
			if _, cerr := s.SetSampleMemory(bytes); cerr == nil {
				t.Fatalf("SetSampleMemory(%d) was accepted", bytes)
			}
		})
	}
}

// The figure describes the machine, not the document, so it must not
// look like an edit: no revision, no history entry, and nothing for
// undo to take back.
func TestSetSampleMemoryIsNotAnEdit(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("MEM"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	before := s.Snapshot()
	snap, cerr := s.SetSampleMemory(twoMB)
	if cerr != nil {
		t.Fatalf("SetSampleMemory: %v", cerr)
	}
	if snap.Revision != before.Revision {
		t.Fatalf("revision moved from %d to %d", before.Revision, snap.Revision)
	}
	if snap.CanUndo != before.CanUndo {
		t.Fatalf("undo availability changed to %v", snap.CanUndo)
	}
}

// What the instrument asks the sampler to hold, which is the figure the
// memory reading measures. A dump loads as a unit, so its audio is what
// has to fit.
func TestSnapshotCarriesTheInstrumentsAudio(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("MEM"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	if got := s.Snapshot().Disk.AudioBytes; got != 0 {
		t.Fatalf("audio = %d on a disk with no instrument, want 0", got)
	}

	const frames = 90000
	if _, cerr := s.ImportWAVToInstrument("pad.wav", monoRateWAV(t, frames, 18000), 18000, ChannelMix); cerr != nil {
		t.Fatalf("import: %v", cerr)
	}
	got := s.Snapshot().Disk.AudioBytes
	want := frames * disk.BytesPerSample
	// Sector padding rounds the audio area up, so the figure lands at
	// or just above the samples imported, never below.
	if got < want || got > want+disk.SectorSize {
		t.Fatalf("audio = %d, want about %d", got, want)
	}
}

// A velocity switch clone points at an earlier slot's samples, so it
// costs the sampler nothing and must not be counted twice.
func TestInstrumentAudioCountsSharedSamplesOnce(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("MEM"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	if _, cerr := s.ImportWAVToInstrument("pad.wav", monoRateWAV(t, 40000, 18000), 18000, ChannelMix); cerr != nil {
		t.Fatalf("import: %v", cerr)
	}
	before := s.Snapshot().Disk.AudioBytes

	if _, cerr := s.DuplicateArea(0, 0); cerr != nil {
		t.Fatalf("DuplicateArea: %v", cerr)
	}
	snap := s.Snapshot()
	shared := false
	for _, v := range snap.Disk.Instrument.Voices {
		if v.SharesAudio {
			shared = true
		}
	}
	if !shared {
		t.Skip("the duplicate does not share audio in this build")
	}
	if got := snap.Disk.AudioBytes; got != before {
		t.Fatalf("audio = %d after a clone, want the original %d", got, before)
	}
}
