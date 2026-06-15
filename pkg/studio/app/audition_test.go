package app

import (
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/studio/audio"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/minimap"
)

// TestApp_AuditionGivesFeedback pins N-09: pressing Space to audition
// always sets a status line, so the user gets on-screen confirmation
// even in a no-audio session (the noop backend stands in for one here).
func TestApp_AuditionGivesFeedback(t *testing.T) {
	audio.InstallNoopForTest(t)
	st := newJourneyWithFixture(t, pianoFixture)
	st.a.current = minimap.Layout
	st.a.minimap.Current = minimap.Layout

	st.a = pump(t, st.a, keyPress(testKeyEnter)) // drill into bank 0 (area list)
	st.a = pump(t, st.a, keyPress(" "))          // audition the focused Area

	if !st.a.status.HasMessage() {
		t.Error("audition produced no status feedback (N-09)")
	}
	// The feedback must reach the rendered frame, not just model state.
	if v := renderView(st.a); !strings.Contains(v, "auditioning") {
		t.Errorf("audition status not rendered (N-09):\n%s", v)
	}
}
