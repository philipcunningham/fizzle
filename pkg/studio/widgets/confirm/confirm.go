// Package confirm renders the studio confirmation modal: a centred
// box with a title, body, and a row of options. Callers receive the
// chosen option's Result value when the modal is dismissed.
package confirm

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/philipcunningham/fizzle/pkg/studio/theme"
)

// Option is one selectable choice.
type Option struct {
	Label  string
	Result int
}

// Prompt describes the modal's content.
type Prompt struct {
	Title   string
	Body    string
	Options []Option
}

// Model holds modal state.
type Model struct {
	open    bool
	prompt  Prompt
	focus   int      // index into prompt.Options
	resultC chan int // single-buffered; closed after a result lands
}

// New returns a closed modal.
func New() *Model { return &Model{} }

// IsOpen reports whether the modal is visible.
func (m *Model) IsOpen() bool { return m.open }

// Show opens the modal with the given prompt and returns a channel
// that yields the chosen option's Result. The channel is closed after
// the result is delivered. If the user dismisses with Esc and the
// prompt has an option labelled "Cancel", that option's Result is
// returned; otherwise -1.
func (m *Model) Show(p Prompt) <-chan int {
	m.open = true
	m.prompt = p
	m.focus = 0
	m.resultC = make(chan int, 1)
	return m.resultC
}

// Next moves focus to the next option (wraps).
func (m *Model) Next() {
	if !m.open || len(m.prompt.Options) == 0 {
		return
	}
	m.focus = (m.focus + 1) % len(m.prompt.Options)
}

// Prev moves focus to the previous option (wraps).
func (m *Model) Prev() {
	if !m.open || len(m.prompt.Options) == 0 {
		return
	}
	m.focus = (m.focus - 1 + len(m.prompt.Options)) % len(m.prompt.Options)
}

// Confirm dismisses the modal with the focused option's Result.
func (m *Model) Confirm() {
	if !m.open {
		return
	}
	r := -1
	if m.focus >= 0 && m.focus < len(m.prompt.Options) {
		r = m.prompt.Options[m.focus].Result
	}
	m.deliver(r)
}

// Cancel dismisses the modal. If an option is labelled "Cancel" its
// Result is returned; otherwise -1.
func (m *Model) Cancel() {
	if !m.open {
		return
	}
	r := -1
	for _, o := range m.prompt.Options {
		if strings.EqualFold(o.Label, "Cancel") {
			r = o.Result
			break
		}
	}
	m.deliver(r)
}

func (m *Model) deliver(r int) {
	if m.resultC != nil {
		m.resultC <- r
		close(m.resultC)
		m.resultC = nil
	}
	m.open = false
}

// View renders the modal. Returns "" when closed.
func (m *Model) View() string {
	if !m.open {
		return ""
	}
	lines := []string{
		theme.Heading.Render(m.prompt.Title),
		"",
		theme.PrimaryText.Render(m.prompt.Body),
		"",
	}
	if len(m.prompt.Options) > 0 {
		buttons := make([]string, 0, len(m.prompt.Options))
		for i, o := range m.prompt.Options {
			style := lipgloss.NewStyle().
				Foreground(theme.Tertiary).
				Padding(0, 2)
			if i == m.focus {
				style = lipgloss.NewStyle().
					Foreground(theme.Background).
					Background(theme.Title).
					Padding(0, 2).
					Bold(true)
			}
			buttons = append(buttons, style.Render(o.Label))
		}
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Center, buttons...))
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 3).
		Render(strings.Join(lines, "\n"))
}
