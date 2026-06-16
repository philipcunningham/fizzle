//go:build feature

package feature

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// F3: saving a new disk must not bleed library log output onto the screen
// (guards run.go's logger.Silence; the F-A stdout leak). Process-boundary;
// only a real-PTY spec can see it.
// Negative control (proven in the spike): removing run.go's logger.Silence()
// makes this FAIL: the stray "creating disk image file=..." log appears on
// screen. If this ever goes green with the silence removed, it has rotted.
// The status redraw is a partial render and does not clear the mid-screen leak.
func TestSaveNoLogLeak(t *testing.T) {
	ws := t.TempDir()
	d := studio(t, ws)
	d.WaitFor("Workspace")
	d.Send("n")
	d.WaitFor("untitled")
	d.Send(kCtrlS)
	d.WaitFor("Filename")
	d.Send("LEAKTEST")
	d.Send(kEnter)
	d.WaitFor("Saved")
	screen := d.Screen()
	for _, marker := range []string{"disk image", "file=", "label=", "INFO", "level=", "DEBUG"} {
		if strings.Contains(screen, marker) {
			t.Fatalf("save leaked log output (%q) onto the screen:\n%s", marker, screen)
		}
	}
	d.Send(kCtrlQ)
	d.WaitExit(3 * time.Second)
}

// F4: shrinking below the minimum shows the gate; restoring recovers.
func TestResizeGate(t *testing.T) {
	ws := workspaceWith(t, junglismImg)
	d := studio(t, ws)
	d.WaitFor("Workspace")
	d.Resize(100, 30)
	d.WaitFor("requires 140")
	d.Resize(cols, rows)
	d.WaitFor("Workspace") // recovered to the prior screen
	d.Send(kCtrlQ)
	d.WaitExit(3 * time.Second)
}

// F5a: Ctrl-Q on a clean disk exits 0 (already covered by M1; kept explicit).
// F5b: Ctrl-Q with unsaved edits raises the guard modal.
func TestQuitWithUnsavedEditsPrompts(t *testing.T) {
	ws := workspaceWith(t, junglismImg)
	d := studio(t, ws)
	openSingleDisk(d)
	d.Send("r") // rename bank -> dirty edit
	d.WaitFor("Rename")
	d.Send("X")
	d.Send(kEnter)
	d.WaitFor("modified") // ● modified indicator
	d.Send(kCtrlQ)
	d.WaitFor("Unsaved changes") // guard modal, not a silent quit
	d.Send(kRight)               // -> "Quit anyway"
	d.Send(kEnter)
	d.WaitExit(3 * time.Second)
}

// F5c: Ctrl-C (SIGINT) tears down cleanly.
func TestSigintExitsCleanly(t *testing.T) {
	ws := workspaceWith(t, junglismImg)
	d := studio(t, ws)
	d.WaitFor("Workspace")
	d.Signal(os.Interrupt)
	// FINDING: studio exits non-zero on SIGINT (context-cancelled). We pin the
	// actual behavior: the spec only requires a PROMPT teardown, not exit 0.
	if code := d.WaitExitAny(3 * time.Second); code == 0 {
		t.Log("note: SIGINT exited 0")
	}
}

// F6: every synthetic disk opens to Layout without crashing or blanking.
func TestOpenEveryFixture(t *testing.T) {
	imgs, err := filepath.Glob(filepath.Join(synthDir(), "*.img"))
	if err != nil || len(imgs) == 0 {
		t.Fatalf("no fixtures found: %v", err)
	}
	for _, img := range imgs {
		img := img
		t.Run(filepath.Base(img), func(t *testing.T) {
			ws := workspaceWith(t, img)
			d := studio(t, ws)
			openSingleDisk(d) // Enter -> Bank 1 (reaches Layout)
			d.Send(kCtrlQ)
			d.WaitExit(3 * time.Second)
		})
	}
}
