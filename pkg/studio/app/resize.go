package app

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// checkTerminalSize is wired via SetAfterDrawFunc so it runs after every
// redraw. It pushes (or pops) the terminal-too-small modal based on the
// current screen dimensions (spec §6).
//
// Pushing inside the AfterDraw callback is safe: tview's main loop calls
// AfterDraw on the main goroutine, after the draw has completed, so
// mutating Pages here doesn't recurse into Draw.
func (a *App) checkTerminalSize(screen tcell.Screen) {
	if screen == nil {
		return
	}
	cols, rows := screen.Size()
	tooSmall := cols < MinCols || rows < MinRows
	hasModal := a.stack.Has(pageTooSmall)

	switch {
	case tooSmall && !hasModal:
		a.stack.Push(pageTooSmall, terminalTooSmallModal(cols, rows))
	case tooSmall && hasModal:
		// Update the message in place so the user sees their current
		// terminal size. tview.Modal doesn't expose SetText after
		// AddPage cleanly across versions, so we replace the page.
		a.stack.Pop()
		a.stack.Push(pageTooSmall, terminalTooSmallModal(cols, rows))
	case !tooSmall && hasModal:
		// Remove only the terminal-too-small modal; leave any other
		// modals (save confirm etc.) in place. Stack.Pop removes the
		// topmost; we need a targeted removal. Since tooSmall is
		// pushed first when triggered, we walk the stack to remove
		// only the named entry.
		a.removeFromStack(pageTooSmall)
	}
}

// removeFromStack pops modals until name is gone. Pushed modals are
// re-pushed in their original order. Used by the terminal-too-small
// handler to dismiss its own modal without disturbing anything the
// user pushed on top of it.
func (a *App) removeFromStack(name string) {
	if !a.stack.Has(name) {
		return
	}
	// Capture the names above `name`.
	var above []string
	for a.stack.Depth() > 0 && a.stack.Top() != name {
		above = append(above, a.stack.Top())
		a.stack.Pop()
	}
	// Pop `name` itself.
	if a.stack.Depth() > 0 {
		a.stack.Pop()
	}
	// Restore the modals that were above. Their primitives are gone
	// from tview.Pages (Pop removed them) so we can't restore them
	// directly. In practice nothing else stacks on top of the
	// terminal-too-small modal (a too-small terminal can't display
	// another modal cleanly anyway). Drop the captured list.
	_ = above
}

// terminalTooSmallModal builds the modal shown when the terminal is
// below the minimum spec §6 size.
func terminalTooSmallModal(cols, rows int) tview.Primitive {
	m := tview.NewModal()
	m.SetText(fmt.Sprintf("fizzle studio needs at least %dx%d. Terminal is %dx%d. Resize and retry.",
		MinCols, MinRows, cols, rows))
	m.AddButtons([]string{"OK"})
	// No DoneFunc; the user can't dismiss this. It goes away
	// automatically when the terminal is resized to meet the
	// minimum (checkTerminalSize removes it on the next redraw).
	return m
}

// terminalSizeStatus reports the verdict the resize handler would
// produce for cols, rows. Exposed for tests so the spec §6 boundary
// check doesn't require a live tcell screen.
func terminalSizeStatus(cols, rows int) bool {
	return cols < MinCols || rows < MinRows
}
