package app

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// handleAppMouse is the Application-level mouse capture. It fires the
// currently-focused InputField's DoneFunc whenever the user starts a
// click OUTSIDE that field, so any typed-but-uncommitted text commits
// to the model before tview's click dispatch moves focus to whatever
// was clicked on.
//
// Without this hook, mouse-induced focus changes orphan the leaving
// field's pending edit. The Tab / Enter / Shift+Tab paths are
// unaffected because tview's InputField already fires DoneFunc on
// those keys (via commitOnDone).
//
// Returns the event and action unchanged so tview's normal mouse
// dispatch continues. Same-field clicks and non-mouse-down actions
// are intentional no-ops: re-flushing an unchanged field would
// generate redundant no-op patches and pollute undo history.
func (a *App) handleAppMouse(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
	if action != tview.MouseLeftDown {
		return event, action
	}
	in := a.focusedInputField()
	if in == nil {
		return event, action
	}
	x, y := event.Position()
	if pointInside(in, x, y) {
		return event, action
	}
	a.flushFocusedInputField()
	return event, action
}

// pointInside reports whether (x, y) lies within the InputField's
// rendered bounding box. Used by handleAppMouse to distinguish
// "click on same field" from "click elsewhere".
func pointInside(in *tview.InputField, x, y int) bool {
	rx, ry, rw, rh := in.GetRect()
	return x >= rx && x < rx+rw && y >= ry && y < ry+rh
}
