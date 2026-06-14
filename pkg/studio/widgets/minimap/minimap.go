// Package minimap renders the four-space minimap. At the top level
// it is a vertical stack of single-letter cues for each space, with
// the current space highlighted. Mirrors the M8 spatial map.
package minimap

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/philipcunningham/fizzle/pkg/studio/theme"
)

// Space identifies a top-level space for highlight purposes.
type Space int

// Space values identify each top-level studio space for the minimap.
const (
	Workspace Space = iota
	Pool
	Layout
	Sound
)

func (s Space) letter() string {
	switch s {
	case Workspace:
		return "W"
	case Pool:
		return "P"
	case Layout:
		return "L"
	case Sound:
		return "S"
	}
	return "?"
}

// Model renders the minimap. State is just the current space.
type Model struct {
	Current Space
}

// New returns a minimap initially pointing at Workspace.
func New() Model {
	return Model{Current: Workspace}
}

// View renders the minimap as a vertical stack of letters.
func (m Model) View() string {
	rows := make([]string, 0, 4)
	for _, s := range []Space{Workspace, Pool, Layout, Sound} {
		letter := s.letter()
		style := theme.SilverText
		if s == m.Current {
			style = theme.Heading
		}
		rows = append(rows, style.Render(letter))
	}
	stack := strings.Join(rows, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(theme.Border).
		Padding(0, 1).
		Render(stack)
}
