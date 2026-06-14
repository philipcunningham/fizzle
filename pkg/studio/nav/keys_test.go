package nav

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestFromKey_TableDrivenBindings is the keymap regression net.
// Every key string the App expects to route to a specific Action
// gets a row here; if someone re-binds or renames an Action by
// accident the table catches it.
//
// The table also pins the multi-binding cases (Rename has F2 AND
// 'r'; Extract has Ctrl+E AND 'c') and the Emacs aliases
// (Ctrl+P -> NavUp, etc.).
func TestFromKey_TableDrivenBindings(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyMsg
		want Action
	}{
		// Cursor navigation: plain arrows.
		{"NavUp/up", tea.KeyPressMsg{Code: tea.KeyUp}, NavUp},
		{"NavDown/down", tea.KeyPressMsg{Code: tea.KeyDown}, NavDown},
		{"NavLeft/left", tea.KeyPressMsg{Code: tea.KeyLeft}, NavLeft},
		{"NavRight/right", tea.KeyPressMsg{Code: tea.KeyRight}, NavRight},

		// Inter-space navigation: shift+up/down.
		{"SpaceUp/shift+up", tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift}, SpaceUp},
		{"SpaceDown/shift+down", tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}, SpaceDown},

		// Emacs aliases.
		{"NavUp/ctrl+p", tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}, NavUp},
		{"NavDown/ctrl+n", tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}, NavDown},
		{"NavLeft/alt+b", tea.KeyPressMsg{Code: 'b', Mod: tea.ModAlt}, NavLeft},
		{"NavRight/alt+f", tea.KeyPressMsg{Code: 'f', Mod: tea.ModAlt}, NavRight},

		// Universal.
		{"OpenHelp/?", tea.KeyPressMsg{Code: '?', Text: "?"}, OpenHelp},
		{"Audition/space", tea.KeyPressMsg{Code: ' ', Text: " "}, Audition},
		{"Save/ctrl+s", tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}, Save},
		{"Quit/ctrl+q", tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl}, Quit},
		{"Undo/ctrl+z", tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl}, Undo},
		{"Redo/ctrl+y", tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}, Redo},
		{"Copy/ctrl+c", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, Copy},
		{"Paste/ctrl+v", tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl}, Paste},
		{"Rename/F2", tea.KeyPressMsg{Code: tea.KeyF2}, Rename},
		{"Rename/r", tea.KeyPressMsg{Code: 'r', Text: "r"}, Rename},
		{"Confirm/enter", tea.KeyPressMsg{Code: tea.KeyEnter}, Confirm},
		{"Cancel/esc", tea.KeyPressMsg{Code: tea.KeyEsc}, Cancel},
		{"Delete/delete", tea.KeyPressMsg{Code: tea.KeyDelete}, Delete},
		{"Delete/backspace", tea.KeyPressMsg{Code: tea.KeyBackspace}, Delete},
		{"Duplicate/ctrl+d", tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}, Duplicate},
		{"Extract/ctrl+e", tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}, Extract},
		{"Extract/c", tea.KeyPressMsg{Code: 'c', Text: "c"}, Extract},
		{"Import/i", tea.KeyPressMsg{Code: 'i', Text: "i"}, Import},
		{"NewDisk/n", tea.KeyPressMsg{Code: 'n', Text: "n"}, NewDisk},
		{"Export/e", tea.KeyPressMsg{Code: 'e', Text: "e"}, Export},
		{"Refresh/ctrl+r", tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}, Refresh},
		{"Move/m", tea.KeyPressMsg{Code: 'm', Text: "m"}, Move},
		{"EditArea/a", tea.KeyPressMsg{Code: 'a', Text: "a"}, EditArea},
		{"EditEffects/f", tea.KeyPressMsg{Code: 'f', Text: "f"}, EditEffects},

		// Unbound keys fall through to NavNone.
		{"NavNone/x", tea.KeyPressMsg{Code: 'x', Text: "x"}, NavNone},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := FromKey(tc.msg)
			if got != tc.want {
				t.Errorf("FromKey(%+v) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}
