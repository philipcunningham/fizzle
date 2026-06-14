package app

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestPump_FakeClock_RecordsTickFromInit pins the harness contract.
// The fake clock must:
//   - intercept App.Init's tea.Tick so the pump loop terminates
//     instead of blocking on a 30-second wall-clock sleep, and
//   - retain the recorded (delay, fn) pair so the test can fire it
//     later via FireMatching / FireAll.
//
// If this test ever hangs, the seam is broken: production clock is
// running where the fake should be.
func TestPump_FakeClock_RecordsTickFromInit(t *testing.T) {
	a, fc := newTestAppEmpty(t)

	// Drain Init through pump. The autosave Tick(30s) returned by
	// Init must land in fc.pending, not block the test.
	cmd := a.Init()
	if cmd == nil {
		t.Fatal("Init returned nil cmd; expected autosave Tick")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("autosave Tick should return nil through the fake clock; got %T", msg)
	}

	pending := fc.Pending()
	if len(pending) != 1 {
		t.Fatalf("fake clock pending = %d, want 1", len(pending))
	}
	if pending[0] != 30*time.Second {
		t.Fatalf("autosave delay = %v, want 30s", pending[0])
	}

	// Firing the recorded autosave tick must produce an autoSaveTick
	// msg the App can then receive.
	msgs := fc.FireAll()
	if len(msgs) != 1 {
		t.Fatalf("FireAll returned %d msgs, want 1", len(msgs))
	}
	if _, ok := msgs[0].(autoSaveTick); !ok {
		t.Fatalf("fired msg = %T, want autoSaveTick", msgs[0])
	}

	// Feeding it back via pump must schedule the NEXT autosave tick
	// (the App's Update re-arms on every autoSaveTick). We don't
	// inspect the returned App; the fake clock's Pending slice is
	// the observable contract for "tick was re-armed."
	_ = pump(t, a, msgs[0])
	pending = fc.Pending()
	if len(pending) != 1 {
		t.Fatalf("after autosave handle, pending = %d, want 1 (re-armed)", len(pending))
	}
}

// TestPump_QuiescentWithoutFakeClockTickReturn pins that pump's
// drain loop terminates when a Cmd's executor returns nil. Without
// this, tests that produce one-shot Cmds (not Ticks) would loop
// forever.
func TestPump_QuiescentOnNilCmdResult(t *testing.T) {
	a, _ := newTestAppEmpty(t)
	// A no-op key press returns no Cmd; pump must just return.
	done := make(chan App, 1)
	go func() { done <- pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40}) }()
	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("pump did not terminate within 2s on a no-Cmd message")
	}
}
