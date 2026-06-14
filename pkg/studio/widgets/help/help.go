// Package help renders the studio contextual Help modal. SHIFT+? (or
// `?`) opens it; Esc closes it. The spike content is a static
// keyboard cheat sheet; per-(space, cell) contextual content lands as
// each space and cell type is built.
package help

import (
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/philipcunningham/fizzle/pkg/studio/theme"
)

// Model holds the help modal state.
type Model struct {
	open bool
}

// New returns a closed help modal.
func New() Model { return Model{} }

// IsOpen reports whether the modal is currently shown.
func (m Model) IsOpen() bool { return m.open }

// Open shows the modal.
func (m *Model) Open() { m.open = true }

// Close hides the modal.
func (m *Model) Close() { m.open = false }

// Toggle flips the modal's visibility.
func (m *Model) Toggle() { m.open = !m.open }

// View renders the modal. Returns "" when closed.
func (m Model) View() string {
	if !m.open {
		return ""
	}
	sections := []struct {
		heading string
		rows    [][]string
	}{
		{
			heading: "Spaces",
			rows: [][]string{
				{"W", "Workspace", "directory browser"},
				{"P", "Pool", "basket of voices"},
				{"L", "Layout", "in-focus disk: banks + Areas"},
				{"S", "Sound", "sculpt the selected voice"},
			},
		},
		{
			heading: "Navigation",
			rows: [][]string{
				{"", "arrows", "move cursor within space"},
				{"", "SHIFT+up / down", "move between spaces"},
			},
		},
		{
			heading: "Layout",
			rows: [][]string{
				{"", "Enter", "open a bank, then an Area for editing"},
				{"", "i", "import a voice from the pool into the focused Area"},
				{"", "r (or F2)", "rename the focused voice"},
				{"", "c (or Ctrl-E)", "copy the focused Area's voice into the pool"},
				{"", "Delete", "clear the focused Area"},
			},
		},
		{
			heading: "Sound (numeric edit mode)",
			rows: [][]string{
				{"", "↑ / ↓", "step value ±1"},
				{"", "Shift+↑ / ↓", "step value ±10"},
				{"", "PgUp / PgDn", "step value ±100"},
				{"", "Alt+↑ / ↓", "step value ±1000"},
				{"", "0-9 (or -)", "type a value directly; Enter commits"},
			},
		},
		{
			heading: "Actions",
			rows: [][]string{
				{"", "Space", "audition the selected voice"},
				{"", "Enter", "drill in / commit"},
				{"", "Esc", "dismiss / cancel"},
				{"", "Ctrl-Z / Ctrl-Y", "undo / redo"},
				{"", "n", "new disk (untitled FZF)"},
				{"", "e", "export focused Pool entry to .fzv"},
				{"", "Ctrl-R", "refresh Workspace listing"},
				{"", "Ctrl-S", "save"},
				{"", "Ctrl-Q", "quit"},
				{"", "?", "this help"},
			},
		},
	}
	parts := []string{theme.Heading.Render("Studio Help"), ""}
	for _, s := range sections {
		body := table.New().
			Border(lipgloss.HiddenBorder()).
			BorderTop(false).BorderBottom(false).
			BorderLeft(false).BorderRight(false).
			BorderHeader(false).BorderColumn(false).BorderRow(false).
			Rows(s.rows...).
			StyleFunc(func(_, col int) lipgloss.Style {
				switch col {
				case 0:
					return theme.AccentText.Padding(0, 1)
				case 1:
					return theme.PrimaryText.Padding(0, 1)
				default:
					return theme.SilverText.Padding(0, 1)
				}
			}).
			Render()
		parts = append(parts, theme.AccentText.Render(s.heading), body, "")
	}
	parts = append(parts, theme.DimText.Render("Esc to close"))
	body := strings.Join(parts, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 2).
		Render(body)
}
