package app

import (
	"strings"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/studio/spaces/pool"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/minimap"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
)

// TestApp_AssignReportsStatus pins N-06: assigning a pooled voice into
// an Area prints a confirmation, not nothing. We build a valid FZV pool
// entry, assign it, and require a success status that names the
// destination.
func TestApp_AssignReportsStatus(t *testing.T) {
	st := newJourneyWithFixture(t, pianoFixture)
	st.a.current = minimap.Layout
	st.a.minimap.Current = minimap.Layout

	// A real, full-size FZV (header + audio) so the assign passes its
	// validation and reaches the success path.
	samples := make([]int16, 1024)
	fzv := voiceimport.Encode(samples, 0, "TESTVOX", 0, voiceimport.NoLoop())
	entry := &pool.Entry{Name: "TESTVOX", Bytes: fzv}

	st.a = st.a.assignPoolEntryToArea(entry, 0, 0)

	if !st.a.status.HasMessage() {
		t.Fatal("assign produced no status feedback (N-06)")
	}
	if msg := st.a.status.View(); !strings.Contains(msg, "Imported") {
		t.Errorf("assign status = %q, want a success message naming the import", msg)
	}
}
