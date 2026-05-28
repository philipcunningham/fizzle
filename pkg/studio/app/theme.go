package app

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Studio theme.
//
// A black-terminal aesthetic with dark-cyan accents and dim-gray
// borders. Codified here so every widget that reads tview.Styles
// inherits the palette without each one hardcoding constants.
//
// Adjust here and re-run; every widget that reads tview.Styles
// inherits the new palette. Widgets that hardcode colours (e.g. red
// for errors) intentionally stay outside the theme.
var fzTheme = tview.Theme{
	// Terminal black is the panel background.
	PrimitiveBackgroundColor: tcell.ColorBlack,
	// Dark slate gray for contrasted regions: modal backgrounds and
	// focused-cell highlights short of full selection.
	ContrastBackgroundColor: tcell.ColorDarkSlateGray,
	// Dark cyan is the most-contrasted region (selection background).
	MoreContrastBackgroundColor: tcell.ColorDarkCyan,
	// Dim gray for understated panel borders.
	BorderColor: tcell.ColorDimGray,
	// Dark cyan for emphasised modal-frame borders and the header accent.
	TitleColor: tcell.ColorDarkCyan,
	// Graphics (envelope lines, separators) match the border tone.
	GraphicsColor: tcell.ColorDimGray,
	// White for primary text in browser entries and detail views.
	PrimaryTextColor: tcell.ColorWhite,
	// Dark cyan for labels and table headers (the accent colour).
	SecondaryTextColor: tcell.ColorDarkCyan,
	// Dim gray for secondary status text and dim labels.
	TertiaryTextColor: tcell.ColorDimGray,
	// Black on the bright accent backgrounds (button text pattern).
	InverseTextColor: tcell.ColorBlack,
	// Silver text on dark-slate-gray backgrounds.
	ContrastSecondaryTextColor: tcell.ColorSilver,
}

// applyTheme installs the studio palette into tview's global Styles.
// Must be called before any widget is constructed so the palette is
// captured at primitive-init time.
func applyTheme() {
	tview.Styles = fzTheme
}
