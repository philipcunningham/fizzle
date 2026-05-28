package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/philipcunningham/fizzle/pkg/studio/helpers"
	"github.com/philipcunningham/fizzle/pkg/version"
)

// showHelp pushes a help overlay built from the KeyRegistry. Each
// registered KeyAction appears on its own line, sorted by Name. Esc
// dismisses.
func (a *App) showHelp() {
	view := tview.NewTextView()
	view.SetDynamicColors(true)
	view.SetText(renderHelp(a.keys))
	view.SetBorder(true)
	view.SetTitle(" Help (Esc to close) ")

	view.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			a.stack.Pop()
			return nil
		}
		return event
	})

	a.stack.Push(pageHelp, centreInBox(view, 70, 20))
}

// renderHelp formats the registry into a help-overlay string. Entries
// are sorted alphabetically by Name so the layout is stable for
// screenshot tests and so users find a binding by scanning rather than
// by registration order.
func renderHelp(r *helpers.KeyRegistry) string {
	actions := r.All()
	sort.Slice(actions, func(i, j int) bool {
		return actions[i].Name < actions[j].Name
	})

	var b strings.Builder
	b.WriteString("Keyboard shortcuts:\n\n")
	for _, a := range actions {
		// The registry's Name field already carries the user-facing
		// chord (e.g. "Ctrl+S", "Alt+1", "1..9"); there's no value in
		// also showing tcell's internal key name in a second column.
		fmt.Fprintf(&b, "  %-12s  %s\n", a.Name, a.Description)
	}
	// Version footer. The header line omits the commit hash to stay
	// compact; the help overlay is the right place to surface the
	// running build.
	fmt.Fprintf(&b, "\nfizzle %s", version.Version)
	return b.String()
}
