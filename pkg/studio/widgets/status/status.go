// Package status is the studio status channel. A single-line surface
// at the bottom of the App view carries transient feedback in four
// severities. Each Set call replaces the current message immediately
// and schedules an auto-dismiss tick (except Error, which is sticky
// until acknowledged via Cancel).
//
// Per-severity dismiss windows match the README spec: Info and
// Success at 4 s, Warning at 8 s, Error sticky. A new Set bumps the
// internal token so older dismiss ticks (for replaced messages)
// become no-ops, defending against the rapid-Set race where the
// first message's tick would otherwise clear its replacement.
package status

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/philipcunningham/fizzle/pkg/studio/clock"
	"github.com/philipcunningham/fizzle/pkg/studio/theme"
)

// Per-severity auto-dismiss durations. Error has no constant: it is
// sticky by construction.
const (
	// InfoDuration is how long an Info message stays before the
	// dismiss tick fires.
	InfoDuration = 4 * time.Second
	// SuccessDuration is how long a Success message stays before the
	// dismiss tick fires.
	SuccessDuration = 4 * time.Second
	// WarningDuration is how long a Warning message stays before the
	// dismiss tick fires.
	WarningDuration = 8 * time.Second
)

// Severity is the message kind.
type Severity int

// Severity values identify the kind of status message being shown.
const (
	Info Severity = iota
	Success
	Warning
	Error
)

// DismissMsg is the tea.Msg the dismiss tick emits. The App matches
// on Token to ignore stale ticks (a newer Set bumps the token, so
// the older tick's clear is a no-op).
type DismissMsg struct {
	Token int
}

// Model is the status channel state.
type Model struct {
	severity Severity
	text     string
	token    int
	tick     clock.TickFn
}

// New returns an empty status model wired with the real clock.
// Tests use SetClock to swap in a fake.
func New() Model { return Model{tick: clock.Real()} }

// SetClock injects a TickFn for the dismiss timer. Production
// callers don't use this; tests set a fake clock so dismiss
// behaviour is exercisable without wall-clock waits.
func (m *Model) SetClock(tick clock.TickFn) { m.tick = tick }

// Set replaces the current message and returns the tick command
// that will dismiss this instance after the severity's duration.
// Error severity returns nil (sticky): the message stays until
// acknowledged via Cancel or replaced by a subsequent Set. Each
// Set bumps the token so older dismiss ticks become no-ops.
func (m *Model) Set(s Severity, text string) tea.Cmd {
	m.severity = s
	m.text = text
	m.token++
	if s == Error {
		// Sticky: no dismiss tick. The token bump above still
		// invalidates any pending tick from a prior non-Error Set,
		// so the Error doesn't inherit a stale dismiss.
		return nil
	}
	d := durationFor(s)
	tok := m.token
	tick := m.tick
	if tick == nil {
		tick = clock.Real()
	}
	return tick(d, func(time.Time) tea.Msg {
		return DismissMsg{Token: tok}
	})
}

// durationFor returns the auto-dismiss duration for a non-Error
// severity. The Error branch in Set short-circuits before reaching
// this helper; it is safe to receive Error here too (returns 0)
// because the Set branch returns nil before scheduling.
func durationFor(s Severity) time.Duration {
	switch s {
	case Info:
		return InfoDuration
	case Success:
		return SuccessDuration
	case Warning:
		return WarningDuration
	case Error:
		return 0
	}
	return 0
}

// Dismiss clears the message if msg.Token matches the current
// token. Older tokens (from a Set that has since been replaced)
// fire and forget.
func (m *Model) Dismiss(msg DismissMsg) {
	if msg.Token == m.token {
		m.severity = Info
		m.text = ""
	}
}

// Cancel clears the current message immediately, regardless of
// severity. Used to acknowledge a sticky Error (the user pressing
// Esc on a visible Error). Bumps the token so any pending tick
// from a future-replaced Set still resolves cleanly.
func (m *Model) Cancel() {
	m.severity = Info
	m.text = ""
	m.token++
}

// Clear removes the current message. Equivalent to Cancel today;
// retained as the legacy name for callers that pre-date the
// auto-dismiss wiring.
func (m *Model) Clear() {
	m.Cancel()
}

// Severity returns the current message's severity. Useful for the
// App to detect "is an Error visible?" before routing Esc to a
// status-acknowledge path rather than a modal-cancel path.
func (m Model) Severity() Severity { return m.severity }

// HasMessage reports whether the status currently carries text.
func (m Model) HasMessage() bool { return m.text != "" }

// View renders the status line.
func (m Model) View() string {
	if m.text == "" {
		return ""
	}
	var style lipgloss.Style
	switch m.severity {
	case Info:
		style = theme.SilverText
	case Success:
		style = theme.AccentText
	case Warning:
		style = theme.WarnText
	case Error:
		style = theme.ErrorText
	}
	return style.Render(m.text)
}
