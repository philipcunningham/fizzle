package app

import (
	"encoding/binary"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/loader"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

// TestDeleteArea_RoutesThroughUndoableModel checks the App wrapper's own
// responsibility: it feeds container.DeleteAreaPatches through ApplyBatch
// so the delete lands (bstep decremented) and is undoable in one step. The
// exhaustive per-field shift / freed-slot zeroing is exercised in
// container/container_test.go.
func TestDeleteArea_RoutesThroughUndoableModel(t *testing.T) {
	t.Parallel()
	data := make([]byte, disk.SectorSize)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceCountOffset:], 3)
	binary.LittleEndian.PutUint16(data[disk.BankVoiceNumOffset+1*disk.VPEntrySize:], 11)

	a := New(t.TempDir())
	a.containerModel = model.FromBytes("", data)
	a.containerInfo = loader.ContainerInfo{BankCount: 1}

	a = a.deleteArea(0, 0, "X")
	got := a.containerModel.Bytes()

	if b := binary.LittleEndian.Uint16(got[disk.BankVoiceCountOffset:]); b != 2 {
		t.Fatalf("bstep = %d, want 2 (delete not applied)", b)
	}
	if v := binary.LittleEndian.Uint16(got[disk.BankVoiceNumOffset:]); v != 11 {
		t.Errorf("vp[0] = %d, want 11 (area 1 shifted down)", v)
	}
	if !a.containerModel.CanUndo() {
		t.Error("delete must be a single undoable batch")
	}
}
