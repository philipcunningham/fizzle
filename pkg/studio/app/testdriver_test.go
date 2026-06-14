package app

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// fakeClock is the test-side replacement for clock.Real(). It
// records every Tick call instead of scheduling one and returns a
// no-op Cmd so the pump() driver terminates without sleeping. Tests
// fire pending ticks on demand via Fire / FireMatching.
type fakeClock struct {
	mu      sync.Mutex
	pending []pendingTick
}

type pendingTick struct {
	delay time.Duration
	fn    func(time.Time) tea.Msg
}

// newFakeClock returns a clock whose Tick method records calls.
func newFakeClock() *fakeClock { return &fakeClock{} }

// Tick implements clock.TickFn. The returned Cmd is a no-op so
// pump's drain loop terminates immediately; tests fire the recorded
// (delay, fn) pairs themselves via Fire.
func (c *fakeClock) Tick(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
	c.mu.Lock()
	c.pending = append(c.pending, pendingTick{delay: d, fn: fn})
	c.mu.Unlock()
	return func() tea.Msg { return nil }
}

// Pending returns a copy of the currently-recorded ticks (delays
// only) for assertions on what timers the App scheduled.
func (c *fakeClock) Pending() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]time.Duration, len(c.pending))
	for i, p := range c.pending {
		out[i] = p.delay
	}
	return out
}

// FireAll fires every pending tick and returns the resulting
// tea.Msg slice. After this call, Pending() is empty.
func (c *fakeClock) FireAll() []tea.Msg {
	c.mu.Lock()
	pending := c.pending
	c.pending = nil
	c.mu.Unlock()
	out := make([]tea.Msg, 0, len(pending))
	for _, p := range pending {
		out = append(out, p.fn(time.Now()))
	}
	return out
}

// FireMatching fires every pending tick where match(delay) is true,
// returning the resulting tea.Msg slice. Non-matching ticks stay
// pending.
func (c *fakeClock) FireMatching(match func(time.Duration) bool) []tea.Msg {
	c.mu.Lock()
	keep := c.pending[:0]
	var fire []pendingTick
	for _, p := range c.pending {
		if match(p.delay) {
			fire = append(fire, p)
		} else {
			keep = append(keep, p)
		}
	}
	c.pending = keep
	c.mu.Unlock()
	out := make([]tea.Msg, 0, len(fire))
	for _, p := range fire {
		out = append(out, p.fn(time.Now()))
	}
	return out
}

// pump drives the App through Update, then drains the returned
// tea.Cmd by invoking it synchronously and re-feeding any non-nil
// resulting msg back into Update. The loop terminates when either
// Update returns a nil Cmd or the Cmd's result is nil.
//
// Sequenced msgs are dispatched left-to-right; each is fully
// drained before the next is sent. Use this when a test cares
// about async behaviour (toast dismissal, autosave tick, etc.); the
// older step() helper in snapshot_test.go intentionally discards
// Cmds and is fine for tests that only care about state-after-Update.
//
// Phase 0 limitations:
//   - tea.Batch is not unpacked. Cmds that fan out to multiple
//     sub-Cmds will only have the first executed.
//   - tea.Quit's QuitMsg is fed back into Update like any other
//     message, which is harmless but may evolve as the App grows
//     a quit-acknowledgement msg.
func pump(t testing.TB, a App, msgs ...tea.Msg) App {
	t.Helper()
	var m tea.Model = a
	for _, msg := range msgs {
		m = drainOne(t, m, msg)
	}
	app, _ := m.(App)
	return app
}

func drainOne(t testing.TB, m tea.Model, msg tea.Msg) tea.Model {
	t.Helper()
	var cmd tea.Cmd
	m, cmd = m.Update(msg)
	for cmd != nil {
		next := cmd()
		if next == nil {
			break
		}
		m, cmd = m.Update(next)
	}
	return m
}

// newTestAppEmpty returns an App rooted at an empty t.TempDir(),
// with a fake clock wired but no container loaded. Useful for
// tests that exercise Workspace gestures (open-while-dirty, etc.).
func newTestAppEmpty(t testing.TB) (App, *fakeClock) {
	t.Helper()
	dir := t.TempDir()
	fc := newFakeClock()
	a := New(dir)
	a.backupDir = filepath.Join(dir, "backups")
	a.tick = fc.Tick
	a.toast.SetClock(fc.Tick)
	a.status.SetClock(fc.Tick)
	return a, fc
}
