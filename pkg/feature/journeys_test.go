//go:build feature

package feature

import (
	"path/filepath"
	"testing"
	"time"
)

// openSingleDisk opens the only disk in the workspace (cursor starts on it)
// and drills Workspace -> Layout bank list.
func openSingleDisk(d *Driver) {
	d.WaitFor("Workspace")
	d.Send(kEnter) // open -> Layout bank list
	d.WaitFor("Bank 1")
}

// drillToSound goes from the bank list into Bank 1 / Area 1 / Sound.
func drillToSound(d *Driver) {
	d.Send(kEnter) // Bank 1 -> area list
	d.WaitFor("A01")
	d.Send(kEnter) // Area 1 -> Sound
	d.WaitFor("Sound")
}

// soundRow steps from the top (DCA) down to a named row and waits for one of
// its field labels to appear.
func soundRow(d *Driver, downs int, fieldLabel string) {
	for i := 0; i < downs; i++ {
		d.Send(kDown)
	}
	d.WaitFor(fieldLabel)
}

// typeField edits a numeric field within the active cell: Enter to begin
// editing the cell's first field, Right `rights` times to reach the target field
// (some cells lead with an enum, e.g. LFO Waveform), type the value, commit.
func typeField(d *Driver, rights int, value string) {
	d.Send(kEnter)
	for i := 0; i < rights; i++ {
		d.Send(kRight)
	}
	d.Send(value)
	d.Send(kEnter)
}

// F2: load an existing realistic disk, edit across DCA / DCF / LFO, rename the
// bank, save, relaunch a fresh process, reopen, and assert every edit persisted.
func TestLoadEditRoundTrip(t *testing.T) {
	ws := workspaceWith(t, junglismImg)

	// --- Phase A: edit + save ---
	d := studio(t, ws)
	openSingleDisk(d)
	drillToSound(d)

	// DCA row (top): set level KF = 7
	soundRow(d, 0, "DCA level KF")
	typeField(d, 0, "7")
	d.WaitForField("DCA level KF", "+7")

	// DCF row: set Cutoff = 88
	soundRow(d, 1, "Cutoff")
	typeField(d, 0, "88")
	d.WaitForField("Cutoff", "88")

	// LFO row: set Rate = 33
	soundRow(d, 1, "Rate")
	typeField(d, 1, "33")
	d.WaitForField("Rate", "33")

	// back to the bank list, rename Bank 1
	d.Send(kEsc) // Sound -> Layout area list
	d.WaitFor("A01")
	d.Send(kEsc)         // area list -> bank list
	d.WaitFor("voices)") // Layout header, unique to the bank list
	d.Send("r")
	d.WaitFor("Rename")
	d.Send("Feature") // mixed case: exercises the FZ-name uppercasing (UXC)
	d.Send(kEnter)
	d.WaitFor("FEATURE") // typed "Feature" must commit uppercased
	t.Log("edits applied: DCA KF=7, DCF Cutoff=88, LFO Rate=33, Bank 1=FEATURE")

	d.Send(kCtrlS)
	d.WaitFor("Saved")
	d.Send(kCtrlQ)
	d.WaitExit(3 * time.Second)

	// --- Phase B: fresh process, reopen, assert persistence ---
	d2 := studio(t, ws)
	openSingleDisk(d2)
	d2.WaitFor("FEATURE") // bank name persisted (visible in the bank list)
	drillToSound(d2)
	d2.WaitForField("DCA level KF", "+7")
	soundRow(d2, 1, "Cutoff")
	d2.WaitForField("Cutoff", "88")
	soundRow(d2, 1, "Rate")
	d2.WaitForField("Rate", "33")
	t.Log("round-trip verified after relaunch: all edits + bank name persisted")
	d2.Send(kCtrlQ)
	d2.WaitExit(3 * time.Second)
}

// F1: build a disk from scratch: new disk, import a WAV from the workspace,
// assign it to an Area, set a key range, rename the bank, save under a name,
// relaunch a fresh process, reopen, and assert the voice + range + bank name
// all persisted. Guards the build-from-scratch data-loss path (a new/empty
// disk must save as a Full Dump that actually holds the imported voice).
func TestBuildFromScratchRoundTrip(t *testing.T) {
	wsA := workspaceWith(t, sampleWav()) // contains "amen 01.wav"

	// --- Phase A: build + save ---
	d := studio(t, wsA)
	d.WaitFor("Workspace")
	d.Send("n") // new disk
	d.WaitFor("untitled")

	// pool the WAV: hop to the Workspace, open it into the pool
	d.Send(kShiftUp) // Layout -> Pool
	d.Send(kShiftUp) // Pool -> Workspace
	d.WaitFor("Workspace")
	d.WaitFor("amen 01.wav")
	d.Send(kEnter) // pool the WAV (cursor is on it)

	// back to Layout, drill into Bank 1 / Area 1
	d.Send(kShiftDown) // Workspace -> Pool
	d.Send(kShiftDown) // Pool -> Layout
	d.WaitFor("untitled")
	d.Send(kEnter) // Bank 1 -> area list
	d.WaitFor("A01")

	// assign the pooled voice into A01
	d.Send("i")
	d.WaitFor("Picking voice")
	d.Send(kEnter) // assign the highlighted (only) voice
	d.WaitFor("AMEN 01")

	// set a key range via the Area editor (type-to-set Key Low = 36 = C2)
	d.Send("a")
	d.WaitFor("Key Low")
	d.Send("36")
	d.WaitForField("Key Low", "36")
	d.Send(kEnter) // commit the area editor
	d.WaitForField("AMEN 01", "C2")

	// rename the bank
	d.Send(kEsc) // area list -> bank list
	d.WaitFor("untitled")
	d.Send("r")
	d.WaitFor("Rename")
	d.Send("MYBANK")
	d.Send(kEnter)
	d.WaitFor("MYBANK")

	// save under a name (Save As, since the disk is untitled)
	d.Send(kCtrlS)
	d.WaitFor("Filename")
	d.Send("MYDISK")
	d.Send(kEnter)
	d.WaitFor("Saved")
	t.Log("built from scratch: AMEN 01 @ C2, bank MYBANK, saved MYDISK.img")
	d.Send(kCtrlQ)
	d.WaitExit(3 * time.Second)

	// --- Phase B: fresh process + fresh workspace, reopen, assert ---
	wsB := workspaceWith(t, filepath.Join(wsA, "MYDISK.img"))
	d2 := studio(t, wsB)
	d2.WaitFor("Workspace")
	d2.WaitFor("MYDISK.img")
	d2.Send(kEnter)      // open MYDISK
	d2.WaitFor("MYBANK") // bank name persisted (bank list)
	d2.Send(kEnter)      // -> area list
	d2.WaitFor("AMEN 01")
	d2.WaitForField("AMEN 01", "C2") // key range persisted
	t.Log("round-trip verified: voice + range + bank name all persisted")
	d2.Send(kCtrlQ)
	d2.WaitExit(3 * time.Second)

	// cross-check at the file level: the saved disk holds a Full Dump
	assertDiskHasFullDump(t, filepath.Join(wsA, "MYDISK.img"))
}
