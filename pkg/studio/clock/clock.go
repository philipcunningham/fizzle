// Package clock is the test seam for tea.Tick-driven timers. The
// studio App and the toast widget both schedule dismissals /
// autosaves via tea.Tick, which blocks for the real wall-clock
// duration when executed. Tests inject a FakeClock that records
// Tick calls instead of sleeping, then fires them on demand.
package clock

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// TickFn matches tea.Tick's signature. The studio App and the
// toast widget both schedule timer-driven messages (autosave
// every 30s, toast dismiss after 3s) by calling a TickFn rather
// than tea.Tick directly. Production wires Real(); tests inject
// a fakeClock that records the (delay, fn) pair and returns a
// no-op tea.Cmd, so the pump-style test driver terminates
// immediately instead of sleeping for wall-clock duration.
//
// The test-side fake lives in pkg/studio/app/testdriver_test.go
// (fakeClock + its Tick / Pending / FireAll / FireMatching
// methods). Look there for the canonical pattern when wiring a
// new timer source through this seam.
type TickFn func(d time.Duration, f func(time.Time) tea.Msg) tea.Cmd

// Real returns the production TickFn: a thin reference to
// tea.Tick. Callers store the result in a field so they can swap
// it for a fake in tests without changing call sites.
func Real() TickFn { return tea.Tick }
