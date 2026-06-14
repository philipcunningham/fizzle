// Package toast is a transient, prominent banner that surfaces a
// successful action above the always-visible status line. Unlike
// status (which sits one line, dim, and persists until replaced), a
// toast renders in a boxed accent style and auto-dismisses on a
// tea.Tick after Duration.
//
// Used today for the post-Save "Saved!" affordance: status logs the
// path; toast catches the eye.
package toast

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/philipcunningham/fizzle/pkg/studio/clock"
	"github.com/philipcunningham/fizzle/pkg/studio/theme"
)

// Duration is how long a toast stays before the dismiss tick fires.
const Duration = 3 * time.Second

// DismissMsg is the tea.Msg the dismiss tick emits. The App matches
// on Token to ignore stale ticks (a newer Set replaces the token, so
// the older tick's clear is a no-op).
type DismissMsg struct {
	Token int
}

// Model is the toast state.
type Model struct {
	text  string
	token int
	tick  clock.TickFn
}

// New returns an empty toast wired with the real clock. Tests in
// the same package use SetClock to swap in a fake.
func New() Model { return Model{tick: clock.Real()} }

// SetClock injects a TickFn for the dismiss timer. Production
// callers don't use this; tests set a fake clock so dismiss
// behaviour is exercisable without wall-clock waits.
func (m *Model) SetClock(tick clock.TickFn) { m.tick = tick }

// Set replaces the current message and returns the tick command that
// will dismiss this instance after Duration. Each Set bumps the token
// so older dismiss ticks become no-ops.
func (m *Model) Set(text string) tea.Cmd {
	m.text = text
	m.token++
	tok := m.token
	tick := m.tick
	if tick == nil {
		tick = clock.Real()
	}
	return tick(Duration, func(time.Time) tea.Msg {
		return DismissMsg{Token: tok}
	})
}

// Dismiss clears the toast if msg.Token matches the current token.
// Older tokens fire-and-forget.
func (m *Model) Dismiss(msg DismissMsg) {
	if msg.Token == m.token {
		m.text = ""
	}
}

// View renders the toast, or "" when empty.
func (m Model) View() string {
	if m.text == "" {
		return ""
	}
	return toastStyle.Render(m.text)
}

var toastStyle = lipgloss.NewStyle().
	Foreground(theme.Background).
	Background(theme.Secondary).
	Bold(true).
	Padding(0, 2)
