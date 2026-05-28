package app

import (
	"github.com/gdamore/tcell/v2"

	"github.com/philipcunningham/fizzle/pkg/studio/helpers"
)

// registerKeys populates the KeyRegistry with every app-level binding.
// The registry drives both the dispatch table inside handleAppKey and
// the Ctrl+H help overlay. Spec §9.3 calls out that having one
// source of truth prevents the two from drifting.
//
// The Name field is the user-facing label (e.g. "Ctrl+S") rather than
// a programmatic identifier; the help overlay sorts by Name and
// renders Name + Description.
func (a *App) registerKeys() {
	add := func(name string, key tcell.Key, desc string) {
		a.keys.Register(helpers.KeyAction{Name: name, Key: key, Description: desc})
	}
	add("Ctrl+S", tcell.KeyCtrlS, "Save changes to disk")
	add("Ctrl+Q", tcell.KeyCtrlQ, "Quit the studio")
	add("Ctrl+Z", tcell.KeyCtrlZ, "Undo last edit")
	add("Ctrl+Y", tcell.KeyCtrlY, "Redo last undone edit")
	add("Ctrl+I", tcell.KeyCtrlI, "Open file info overlay")
	add("Ctrl+H", tcell.KeyCtrlH, "Open keyboard help overlay")
	add("Alt+1", tcell.KeyRune, "Switch lower section to Voice Details")
	add("Alt+2", tcell.KeyRune, "Switch lower section to Loop Details")
	add("Alt+3", tcell.KeyRune, "Switch lower section to Global Effect")
	add("Shift+Tab", tcell.KeyBacktab, "Cycle panes (Voices, DCA, DCF, LFO, footer, ...)")
	add("Tab", tcell.KeyTab, "Move focus to the next field within the current pane")
	add("1..9", tcell.KeyRune, "Switch upper section tab (1=Voices, 2..N=banks)")
	add("Space", tcell.KeyRune, "Audition the focused voice (second press stops)")
	add("Escape", tcell.KeyEscape, "Dismiss the topmost modal")
}

// handleAppKey is the Application-level SetInputCapture handler. It
// catches universal shortcuts and lets everything else fall through to
// the focused widget. The handler runs on the main goroutine; per
// spec §9.4 it must NOT call QueueUpdate (deadlock risk).
func (a *App) handleAppKey(event *tcell.EventKey) *tcell.EventKey {
	// Escape from any active modal pops the stack rather than reaching
	// the modal's own input capture. tview.Modal already handles its
	// own Escape, but our centred overlays (info/help) don't, so we
	// dispatch here unconditionally.
	if event.Key() == tcell.KeyEscape && a.stack.Depth() > 0 {
		a.stack.Pop()
		return nil
	}
	// If any modal is on the stack, let it own all other input.
	if a.stack.Depth() > 0 {
		return event
	}

	switch event.Key() { //nolint:exhaustive // only universal shortcuts here
	case tcell.KeyBacktab:
		// Shift+Tab cycles between upper and lower panes. The lower
		// panel additionally cycles internally section-by-section
		// before wrapping back to the upper pane.
		//
		// Subtlety: when focus is on any InputField (in either pane)
		// the app must pass Shift+Tab through to the field's DoneFunc
		// so the user's typed value commits. The widget's
		// handleDoneKey then performs the pane handoff: lower-pane
		// fields call CycleSection (which may trigger onCycleOut ->
		// focusUpperPane); upper-pane (bank tab) fields call
		// onShiftTab -> shiftTabCycle -> focusLowerPane.
		if a.inputFieldFocused() {
			return event
		}
		a.shiftTabCycle()
		return nil
	case tcell.KeyCtrlS:
		a.flushFocusedInputField()
		a.showSaveConfirm()
		return nil
	case tcell.KeyCtrlQ:
		a.flushFocusedInputField()
		a.quit()
		return nil
	case tcell.KeyCtrlZ:
		if err := a.m.Undo(); err == nil {
			a.status.SetInfo("Undid last edit")
		}
		return nil
	case tcell.KeyCtrlY:
		if err := a.m.Redo(); err == nil {
			a.status.SetInfo("Redid edit")
		}
		return nil
	case tcell.KeyCtrlI:
		a.showInfo()
		return nil
	case tcell.KeyCtrlH:
		a.showHelp()
		return nil
	case tcell.KeyRune:
		return a.handleRune(event)
	}
	return event
}

// handleRune routes printable runes that act as universal shortcuts.
//
// Alt+1/2/3 (lower-tab switch) work even when an InputField has focus;
// they're meta shortcuts that should always be available. Plain-rune
// shortcuts (S, Q, 1..9, [, ], Space) MUST defer to text entry when
// an InputField has focus.
func (a *App) handleRune(event *tcell.EventKey) *tcell.EventKey {
	r := event.Rune()

	// Alt+1/2/3 select the lower tab regardless of focus. tcell encodes
	// these as ModAlt + the corresponding rune.
	if event.Modifiers()&tcell.ModAlt != 0 && r >= '1' && r <= '3' {
		a.switchLowerTab(int(r - '1'))
		return nil
	}

	if a.inputFieldFocused() {
		return event
	}
	switch {
	case r >= '1' && r <= '9':
		a.switchUpperTab(int(r - '1')) // '1' -> 0 (Voices), '2' -> 1 (Bank 1), ...
		return nil
	case r == ' ':
		a.toggleAudition()
		return nil
	}
	return event
}
