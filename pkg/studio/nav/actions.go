// Package nav defines the studio navigation action set and the key
// map that produces actions from key events. Every space dispatches
// off the same Action enum so navigation is uniform.
package nav

// Action is the studio-wide enum of user navigation intents. Spaces
// and the App receive Action values rather than raw keys; this keeps
// key map customisation isolated to the keys.go file.
type Action int

// Action enum values. NavNone is the zero value used when a key event
// produces no navigation intent. NavUp/NavDown/NavLeft/NavRight are
// intra-space cursor moves dispatched to the focused space's Apply.
// SpaceUp/SpaceDown are inter-space moves the App handles directly.
// The remainder are universal actions.
const (
	// NavNone is the zero value indicating no navigation intent.
	NavNone Action = iota

	// NavUp moves the cursor up within the focused space.
	NavUp
	// NavDown moves the cursor down within the focused space.
	NavDown
	// NavLeft moves the cursor left within the focused space.
	NavLeft
	// NavRight moves the cursor right within the focused space.
	NavRight

	// NavTop / NavBottom jump the cursor to the first / last item in
	// the focused list (Home / End). NavPageUp / NavPageDown move the
	// cursor by one page (PgUp / PgDn). Fast navigation for long lists
	// (a bank holds up to 64 areas).
	NavTop
	NavBottom
	NavPageUp
	NavPageDown

	// SpaceUp moves focus to the previous top-level space
	// (Workspace -> Pool -> Layout -> Sound, in reverse).
	SpaceUp
	// SpaceDown moves focus to the next top-level space
	// (Workspace -> Pool -> Layout -> Sound).
	SpaceDown

	// OpenHelp opens the studio help overlay.
	OpenHelp
	Audition
	Save
	Quit
	Undo
	Redo
	Copy
	Paste
	Rename
	// RenameDisk opens the disk-label editor (the FZ volume name the
	// hardware shows). Distinct from Rename, which targets a bank or
	// voice. Bound to `l` (label).
	RenameDisk
	Confirm
	Cancel
	Delete
	Duplicate
	Extract
	// Import opens the pool-picker for the focused target (e.g. an
	// Area in Layout). Bound to `i` in the default key map.
	Import
	// NewDisk discards the in-flight container (with a dirty prompt)
	// and starts with a fresh untitled FZF, dropping the user into
	// Layout on bank A. Bound to `n`.
	NewDisk
	// Export writes the focused element to a file in the workspace
	// directory. In Pool, that's the focused entry's FZV bytes,
	// landing at <workspace>/<name>.fzv. Bound to `e`.
	Export
	// Refresh re-reads the focused space's external view of the
	// world. In Workspace, that's a rescan of the directory listing.
	// Bound to `ctrl+r`.
	Refresh
	// Move marks the focused Area as a swap source, or completes a
	// pending swap when one is already marked. Two `m` presses in a
	// row exchange the source and target Areas. Bound to `m`.
	Move
	// EditArea opens the spatial Area editor modal for the focused
	// Area: piano visualisation of key range with live preview as
	// the user adjusts low/high. Bound to `a` on the area list.
	EditArea
	// EditEffects opens the per-bank effects editor modal (bend +
	// 3x7 controller modulation matrix from the bank sector's
	// effectdata block at offset 0x3c0). Bound to `f`.
	EditEffects
)
