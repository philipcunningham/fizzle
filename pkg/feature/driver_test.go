//go:build feature

package feature

import (
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	vt "github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// Key sequences a real terminal sends. Specs use these with Driver.Send.
const (
	kUp        = "\x1b[A"
	kDown      = "\x1b[B"
	kRight     = "\x1b[C"
	kLeft      = "\x1b[D"
	kEnter     = "\r"
	kEsc       = "\x1b"
	kTab       = "\t"
	kCtrlS     = "\x13"
	kCtrlQ     = "\x11"
	kCtrlC     = "\x03"
	kShiftUp   = "\x1b[1;2A"
	kShiftDown = "\x1b[1;2B"
)

const (
	defaultWait = 6 * time.Second
	pollEvery   = 25 * time.Millisecond
)

// Driver runs a compiled binary under a PTY and feeds its output through a VT
// emulator so specs can assert on a stable screen grid (à la tmux capture-pane).
type Driver struct {
	t    *testing.T
	cmd  *exec.Cmd
	ptmx *os.File
	emu  *vt.SafeEmulator
}

// Start launches bin with args under a cols x rows PTY and registers cleanup.
func Start(t *testing.T, bin string, cols, rows int, args ...string) *Driver {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		t.Fatalf("pty start %s: %v", bin, err)
	}
	d := &Driver{t: t, cmd: cmd, ptmx: ptmx, emu: vt.NewSafeEmulator(cols, rows)}

	// program frames -> emulator
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				_, _ = d.emu.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	// emulator query-replies (DA/DSR/bg-color) -> program stdin. Mandatory:
	// a bubbletea v2 program can stall waiting on a terminal query otherwise.
	go func() { _, _ = io.Copy(ptmx, d.emu) }() // SafeEmulator: Read is synchronized vs Write

	t.Cleanup(d.close)
	return d
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b[][PX^_].*?\x1b\\|\x1b[@-Z\\-_]`)

// Screen returns the current visible screen as plain text: ANSI stripped and
// trailing whitespace trimmed per line, so comparisons are stable.
func (d *Driver) Screen() string {
	raw := d.emu.Render()
	plain := ansiRe.ReplaceAllString(raw, "")
	lines := strings.Split(plain, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}

// Send writes raw bytes (keystrokes) to the PTY.
func (d *Driver) Send(s string) {
	if _, err := d.ptmx.Write([]byte(s)); err != nil {
		d.t.Fatalf("send %q: %v", s, err)
	}
}

// WaitFor polls until the screen contains sub, or fails with a screen dump.
func (d *Driver) WaitFor(sub string) {
	d.t.Helper()
	if !d.waitFor(sub, defaultWait) {
		d.t.Fatalf("did not see %q within %s; screen:\n%s", sub, defaultWait, d.Screen())
	}
}

// WaitForField waits until some screen row contains both label and value
// (whitespace-insensitive), so value assertions survive padding/layout tweaks.
func (d *Driver) WaitForField(label, value string) {
	d.t.Helper()
	deadline := time.Now().Add(defaultWait)
	for time.Now().Before(deadline) {
		for _, ln := range strings.Split(d.Screen(), "\n") {
			if strings.Contains(ln, label) && strings.Contains(ln, value) {
				return
			}
		}
		time.Sleep(pollEvery)
	}
	d.t.Fatalf("did not see field %q = %q on one row within %s; screen:\n%s", label, value, defaultWait, d.Screen())
}

// Seen reports whether sub appears within timeout (non-fatal).
func (d *Driver) Seen(sub string, timeout time.Duration) bool { return d.waitFor(sub, timeout) }

func (d *Driver) waitFor(sub string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(d.Screen(), sub) {
			return true
		}
		time.Sleep(pollEvery)
	}
	return false
}

// Resize changes the PTY window size (drives the SIGWINCH path).
func (d *Driver) Resize(cols, rows int) {
	_ = pty.Setsize(d.ptmx, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	d.emu.Resize(cols, rows)
}

// Signal sends an OS signal to the process (e.g. os.Interrupt for Ctrl-C).
func (d *Driver) Signal(sig os.Signal) {
	if d.cmd.Process != nil {
		_ = d.cmd.Process.Signal(sig)
	}
}

// WaitExit asserts the process exits cleanly within timeout.
func (d *Driver) WaitExit(timeout time.Duration) {
	d.t.Helper()
	done := make(chan error, 1)
	go func() { done <- d.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			d.t.Fatalf("process exited with error: %v\nscreen:\n%s", err, d.Screen())
		}
	case <-time.After(timeout):
		d.t.Fatalf("process did not exit within %s\nscreen:\n%s", timeout, d.Screen())
	}
}

// WaitExitAny asserts the process exits within timeout, regardless of exit
// code (e.g. SIGINT teardown). Returns the exit code (-1 if not an exit error).
func (d *Driver) WaitExitAny(timeout time.Duration) int {
	d.t.Helper()
	type res struct{ err error }
	done := make(chan res, 1)
	go func() { done <- res{d.cmd.Wait()} }()
	select {
	case r := <-done:
		if r.err == nil {
			return 0
		}
		if ee, ok := r.err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		return -1
	case <-time.After(timeout):
		d.t.Fatalf("process did not exit within %s\nscreen:\n%s", timeout, d.Screen())
		return -1
	}
}

func (d *Driver) close() {
	_ = d.ptmx.Close()
	if d.cmd.Process != nil {
		_ = d.cmd.Process.Kill()
		_, _ = d.cmd.Process.Wait()
	}
}
