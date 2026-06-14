package status

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// recordingClock is a TickFn that records the (delay, fn) pair each
// time it is called and returns a no-op tea.Cmd. Tests inspect
// pending to assert what timers status scheduled and fire them on
// demand to drive Dismiss.
type recordingClock struct {
	pending []recordedTick
}

type recordedTick struct {
	delay time.Duration
	fn    func(time.Time) tea.Msg
}

func (c *recordingClock) Tick(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
	c.pending = append(c.pending, recordedTick{delay: d, fn: fn})
	return func() tea.Msg { return nil }
}

// fireAll invokes every recorded tick in order and returns the
// emitted msgs. After this call pending is empty.
func (c *recordingClock) fireAll() []tea.Msg {
	out := make([]tea.Msg, 0, len(c.pending))
	for _, p := range c.pending {
		out = append(out, p.fn(time.Now()))
	}
	c.pending = nil
	return out
}

// newModelWithClock wires a fresh Model with the recordingClock as
// its TickFn so the per-severity dismiss timers are inspectable
// without wall-clock waits.
func newModelWithClock() (*Model, *recordingClock) {
	c := &recordingClock{}
	m := New()
	m.SetClock(c.Tick)
	return &m, c
}

// TestStatus_Set_InfoSchedulesDismissAt4s pins the spec: an Info
// message must schedule a dismiss tick at InfoDuration.
func TestStatus_Set_InfoSchedulesDismissAt4s(t *testing.T) {
	m, c := newModelWithClock()
	cmd := m.Set(Info, "info message")
	if cmd == nil {
		t.Fatal("Set(Info, ...) returned nil cmd; expected the dismiss tick")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("recording-clock Tick should return nil; got %T", msg)
	}
	if len(c.pending) != 1 {
		t.Fatalf("pending ticks = %d, want 1", len(c.pending))
	}
	if got, want := c.pending[0].delay, InfoDuration; got != want {
		t.Errorf("Info dismiss delay = %v, want %v", got, want)
	}
	if want := 4 * time.Second; InfoDuration != want {
		t.Errorf("InfoDuration = %v, want %v", InfoDuration, want)
	}
}

// TestStatus_Set_SuccessSchedulesDismissAt4s pins the spec: a
// Success message must schedule a dismiss tick at SuccessDuration.
func TestStatus_Set_SuccessSchedulesDismissAt4s(t *testing.T) {
	m, c := newModelWithClock()
	cmd := m.Set(Success, "saved")
	if cmd == nil {
		t.Fatal("Set(Success, ...) returned nil cmd; expected the dismiss tick")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("recording-clock Tick should return nil; got %T", msg)
	}
	if len(c.pending) != 1 {
		t.Fatalf("pending ticks = %d, want 1", len(c.pending))
	}
	if got, want := c.pending[0].delay, SuccessDuration; got != want {
		t.Errorf("Success dismiss delay = %v, want %v", got, want)
	}
	if want := 4 * time.Second; SuccessDuration != want {
		t.Errorf("SuccessDuration = %v, want %v", SuccessDuration, want)
	}
}

// TestStatus_Set_WarningSchedulesDismissAt8s pins the spec: a
// Warning message must schedule a dismiss tick at WarningDuration.
func TestStatus_Set_WarningSchedulesDismissAt8s(t *testing.T) {
	m, c := newModelWithClock()
	cmd := m.Set(Warning, "watch out")
	if cmd == nil {
		t.Fatal("Set(Warning, ...) returned nil cmd; expected the dismiss tick")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("recording-clock Tick should return nil; got %T", msg)
	}
	if len(c.pending) != 1 {
		t.Fatalf("pending ticks = %d, want 1", len(c.pending))
	}
	if got, want := c.pending[0].delay, WarningDuration; got != want {
		t.Errorf("Warning dismiss delay = %v, want %v", got, want)
	}
	if want := 8 * time.Second; WarningDuration != want {
		t.Errorf("WarningDuration = %v, want %v", WarningDuration, want)
	}
}

// TestStatus_Set_ErrorIsStickyNoDismissScheduled pins the spec:
// Error severity must NOT schedule a dismiss. The message stays
// visible until acknowledged via Cancel (or replaced by another
// Set).
func TestStatus_Set_ErrorIsStickyNoDismissScheduled(t *testing.T) {
	m, c := newModelWithClock()
	cmd := m.Set(Error, "boom")
	if cmd != nil {
		// Drain it; if it scheduled a tick, pending would grow.
		if msg := cmd(); msg != nil {
			t.Fatalf("Set(Error, ...) returned a cmd that produced %T; expected nil cmd", msg)
		}
	}
	if len(c.pending) != 0 {
		t.Errorf("Error scheduled %d ticks; want 0 (sticky)", len(c.pending))
	}
	if m.View() == "" {
		t.Errorf("Error View is empty; want the message rendered")
	}
}

// TestStatus_Dismiss_StaleTokenIgnored pins the token guard: an
// older Set's dismiss tick must not clear a newer message. We Set
// Info, then Set Warning; firing the Info tick first must NOT clear
// the Warning.
func TestStatus_Dismiss_StaleTokenIgnored(t *testing.T) {
	m, c := newModelWithClock()
	cmd1 := m.Set(Info, "info first")
	_ = cmd1()
	cmd2 := m.Set(Warning, "warning second")
	_ = cmd2()

	if len(c.pending) != 2 {
		t.Fatalf("pending ticks = %d, want 2", len(c.pending))
	}

	// Pre-condition: View renders the Warning text.
	if got := m.View(); got == "" {
		t.Fatal("View is empty before any dismiss; want the Warning")
	}

	msgs := c.fireAll()
	if len(msgs) != 2 {
		t.Fatalf("fireAll returned %d msgs, want 2", len(msgs))
	}

	// Feed the FIRST (older) dismiss msg. Warning must survive
	// because its token is newer.
	firstDismiss, ok := msgs[0].(DismissMsg)
	if !ok {
		t.Fatalf("first emitted msg = %T, want DismissMsg", msgs[0])
	}
	m.Dismiss(firstDismiss)
	if m.View() == "" {
		t.Errorf("stale Info dismiss cleared the Warning; View went empty")
	}

	// Feed the SECOND (current) dismiss msg. The Warning clears.
	secondDismiss, ok := msgs[1].(DismissMsg)
	if !ok {
		t.Fatalf("second emitted msg = %T, want DismissMsg", msgs[1])
	}
	m.Dismiss(secondDismiss)
	if m.View() != "" {
		t.Errorf("current dismiss did not clear the Warning; View = %q", m.View())
	}
}

// TestStatus_Cancel_ClearsErrorImmediately pins the Esc-acknowledges
// path: Cancel clears whatever message is showing, even an Error
// (which would otherwise stay sticky).
func TestStatus_Cancel_ClearsErrorImmediately(t *testing.T) {
	m, _ := newModelWithClock()
	_ = m.Set(Error, "fatal")
	if m.View() == "" {
		t.Fatal("Error View is empty after Set; want the message rendered")
	}
	m.Cancel()
	if m.View() != "" {
		t.Errorf("Cancel did not clear; View = %q", m.View())
	}
}
