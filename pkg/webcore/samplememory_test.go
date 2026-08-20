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

// The room before an import is bound by the tighter of the two
// ceilings. On a stock FZ-1 the machine binds first, at the 14.5
// seconds Casio quoted for 1 MB at 36 kHz. On an expanded machine the
// floppy binds first, at about 18 seconds, which is why an instrument
// that fills 2 MB needs a two disk set to carry it.
func TestRoomFollowsTheDeclaredMachine(t *testing.T) {
	for name, tc := range map[string]struct {
		memory int
		want   float64
	}{
		"the machine binds": {memory: oneMB, want: 14.56},
		"the floppy binds":  {memory: twoMB, want: 18.16},
	} {
		t.Run(name, func(t *testing.T) {
			s := NewSession()
			if _, cerr := s.NewDisk("ROOM"); cerr != nil {
				t.Fatalf("NewDisk: %v", cerr)
			}
			if _, cerr := s.SetSampleMemory(tc.memory); cerr != nil {
				t.Fatalf("SetSampleMemory: %v", cerr)
			}
			est, cerr := s.EstimateImport(
				map[string][]byte{"tick.wav": monoRateWAV(t, 100, 36000)}, 36000, ChannelMix)
			if cerr != nil {
				t.Fatalf("EstimateImport: %v", cerr)
			}
			if est.RoomSeconds < tc.want-0.5 || est.RoomSeconds > tc.want+0.5 {
				t.Fatalf("room = %f s, want about %f", est.RoomSeconds, tc.want)
			}
		})
	}
}

// The declared figure informs; it never blocks. A disk is not a load,
// so a machine that cannot hold everything on a floppy is still a disk
// worth building, and the user may be building it for someone else.
func TestImportPastTheDeclaredMemoryStillLands(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("OVER"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	// 600,000 frames is 1.2 MB of audio: past a 1 MB machine, inside
	// one floppy, and inside the 2 MB no FZ exceeds.
	wav := monoRateWAV(t, 600000, 18000)
	if _, cerr := s.ImportWAVToInstrument("big.wav", wav, 18000, ChannelMix); cerr != nil {
		t.Fatalf("import refused at the 1 MB default: %v", cerr)
	}
	snap := s.Snapshot()
	if snap.Disk.AudioBytes <= snap.Disk.MemoryBytes {
		t.Fatalf("audio %d did not exceed memory %d, so this proves nothing",
			snap.Disk.AudioBytes, snap.Disk.MemoryBytes)
	}
	if _, cerr := s.ExportImage(); cerr != nil {
		t.Fatalf("export refused over the declared memory: %v", cerr)
	}
}

// The hardware's own ceiling is not the user's, so declaring 1 MB must
// not tighten what fizzle refuses to build.
func TestTheHardRefusalKeepsTheHardwareCeiling(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("HARD"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	if _, cerr := s.SetSampleMemory(oneMB); cerr != nil {
		t.Fatalf("SetSampleMemory: %v", cerr)
	}
	// Past 2 MB: refused at any declaration, as today.
	huge := map[string][]byte{"huge.wav": monoRateWAV(t, 1200000, 18000)}
	est, cerr := s.EstimateImport(huge, 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("EstimateImport: %v", cerr)
	}
	if est.Verdict != VerdictWontFit {
		t.Fatalf("verdict = %q for a batch past 2 MB, want a refusal", est.Verdict)
	}
}

// The dialog says what the import needs and what the machine holds,
// two facts and no inference, so the core supplies both.
func TestEstimateStatesTheLoadAgainstTheMachine(t *testing.T) {
	s := NewSession()
	if _, cerr := s.NewDisk("SAY"); cerr != nil {
		t.Fatalf("NewDisk: %v", cerr)
	}
	// Half a megabyte already in, and more than half arriving.
	if _, cerr := s.ImportWAVToInstrument("first.wav", monoRateWAV(t, 280000, 18000), 18000, ChannelMix); cerr != nil {
		t.Fatalf("import: %v", cerr)
	}
	est, cerr := s.EstimateImport(
		map[string][]byte{"second.wav": monoRateWAV(t, 280000, 18000)}, 18000, ChannelMix)
	if cerr != nil {
		t.Fatalf("EstimateImport: %v", cerr)
	}
	if est.MemoryBytes != oneMB {
		t.Fatalf("memory = %d, want the declared %d", est.MemoryBytes, oneMB)
	}
	// Both halves together pass a stock machine.
	if est.AudioAfterBytes <= oneMB {
		t.Fatalf("audio after = %d, want past the 1 MB machine", est.AudioAfterBytes)
	}
	// And the verdict still lets it through: the figure informs.
	if est.Verdict != VerdictFits {
		t.Fatalf("verdict = %q, want the import to land anyway", est.Verdict)
	}
}
