package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/status"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/toast"
)

// modelApplyDirtyByte returns a Patch that flips one byte in data.
// We target the first byte of bank 0's name field, which any FZ
// container has and which doesn't affect downstream invariants.
// Used by the quit-when-dirty tests to induce a dirty state
// without depending on a fixture.
func modelApplyDirtyByte(t testing.TB, data []byte) model.Patch {
	t.Helper()
	off := disk.BankNameOffset
	if off >= len(data) {
		t.Fatalf("modelApplyDirtyByte: data too small (%d bytes)", len(data))
	}
	return model.Patch{
		Offset: off,
		Old:    []byte{data[off]},
		New:    []byte{data[off] ^ 0x01},
	}
}

// TestApp_TooSmallTerminal_BlocksRenderUntilResized pins the
// minimum-terminal guard. Below the floor (minCols x minRows), the
// View must show a hint message instead of the regular layout;
// the body widgets assume at least that much room (the footer hint
// band alone is ~135 columns).
func TestApp_TooSmallTerminal_BlocksRenderUntilResized(t *testing.T) {
	a, _ := newTestAppEmpty(t)
	a = pump(t, a, tea.WindowSizeMsg{Width: 60, Height: 10})
	if !a.tooSmall {
		t.Fatal("expected tooSmall=true at 60x10")
	}
	got := a.View().Content
	low := strings.ToLower(got)
	if !strings.Contains(low, "requires") && !strings.Contains(low, "resize") {
		t.Errorf("View under too-small didn't surface a size hint:\n%s", got)
	}

	// Resize at-or-above the floor and confirm tooSmall clears.
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})
	if a.tooSmall {
		t.Errorf("expected tooSmall=false at 140x40 (floor %dx%d)", 140, 30)
	}
}

// TestApp_ToastAutoDismiss_FiresViaFakeClock pins the toast
// auto-dismiss path end-to-end:
//
//  1. toast.Set returns a tea.Cmd that, when invoked, schedules a
//     dismiss via the (faked) clock.
//  2. The fake clock records the tick instead of sleeping.
//  3. Firing the recorded tick produces a toast.DismissMsg.
//  4. Feeding that msg through pump clears the toast text.
//
// Without the Phase 0 clock seam, this test would have to wait the
// full toast.Duration (3s) wall-clock, and the cmd pump would
// never see the dismiss msg because it's emitted from a goroutine
// the test doesn't control.
func TestApp_ToastAutoDismiss_FiresViaFakeClock(t *testing.T) {
	a, fc := newTestAppEmpty(t)
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})

	// Drive a Save via the App's toast helper. Set returns the
	// dismiss-cmd that our pump must execute through the fake clock.
	cmd := a.toast.Set("Saved!")
	if cmd == nil {
		t.Fatal("toast.Set returned nil cmd; expected the dismiss tick")
	}
	// Pump processes the cmd; the fake clock should swallow it and
	// record the (delay, fn) pair.
	if msg := cmd(); msg != nil {
		t.Fatalf("fake-clock Tick should return nil; got %T", msg)
	}

	pending := fc.Pending()
	if len(pending) != 1 {
		t.Fatalf("fake clock pending = %d, want 1 (toast dismiss tick)", len(pending))
	}
	if pending[0] != toast.Duration {
		t.Errorf("toast dismiss delay = %v, want %v", pending[0], toast.Duration)
	}

	// Pre-condition: toast is showing.
	if a.toast.View() == "" {
		t.Fatal("toast didn't render Set text")
	}

	// Fire the recorded tick and feed it back. The toast should
	// clear.
	msgs := fc.FireAll()
	if len(msgs) != 1 {
		t.Fatalf("FireAll returned %d msgs, want 1", len(msgs))
	}
	if _, ok := msgs[0].(toast.DismissMsg); !ok {
		t.Fatalf("fired msg = %T, want toast.DismissMsg", msgs[0])
	}
	a = pump(t, a, msgs[0])
	if a.toast.View() != "" {
		t.Errorf("toast still visible after DismissMsg; got %q", a.toast.View())
	}
}

