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
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/minimap"
)

// ProductTagline is the one-line description of what studio is, shown
// on the resize gate and in the Help header. It names the hardware
// family (FZ-1, FZ-10M, FZ-20M) rather than a single model so a
// first-time user learns the tool's purpose before needing any FZ
// fluency.
const ProductTagline = "Casio FZ series sampler disk editor (FZ-1, FZ-10M, FZ-20M)"

// Shared display literals, pulled out so the same word isn't repeated
// across the help tables and the help tests (which would trip goconst).
const (
	termPool = "Pool"
	keyEnter = "enter"
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

// section is a titled block of key/description rows in the help modal.
type section struct {
	heading string
	rows    [][]string
}

// Per-space sections. The modal shows only the focused space's section
// (plus the glossary on Workspace, where a first-time user starts), so
// it stays short enough to fit the supported 140x30 window without
// scrolling (N-01). The space sections carry no heading: the modal is
// already scoped to the focused space, so naming it again is redundant.
// Global actions (save, undo/redo, quit, help, switch spaces) live in
// the always-visible bottom bar rather than being repeated here.
var (
	glossarySection = section{"Glossary", [][]string{
		{"Disk", "an FZ floppy image (.img) you load into the editor"},
		{"Full dump", "a disk's complete contents (.fzf): all banks and voices"},
		{"Bank", "one of 8 performance sets; groups up to 64 Areas"},
		{"Area", "a key and velocity zone within a bank, mapped to a voice"},
		{"Voice", "one sampled sound (.fzv): audio plus its parameters"},
		{termPool, "a holding basket of voices you can drop into Areas"},
	}}
	workspaceSection = section{"", [][]string{
		{keyEnter, "open a disk, or descend into a folder"},
		{"left / esc", "go up a directory"},
		{"ctrl-r", "refresh the listing"},
		{"n", "new disk (untitled FZF)"},
	}}
	poolSection = section{"", [][]string{
		{"space", "audition the focused voice"},
		{keyEnter, "assign to the Area (in picker mode)"},
		{"e", "export the focused voice to .fzv"},
		{"del", "remove the focused voice from the pool"},
		{"i", "(on a Layout Area) import a pool voice into it"},
	}}
	layoutSection = section{"", [][]string{
		{keyEnter, "open a bank, then an Area for editing"},
		{"i", "import a pool voice into the focused Area"},
		{"r (or f2)", "rename the focused voice"},
		{"c (or ctrl-e)", "send the focused Area's voice to the pool"},
		{"ctrl-d", "duplicate the focused Area into a new voice slot"},
		{"a", "edit the focused Area's key/velocity range and config"},
		{"f", "edit the focused bank's effects (bend and mod matrix)"},
		{"m", "swap two Areas: press m on the source, then the target"},
		{"delete", "clear the focused Area"},
	}}
	soundSection = section{"", [][]string{
		{"up / down", "switch row"},
		{"left / right", "switch cell"},
		{keyEnter, "edit the focused cell"},
		{"ctrl-c / ctrl-v", "copy / paste a cell value"},
		{"esc", "back to Layout"},
		{"shift / pgup / alt + arrows", "bigger steps while editing a value"},
	}}
)

// spaceTitle is the modal heading for the focused space. Help is
// per-context, so the title names the space ("Layout Help") instead of a
// generic "Studio Help", making the scope obvious at a glance.
func spaceTitle(space minimap.Space) string {
	switch space {
	case minimap.Workspace:
		return "Workspace Help"
	case minimap.Pool:
		return "Pool Help"
	case minimap.Layout:
		return "Layout Help"
	case minimap.Sound:
		return "Sound Help"
	default:
		return "Studio Help"
	}
}

// contextSections returns the sections to show for the focused space.
func contextSections(space minimap.Space) []section {
	switch space {
	case minimap.Workspace:
		return []section{glossarySection, workspaceSection}
	case minimap.Pool:
		return []section{poolSection}
	case minimap.Layout:
		return []section{layoutSection}
	case minimap.Sound:
		return []section{soundSection}
	default:
		return nil
	}
}

// View renders the modal for the focused space. Returns "" when closed.
func (m Model) View(space minimap.Space) string {
	if !m.open {
		return ""
	}
	parts := []string{
		theme.Heading.Render(spaceTitle(space)),
		theme.DimText.Render(ProductTagline),
		"",
	}
	for _, s := range contextSections(space) {
		if s.heading != "" {
			parts = append(parts, theme.AccentText.Render(s.heading))
		}
		parts = append(parts, renderRows(s.rows), "")
	}
	parts = append(parts, theme.DimText.Render("esc to close"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 2).
		Render(strings.Join(parts, "\n"))
}

// renderRows renders a section's key/description rows as a borderless
// two-column table. The key column carries no left padding so the keys
// line up flush with the section heading rather than sitting indented
// under it.
func renderRows(rows [][]string) string {
	return table.New().
		Border(lipgloss.HiddenBorder()).
		BorderTop(false).BorderBottom(false).
		BorderLeft(false).BorderRight(false).
		BorderHeader(false).BorderColumn(false).BorderRow(false).
		Rows(rows...).
		StyleFunc(func(_, col int) lipgloss.Style {
			if col == 0 {
				// Keys are dark grey and de-emphasised; the description
				// (silver) carries the meaning. Padding(top,right,bottom,
				// left): no left pad aligns the key flush with the left
				// margin; right pad spaces the two columns.
				return theme.DimText.Padding(0, 2, 0, 0)
			}
			return theme.SilverText.Padding(0, 1)
		}).
		Render()
}
