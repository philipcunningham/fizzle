package nav

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestFromKey_FastListNav pins F-QA-23: Home/End and PgUp/PgDn map to the
// fast list-navigation actions.
func TestFromKey_FastListNav(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyMsg
		want Action
	}{
		{"home", tea.KeyPressMsg{Code: tea.KeyHome}, NavTop},
		{"end", tea.KeyPressMsg{Code: tea.KeyEnd}, NavBottom},
		{"pgup", tea.KeyPressMsg{Code: tea.KeyPgUp}, NavPageUp},
		{"pgdown", tea.KeyPressMsg{Code: tea.KeyPgDown}, NavPageDown},
	}
	for _, c := range cases {
		if got := FromKey(c.msg); got != c.want {
			t.Errorf("FromKey(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestFromKey_TmuxSafeCopyPaste pins F-QA-14: plain 'y'/'p' alias the
// cell copy/paste so they work under tmux (which swallows Ctrl-C).
func TestFromKey_TmuxSafeCopyPaste(t *testing.T) {
	if got := FromKey(tea.KeyPressMsg{Code: 'y'}); got != Copy {
		t.Errorf("FromKey('y') = %v, want Copy", got)
	}
	if got := FromKey(tea.KeyPressMsg{Code: 'p'}); got != Paste {
		t.Errorf("FromKey('p') = %v, want Paste", got)
	}
}
