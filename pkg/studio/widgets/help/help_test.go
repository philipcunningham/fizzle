package help

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/philipcunningham/fizzle/pkg/studio/nav"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/minimap"
)

// allSpaces is every context the help modal can render for.
var allSpaces = []minimap.Space{
	minimap.Workspace, minimap.Pool, minimap.Layout, minimap.Sound,
}

// unionRendered concatenates the help modal rendered in every space
// context, so a discoverability assertion can require that a binding is
// documented in at least one context.
func unionRendered() string {
	m := New()
	m.Open()
	var b strings.Builder
	for _, s := range allSpaces {
		b.WriteString(m.View(s))
		b.WriteString("\n")
	}
	return b.String()
}

// TestHelp_DocumentsEveryContextBinding is the discoverability contract
// (F-05, F-12) for context-scoped actions: every space-specific
// nav.Action must be findable in the Help modal in at least one context.
// Help is per-context (N-01), so the guarantee is over the union of
// contexts. Global actions (save, undo/redo, quit, help, switch spaces)
// are deliberately NOT here: they live in the always-visible bottom bar
// (asserted in the app package), so repeating them in the modal would be
// redundant clutter.
func TestHelp_DocumentsEveryContextBinding(t *testing.T) {
	rendered := unionRendered()

	want := map[nav.Action]string{
		nav.Copy:        "ctrl-c",
		nav.Paste:       "ctrl-v",
		nav.Duplicate:   "ctrl-d",
		nav.Extract:     "ctrl-e",
		nav.Refresh:     "ctrl-r",
		nav.Rename:      "rename",
		nav.Delete:      "clear",
		nav.Import:      "import",
		nav.NewDisk:     "new disk",
		nav.Export:      "export",
		nav.Audition:    "audition",
		nav.EditArea:    "key/velocity range",
		nav.EditEffects: "effects",
		nav.Move:        "swap",
		nav.Confirm:     keyEnter,
		nav.Cancel:      "esc",
	}
	for act, token := range want {
		if !strings.Contains(rendered, token) {
			t.Errorf("no help context documents %q (for nav action %d)", token, act)
		}
	}
}

// TestHelp_EachContextFitsWindow pins N-01: every context must fit the
// supported 140x30 terminal so no section is clipped or unreachable.
// The modal is centred, so its height must not exceed the row count.
func TestHelp_EachContextFitsWindow(t *testing.T) {
	const maxRows = 30
	m := New()
	m.Open()
	for _, s := range allSpaces {
		if h := lipgloss.Height(m.View(s)); h > maxRows {
			t.Errorf("help context %d is %d rows tall; must fit %d", s, h, maxRows)
		}
	}
}

// TestHelp_ShowsTaglineAndGlossary pins the orientation content (F-02):
// the tagline appears in every context, and the Workspace context (where
// a newcomer starts) defines the core nouns.
func TestHelp_ShowsTaglineAndGlossary(t *testing.T) {
	m := New()
	m.Open()

	if !strings.Contains(ProductTagline, "FZ series") {
		t.Errorf("tagline should name the FZ series, got %q", ProductTagline)
	}
	for _, s := range allSpaces {
		if !strings.Contains(m.View(s), ProductTagline) {
			t.Errorf("context %d does not show the product tagline", s)
		}
	}
	ws := m.View(minimap.Workspace)
	for _, term := range []string{"Disk", "Full dump", "Bank", "Area", "Voice", termPool} {
		if !strings.Contains(ws, term) {
			t.Errorf("Workspace glossary is missing the term %q", term)
		}
	}
}

// TestHelp_TitleNamesTheContext pins the contextual title: now that
// help is per-space, the heading names the focused space ("Layout Help")
// rather than a generic "Studio Help".
func TestHelp_TitleNamesTheContext(t *testing.T) {
	m := New()
	m.Open()
	want := map[minimap.Space]string{
		minimap.Workspace: "Workspace Help",
		minimap.Pool:      "Pool Help",
		minimap.Layout:    "Layout Help",
		minimap.Sound:     "Sound Help",
	}
	for space, title := range want {
		if v := m.View(space); !strings.Contains(v, title) {
			t.Errorf("context %d title = want %q in:\n%s", space, title, v)
		}
	}
}

// TestHelp_DisambiguatesCopy pins F-06: the pool gesture and the cell
// clipboard gesture are not both called "copy". The Area-to-pool action
// reads "send ... to the pool"; "copy" is reserved for the Ctrl-C/Ctrl-V
// cell clipboard.
func TestHelp_DisambiguatesCopy(t *testing.T) {
	m := New()
	m.Open()

	if !strings.Contains(m.View(minimap.Layout), "send the focused Area's voice to the pool") {
		t.Errorf("Layout help should describe the pool gesture as 'send ... to the pool'")
	}
	if !strings.Contains(m.View(minimap.Sound), "copy / paste a stage, envelope, loop or LFO cell") {
		t.Errorf("Sound help should reserve 'copy' for the cell clipboard")
	}
}
