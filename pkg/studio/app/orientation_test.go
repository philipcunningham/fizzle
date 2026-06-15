package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/philipcunningham/fizzle/pkg/studio/widgets/help"
)

// TestApp_ResizeGate_ShowsTaglineAndQuit pins F-01/F-02: the resize
// gate (shown when the terminal is below the minimum) is not a bare
// dead-end. It states what the tool is (the FZ series tagline) and
// how to leave (Ctrl-Q), so first contact on a small terminal still
// orients the user.
func TestApp_ResizeGate_ShowsTaglineAndQuit(t *testing.T) {
	a, _ := newTestAppEmpty(t)
	a = pump(t, a, tea.WindowSizeMsg{Width: 80, Height: 20}) // below minCols/minRows

	if !a.tooSmall {
		t.Fatal("80x20 should trip the tooSmall gate")
	}
	rendered := renderView(a)
	if !strings.Contains(rendered, help.ProductTagline) {
		t.Errorf("resize gate does not show the product tagline")
	}
	if !strings.Contains(rendered, "Ctrl-Q") {
		t.Errorf("resize gate does not tell the user how to quit")
	}
}
