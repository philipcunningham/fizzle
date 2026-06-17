package nav

import (
	tea "charm.land/bubbletea/v2"
)

// FromKey translates a Bubble Tea key event into a navigation Action.
// Returns NavNone when the key is not bound. Callers fall back to
// passing the raw key through (for text-entry fields).
func FromKey(msg tea.KeyMsg) Action {
	switch msg.String() {

	// Intra-space cursor navigation: plain arrows.
	case "up":
		return NavUp
	case "down":
		return NavDown
	case "left":
		return NavLeft
	case "right":
		return NavRight

	// Inter-space navigation: SHIFT+up/down moves between spaces.
	// SHIFT+left/right is reserved; the App ignores it for now.
	case "shift+up":
		return SpaceUp
	case "shift+down":
		return SpaceDown

	// Fast list navigation: Home/End to ends, PgUp/PgDn by a page.
	case "home":
		return NavTop
	case "end":
		return NavBottom
	case "pgup":
		return NavPageUp
	case "pgdown":
		return NavPageDown

	// Cursor navigation: Emacs.
	case "ctrl+p":
		return NavUp
	case "ctrl+n":
		return NavDown
	case "alt+b":
		return NavLeft
	case "alt+f":
		return NavRight

	// Universal.
	case "?":
		return OpenHelp
	case " ", "space":
		return Audition
	case "ctrl+s":
		return Save
	case "ctrl+q":
		// Quit is Ctrl-Q only: a bare `q` is too easy to hit by
		// accident. Back navigation has its own keys (Esc, Left,
		// SHIFT+up).
		return Quit
	case "ctrl+z":
		return Undo
	case "ctrl+y":
		return Redo
	case "ctrl+c", "y":
		// 'y' (yank) is the tmux-safe alias: terminals/tmux swallow
		// Ctrl-C as SIGINT, so cell copy needs a plain-key option.
		return Copy
	case "ctrl+v", "p":
		return Paste
	case "f2", "r":
		return Rename
	case "l":
		return RenameDisk
	case "enter":
		return Confirm
	case "esc":
		return Cancel
	case "delete", "backspace":
		return Delete
	case "ctrl+d":
		return Duplicate
	case "ctrl+e", "c":
		return Extract
	case "i":
		return Import
	case "n":
		return NewDisk
	case "e":
		return Export
	case "ctrl+r":
		return Refresh
	case "m":
		return Move
	case "a":
		return EditArea
	case "f":
		return EditEffects
	}
	return NavNone
}