// TestApp_ToastDismissMsg_StaleTokenIgnored pins the token guard:
// if a second Set replaces the toast, the first toast's dismiss
// tick (which has the older token) must NOT clear the new toast.
//
// This is the bug a naive auto-dismiss would have: rapid double
// Set produces two ticks, the first fires after Duration and
// clears the second toast prematurely.
func TestApp_ToastDismissMsg_StaleTokenIgnored(t *testing.T) {
	a, fc := newTestAppEmpty(t)
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})

	cmd1 := a.toast.Set("first")
	_ = cmd1()
	cmd2 := a.toast.Set("second")
	_ = cmd2()

	if a.toast.View() == "" || !strings.Contains(a.toast.View(), "second") {
		t.Fatalf("toast should show 'second'; got %q", a.toast.View())
	}

	// Fire the FIRST tick only (delay == Duration both times; we
	// reorder by selecting the older token via FireMatching seeing
	// both as equal). Easier: fire all and confirm the SECOND
	// toast survives the first dismiss thanks to the token guard.
	msgs := fc.FireAll()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 pending dismisses; got %d", len(msgs))
	}
	// Feed both in order. After the first, toast should still show
	// "second". After the second, it should clear.
	a = pump(t, a, msgs[0])
	if !strings.Contains(a.toast.View(), "second") {
		t.Errorf("stale first-dismiss cleared the second toast: %q", a.toast.View())
	}
	a = pump(t, a, msgs[1])
	if a.toast.View() != "" {
		t.Errorf("second dismiss didn't clear; got %q", a.toast.View())
	}
	_ = time.Now
}

// TestApp_QuitOnCleanContainer_EmitsQuit pins the happy path: Ctrl+Q
// on an unmodified container quits immediately, no confirmation
// modal in the way. The pump driver detects tea.Quit by the cmd's
// produced msg being tea.QuitMsg.
func TestApp_QuitOnCleanContainer_EmitsQuit(t *testing.T) {
	a, _ := newTestAppEmpty(t)
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})

	// Untitled containers are clean by default; verify the
	// assumption before testing.
	if a.containerModel != nil && a.containerModel.Dirty() {
		t.Fatal("fresh App should be clean; got dirty")
	}

	_, cmd := a.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Ctrl+Q on clean container returned nil cmd; expected tea.Quit")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("Ctrl+Q cmd produced nil msg; expected tea.QuitMsg")
	} else if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("Ctrl+Q produced %T; want tea.QuitMsg", msg)
	}
}

// TestApp_QuitOnDirtyContainer_OpensConfirm pins the safety net:
// Ctrl+Q on a dirty container does NOT quit; it opens the
// confirmation modal so the user can opt in to lose edits.
func TestApp_QuitOnDirtyContainer_OpensConfirm(t *testing.T) {
	a, _ := newTestAppEmpty(t)
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})

	// Force a dirty state.
	data := a.containerModel.Bytes()
	if len(data) < 2 {
		t.Fatalf("untitled container too small to mutate: %d bytes", len(data))
	}
	// Flip one byte via the model's Apply path so dirty=true.
	if err := a.containerModel.Apply(modelApplyDirtyByte(t, data)); err != nil {
		t.Fatalf("seed dirty: %v", err)
	}
	if !a.containerModel.Dirty() {
		t.Fatal("expected dirty after mutation; got clean")
	}

	updated, cmd := a.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if cmd != nil {
		// If the cmd resolves to tea.QuitMsg, the safety net failed.
		if msg := cmd(); msg != nil {
			if _, ok := msg.(tea.QuitMsg); ok {
				t.Fatal("Ctrl+Q on dirty container emitted tea.Quit; expected confirm modal")
			}
		}
	}
	a, _ = updated.(App)
	if a.confirm == nil || !a.confirm.IsOpen() {
		t.Errorf("expected confirm modal open after Ctrl+Q on dirty container")
	}
}

// TestApp_StatusInfoAutoDismissesAfter4s pins the spec for the
// non-sticky severities by driving Info through the full path:
// the status widget schedules a dismiss tick on the fake clock,
// Update batches the cmd, the fake clock fires it, and the
// resulting DismissMsg clears the status line.
func TestApp_StatusInfoAutoDismissesAfter4s(t *testing.T) {
	a, fc := newTestAppEmpty(t)
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})

	// Drain any ticks the App scheduled at startup (autosave fires
	// at 30 s) so the assertions below only see the status tick.
	_ = fc.FireMatching(func(d time.Duration) bool { return d == 30*time.Second })

	// Set an Info message directly through the helper; the Update
	// wrapper will batch the dismiss tick the next time Update runs.
	// Here we drive a benign Update path (a no-op WindowSizeMsg at
	// the current size) so the wrapper drains pendingStatusCmd.
	a.setStatus(status.Info, "info ping")
	if a.status.View() == "" {
		t.Fatal("status didn't render after setStatus(Info)")
	}
	// Run the inner state through Update so the wrapper drains the
	// pending cmd into the fake clock.
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})

	infoTicks := fc.FireMatching(func(d time.Duration) bool {
		return d == status.InfoDuration
	})
	if len(infoTicks) != 1 {
		t.Fatalf("expected 1 InfoDuration tick scheduled; got %d", len(infoTicks))
	}
	dismiss, ok := infoTicks[0].(status.DismissMsg)
	if !ok {
		t.Fatalf("fired tick = %T, want status.DismissMsg", infoTicks[0])
	}
	a = pump(t, a, dismiss)
	if a.status.View() != "" {
		t.Errorf("status still visible after DismissMsg; got %q", a.status.View())
	}
}

