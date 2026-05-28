package helpers

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

const (
	descSave = "Save"
	nameSave = "save"
)

func TestKeyRegistryRegisterLookup(t *testing.T) {
	t.Parallel()
	r := NewKeyRegistry()
	a := KeyAction{Name: nameSave, Key: tcell.KeyCtrlS, Description: descSave}
	r.Register(a)
	got, ok := r.Lookup(nameSave)
	if !ok {
		t.Fatalf("Lookup(save): not found")
	}
	if got.Key != tcell.KeyCtrlS {
		t.Errorf("Lookup(save).Key = %v, want CtrlS", got.Key)
	}
}

func TestKeyRegistryAllOrdered(t *testing.T) {
	t.Parallel()
	r := NewKeyRegistry()
	r.Register(KeyAction{Name: nameSave, Key: tcell.KeyCtrlS, Description: descSave})
	r.Register(KeyAction{Name: "quit", Key: tcell.KeyCtrlQ, Description: "Quit"})
	r.Register(KeyAction{Name: "undo", Key: tcell.KeyCtrlZ, Description: "Undo"})

	all := r.All()
	if len(all) != 3 {
		t.Fatalf("All() len = %d, want 3", len(all))
	}
	wantOrder := []string{nameSave, "quit", "undo"}
	for i, w := range wantOrder {
		if all[i].Name != w {
			t.Errorf("All()[%d].Name = %q, want %q", i, all[i].Name, w)
		}
	}
}

func TestKeyRegistryRegisterReplaces(t *testing.T) {
	t.Parallel()
	r := NewKeyRegistry()
	r.Register(KeyAction{Name: nameSave, Key: tcell.KeyCtrlS, Description: descSave})
	// Re-registering the same name should overwrite (e.g. user-customised
	// binding) without duplicating the entry.
	r.Register(KeyAction{Name: nameSave, Key: tcell.KeyF2, Description: "Save (F2)"})
	if got := len(r.All()); got != 1 {
		t.Errorf("All() len = %d, want 1 after replace", got)
	}
	got, _ := r.Lookup(nameSave)
	if got.Key != tcell.KeyF2 {
		t.Errorf("Lookup(save).Key = %v, want F2", got.Key)
	}
}
