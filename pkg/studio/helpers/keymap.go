package helpers

import "github.com/gdamore/tcell/v2"

// KeyAction describes one studio keyboard binding. The widget wave wires
// these into Application.SetInputCapture for app-level dispatch and an
// auto-generated help overlay (Ctrl+H); having the description on the same
// struct as the binding means the two cannot drift.
//
// tcell.Key is an underlying terminal-library type, not a tview primitive,
// so this lives in the foundation layer (k9s and irccloud both reference
// tcell types in non-widget code).
type KeyAction struct {
	Name        string
	Key         tcell.Key
	Description string
}

// KeyRegistry is an ordered, name-keyed collection of KeyActions. Insertion
// order is preserved so the help overlay renders bindings in the order the
// app shell registered them rather than in map-iteration order. Registering
// the same name twice replaces the existing entry in place; useful when
// the user customises a binding.
type KeyRegistry struct {
	order   []string
	entries map[string]KeyAction
}

// NewKeyRegistry returns an empty KeyRegistry.
func NewKeyRegistry() *KeyRegistry {
	return &KeyRegistry{
		entries: map[string]KeyAction{},
	}
}

// Register adds (or replaces) the action under a.Name. Replacement keeps the
// original insertion position.
func (r *KeyRegistry) Register(a KeyAction) {
	if _, exists := r.entries[a.Name]; !exists {
		r.order = append(r.order, a.Name)
	}
	r.entries[a.Name] = a
}

// Lookup returns the action with the given name and whether it was found.
func (r *KeyRegistry) Lookup(name string) (KeyAction, bool) {
	a, ok := r.entries[name]
	return a, ok
}

// All returns the registered actions in insertion order.
func (r *KeyRegistry) All() []KeyAction {
	out := make([]KeyAction, 0, len(r.order))
	for _, n := range r.order {
		out = append(out, r.entries[n])
	}
	return out
}
