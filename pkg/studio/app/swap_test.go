package app

import (
	"encoding/binary"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/loader"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

// TestSwapAreasInBank_RoutesThroughUndoableModel checks the App wrapper's
// own responsibility: it feeds container.SwapAreaPatches through
// ApplyBatch so the swap lands and is undoable in one step. The exhaustive
// per-field swap behaviour (including the REMAIN-008 MIDI-channel omission)
// is exercised in container/container_test.go.
func TestSwapAreasInBank_RoutesThroughUndoableModel(t *testing.T) {
	t.Parallel()
	data := make([]byte, 2*disk.SectorSize) // bank 0 + 1 voice sector
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset+0*disk.VPEntrySize:], 100)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset+1*disk.VPEntrySize:], 200)

	a := New(t.TempDir())
	a.containerModel = model.FromBytes("", data)
	a.containerInfo = loader.ContainerInfo{BankCount: 1}

	a = a.swapAreasInBank(0, 0, 1)

	got := a.containerModel.Bytes()
	if v := binary.LittleEndian.Uint16(got[disk.BankVoiceNumOffset:]); v != 200 {
		t.Errorf("vp[0] = %d, want 200 (swap not applied)", v)
	}
	if !a.containerModel.CanUndo() {
		t.Error("swap must be a single undoable batch")
	}
}
