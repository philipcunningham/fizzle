// Package topbar renders the studio application's title banner. The
// bar carries the product name (`fizzle`) and a sub-label (`studio`)
// on the left, and the in-focus container's display path on the right
// when one is loaded. Styled as a solid coloured strip so it reads as
// distinct from the body and from the modal stack.
package topbar

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/philipcunningham/fizzle/pkg/studio/theme"
)

// View renders the banner. width is the terminal column count;
// the bar always fills it exactly (left label + path tail aligned
// right, with spaces filling the gap). containerPath is hard-
// truncated keep-tail style if it can't fit alongside the left
// label, preserving the layout-line-fits-in-terminal invariant
// across long fixture paths.
func View(width int, containerPath string) string {
	if width < 1 {
		return ""
	}
	const leftText = " fizzle studio "
	leftRunes := []rune(leftText)
	if width <= len(leftRunes) {
		return barStyle.Render(string(leftRunes[:width]))
	}

	right := containerPath
	rightRunes := []rune(right)
	maxRightW := width - len(leftRunes)
	// Truncate path keep-tail style (the basename is what the user
	// scans for); leave one cell of breathing room before the path.
	if len(rightRunes) > maxRightW-1 {
		if maxRightW <= 1 {
			right = ""
			rightRunes = nil
		} else {
			rightRunes = rightRunes[len(rightRunes)-(maxRightW-1):]
			right = string(rightRunes)
		}
	}
	gap := width - len(leftRunes) - len(rightRunes)
	if gap < 0 {
		gap = 0
	}
	return barStyle.Render(leftText + strings.Repeat(" ", gap) + right)
}

var barStyle = lipgloss.NewStyle().
	Background(theme.Title).
	Foreground(theme.ContrastSecondary)
