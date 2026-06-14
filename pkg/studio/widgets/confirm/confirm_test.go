package confirm

import "testing"

func TestShowReturnsConfirmedResult(t *testing.T) {
	m := New()
	ch := m.Show(Prompt{
		Title: "Save?",
		Body:  "Discard or save your edits.",
		Options: []Option{
			{Label: "Save", Result: 1},
			{Label: "Discard", Result: 2},
			{Label: "Cancel", Result: 0},
		},
	})
	if !m.IsOpen() {
		t.Fatalf("IsOpen = false after Show, want true")
	}
	m.Next()    // focus = Discard (2)
	m.Confirm() // chosen = 2
	got := <-ch
	if got != 2 {
		t.Errorf("Confirm result = %d, want 2", got)
	}
	if m.IsOpen() {
		t.Errorf("IsOpen = true after Confirm, want false")
	}
}

func TestCancelReturnsCancelOption(t *testing.T) {
	m := New()
	ch := m.Show(Prompt{
		Title: "Delete?",
		Body:  "Removes the Area.",
		Options: []Option{
			{Label: "Yes", Result: 1},
			{Label: "Cancel", Result: 0},
		},
	})
	m.Cancel()
	if got := <-ch; got != 0 {
		t.Errorf("Cancel result = %d, want 0 (Cancel option)", got)
	}
}

func TestCancelReturnsMinusOneWithoutCancelOption(t *testing.T) {
	m := New()
	ch := m.Show(Prompt{
		Title:   "Delete?",
		Options: []Option{{Label: "Yes", Result: 1}, {Label: "No", Result: 0}},
	})
	m.Cancel()
	if got := <-ch; got != -1 {
		t.Errorf("Cancel without Cancel option = %d, want -1", got)
	}
}

func TestNextPrevWrap(t *testing.T) {
	m := New()
	_ = m.Show(Prompt{Options: []Option{
		{Label: "A", Result: 0}, {Label: "B", Result: 1}, {Label: "C", Result: 2},
	}})
	if m.focus != 0 {
		t.Fatalf("initial focus = %d, want 0", m.focus)
	}
	m.Next()
	m.Next()
	m.Next() // wrap to 0
	if m.focus != 0 {
		t.Errorf("after 3 Next, focus = %d, want 0", m.focus)
	}
	m.Prev() // wrap to 2
	if m.focus != 2 {
		t.Errorf("after Prev from 0, focus = %d, want 2", m.focus)
	}
}
