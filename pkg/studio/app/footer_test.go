package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestGlobalFooter_DocumentsGlobals pins that the always-visible bottom
// bar carries the global actions (save, undo/redo, quit, help, switch
// spaces). The Help modal deliberately omits these to avoid redundancy,
// so this is where their discoverability is guaranteed (F-05, F-12).
func TestGlobalFooter_DocumentsGlobals(t *testing.T) {
	a, _ := newTestAppEmpty(t)
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})
	view := renderView(a)

	for _, token := range []string{
		"ctrl-s save",
		"ctrl-z/y undo/redo",
		"ctrl-q quit",
		"? help",
		"shift+up/down spaces",
	} {
		if !strings.Contains(view, token) {
			t.Errorf("global footer is missing %q", token)
		}
	}
}
