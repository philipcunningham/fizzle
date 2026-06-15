// Package theme is the studio palette. Every widget imports from
// here; no widget should construct its own lipgloss.Color. The palette
// matches the FZ-1 / FZ-10M LCD aesthetic: black background, dark
// cyan accents, dim gray borders, white primary text, silver
// secondary.
package theme

import (
	"charm.land/lipgloss/v2"
)

// Colours.
var (
	Background        = lipgloss.Color("#000000")
	Primary           = lipgloss.Color("#FFFFFF") // white
	Secondary         = lipgloss.Color("#008B8B") // dark cyan
	Title             = lipgloss.Color("#008B8B") // dark cyan
	Border            = lipgloss.Color("#696969") // dim gray
	Graphics          = lipgloss.Color("#696969") // dim gray
	Tertiary          = lipgloss.Color("#696969") // dim gray
	ContrastSecondary = lipgloss.Color("#C0C0C0") // silver
	Warning           = lipgloss.Color("#FFB000") // amber
	Error             = lipgloss.Color("#FF4040") // red
)

// Styles.
var (
	Heading = lipgloss.NewStyle().Foreground(Title).Bold(true)

	BorderBox = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(Border)

	PrimaryText = lipgloss.NewStyle().Foreground(Primary)
	DimText     = lipgloss.NewStyle().Foreground(Tertiary)
	AccentText  = lipgloss.NewStyle().Foreground(Secondary)
	SilverText  = lipgloss.NewStyle().Foreground(ContrastSecondary)
	WarnText    = lipgloss.NewStyle().Foreground(Warning)
	ErrorText   = lipgloss.NewStyle().Foreground(Error)
)

// Field renders a "label: value" line in the studio palette, with a
// focus caret and an underlined accent value when focused. Shared by
// the area and effects editors so their field rows render identically.
func Field(label, value string, focused bool) string {
	caret := "  "
	if focused {
		caret = AccentText.Render("▶ ")
	}
	labelStr := PrimaryText.Render(label + ": ")
	if focused {
		return caret + labelStr + AccentText.Underline(true).Render(value)
	}
	return caret + labelStr + DimText.Render(value)
}
