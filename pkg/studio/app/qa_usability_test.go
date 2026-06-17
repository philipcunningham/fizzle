package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestPoolDisplayName pins F-QA-24: pool confirmations use a clean base
// name (no directory, no extension), not a full path.
func TestPoolDisplayName(t *testing.T) {
	if got := poolDisplayName("/tmp/fz-qa/ws/amen 01.wav"); got != "amen 01" {
		t.Errorf("poolDisplayName = %q, want %q", got, "amen 01")
	}
}

// TestSound_CopyNonCopyableCellGivesFeedback pins F-QA-25: copying a cell
// that holds nothing copyable (the KF/rate columns) reports why instead
// of silently doing nothing. navInto lands on DCA col 1 ([lvlKF/VF]).
func TestSound_CopyNonCopyableCellGivesFeedback(t *testing.T) {
	st := newJourneyWithFixture(t, "synthetic/HOOVER.img")
	st = navInto(t, st, 0, 0)
	st.a = pump(t, st.a, tea.KeyPressMsg{Code: 'y', Text: "y"})
	got := stripANSI(st.a.status.View())
	if strings.TrimSpace(got) == "" {
		t.Error("copy on a non-copyable cell gave no feedback (silent no-op)")
	}
}

// TestNormaliseRenameKey_AllowsSlash pins F-QA-9: the rename filter must
// keep '/', which the FZ name charset uses (shipped banks like
// NSTY/DWN/PPG), so a slash-named bank/voice can be reproduced.
func TestNormaliseRenameKey_AllowsSlash(t *testing.T) {
	if got := normaliseRenameKey('/'); got != '/' {
		t.Errorf("normaliseRenameKey('/') = %q, want '/'", got)
	}
	// Existing behaviour intact: lowercase upper-cases, punctuation drops.
	if got := normaliseRenameKey('a'); got != 'A' {
		t.Errorf("normaliseRenameKey('a') = %q, want 'A'", got)
	}
	if got := normaliseRenameKey('!'); got != 0 {
		t.Errorf("normaliseRenameKey('!') = %q, want drop (0)", got)
	}
}