// TestApp_StatusErrorRequiresEscToDismiss pins the sticky-Error
// spec end-to-end: an Error message stays visible even after
// time passes (no tick scheduled), and pressing Esc clears it
// instead of falling through to per-space Cancel.
func TestApp_StatusErrorRequiresEscToDismiss(t *testing.T) {
	a, fc := newTestAppEmpty(t)
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})

	// Drain startup ticks (autosave at 30 s) so the assertions
	// below see only what the Error path scheduled (which is
	// nothing).
	_ = fc.FireMatching(func(d time.Duration) bool { return d == 30*time.Second })

	a.setStatus(status.Error, "boom")
	if a.status.View() == "" {
		t.Fatal("Error status didn't render after setStatus")
	}
	// Drive Update so the wrapper would batch any pending tick.
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})

	// Fire every pending tick (even the 30 s autosave reload, if
	// any), and confirm none of them dismiss the Error.
	for _, msg := range fc.FireAll() {
		if msg == nil {
			continue
		}
		a = pump(t, a, msg)
	}
	if a.status.View() == "" {
		t.Fatalf("Error status cleared by a tick; spec says it must be sticky")
	}

	// Press Esc. The routeAction interceptor must clear the
	// Error rather than forward Cancel to the focused space.
	a = pump(t, a, tea.KeyPressMsg{Code: tea.KeyEsc})
	if a.status.View() != "" {
		t.Errorf("Esc did not clear sticky Error; got %q", a.status.View())
	}
}

// TestApp_StatusReplacementInvalidatesOlderTick pins the
// rapid-Set race at the App-level boundary: a Warning that
// replaces an Info must survive the Info's dismiss tick firing.
// Without the token guard, the older tick would clear the newer
// message prematurely.
func TestApp_StatusReplacementInvalidatesOlderTick(t *testing.T) {
	a, fc := newTestAppEmpty(t)
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})

	// Drain startup ticks (autosave at 30 s) so subsequent counts
	// reflect status only.
	_ = fc.FireMatching(func(d time.Duration) bool { return d == 30*time.Second })

	// Two back-to-back sets, each driven through Update so its
	// dismiss tick lands in the fake clock.
	a.setStatus(status.Info, "first")
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})
	a.setStatus(status.Warning, "second")
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})

	if !strings.Contains(a.status.View(), "second") {
		t.Fatalf("status should show 'second'; got %q", a.status.View())
	}

	// Fire every scheduled status tick. The fake clock returns
	// pending ticks in insertion order, so msgs[0] is the older
	// Info dismiss and msgs[1] is the newer Warning dismiss.
	msgs := fc.FireMatching(func(d time.Duration) bool {
		return d == status.InfoDuration || d == status.WarningDuration
	})
	if len(msgs) != 2 {
		t.Fatalf("expected 2 status ticks (Info + Warning); got %d", len(msgs))
	}
	staleDismiss, ok := msgs[0].(status.DismissMsg)
	if !ok {
		t.Fatalf("first tick = %T, want status.DismissMsg", msgs[0])
	}
	currentDismiss, ok := msgs[1].(status.DismissMsg)
	if !ok {
		t.Fatalf("second tick = %T, want status.DismissMsg", msgs[1])
	}
	if staleDismiss.Token == currentDismiss.Token {
		t.Fatalf("tokens should differ between consecutive Sets; both = %d", staleDismiss.Token)
	}
	a = pump(t, a, staleDismiss)
	if !strings.Contains(a.status.View(), "second") {
		t.Errorf("stale Info dismiss cleared the Warning: %q", a.status.View())
	}
	a = pump(t, a, currentDismiss)
	if a.status.View() != "" {
		t.Errorf("Warning dismiss did not clear; got %q", a.status.View())
	}
}
