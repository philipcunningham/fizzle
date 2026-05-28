// Package statusline implements the bottom-of-screen status line widget for
// fizzle studio (spec §2.4, §9.5).
//
// The widget is a single tview.TextView with style-tag markup for error
// (red), warning (yellow), and info (default-grey) text. Callers use the
// helper methods (SetError, SetWarning, SetInfo, Clear) instead of
// constructing tview style tags inline. This keeps formatting consistent
// and means the status-line widget owns its colour vocabulary.
//
// k9s adapts the same pattern in its logo widget; the dim-grey-info /
// yellow-warning / red-error palette comes from k9s.
package statusline

import "github.com/rivo/tview"

// Style tags used by the helper methods. They are unexported so callers
// route through SetError/SetWarning/SetInfo and the studio's status-line
// look stays consistent across packages.
const (
	tagError   = "[red]"
	tagWarning = "[yellow]"
	tagInfo    = "[white]"
	tagReset   = "[-]"
)

// Widget is a status-line text view. Construct via New.
type Widget struct {
	view *tview.TextView
}

// New returns a fresh status-line widget with dynamic-colors enabled.
func New() *Widget {
	v := tview.NewTextView()
	v.SetDynamicColors(true)
	v.SetTextAlign(tview.AlignLeft)
	return &Widget{view: v}
}

// Primitive exposes the underlying tview primitive so the app shell can
// embed the widget in a Flex.
func (w *Widget) Primitive() tview.Primitive { return w.view }

// SetError renders msg in red.
func (w *Widget) SetError(msg string) {
	w.view.SetText(tagError + msg + tagReset)
}

// SetWarning renders msg in yellow.
func (w *Widget) SetWarning(msg string) {
	w.view.SetText(tagWarning + msg + tagReset)
}

// SetInfo renders msg in the default (info) colour. Used for save
// confirmation, audition state, and field validation hints.
func (w *Widget) SetInfo(msg string) {
	w.view.SetText(tagInfo + msg + tagReset)
}

// Clear empties the status line.
func (w *Widget) Clear() {
	w.view.SetText("")
}

// Text returns the raw text including style tags. Primarily for tests.
func (w *Widget) Text() string {
	return w.view.GetText(false)
}
