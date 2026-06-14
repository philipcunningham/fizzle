// Package hint renders contextual guidance text inside the bounded
// pane of a space. Each space supplies its own Hint string describing
// what the user is looking at and what the common next moves are.
// Sits above the keybinding footer so the two are visually distinct:
// hint is sentences, footer is keystrokes.
package hint

import (
	"charm.land/lipgloss/v2"

	"github.com/philipcunningham/fizzle/pkg/studio/theme"
)

// View renders the hint text as a single line. The caller is
// responsible for keeping summary short enough to fit. Returns ""
// when summary is empty.
func View(_ int, summary string) string {
	if summary == "" {
		return ""
	}
	return summaryStyle.Render(summary)
}

var summaryStyle = lipgloss.NewStyle().
	Foreground(theme.ContrastSecondary)
