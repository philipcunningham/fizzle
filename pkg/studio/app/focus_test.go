package app

import (
	"testing"

	"github.com/rivo/tview"
)

// TestFindFocusedInputField covers the two focus shapes tview produces
// (direct InputField focus and embedded-TextArea focus from mouse
// click) plus the negative cases.
func TestFindFocusedInputField(t *testing.T) {
	t.Parallel()

	t.Run("direct InputField focus returns the field", func(t *testing.T) {
		t.Parallel()
		in := tview.NewInputField()
		got := findFocusedInputField(in, nil)
		if got != in {
			t.Errorf("got %v, want %v", got, in)
		}
	})

	t.Run("TextArea focus + matching candidate returns the candidate", func(t *testing.T) {
		t.Parallel()
		// Reproduce the post-mouse-click state: a.GetFocus() would
		// return the embedded TextArea, but the parent InputField's
		// HasFocus() is true via the OR with Box.HasFocus().
		//
		// We can't reach the InputField's private embedded TextArea
		// from outside tview, so we simulate the state shape: mark a
		// bare InputField as having focus on its wrapper Box (via
		// Focus()), and pass a separately-created TextArea as the
		// "focused" primitive that GetFocus() would return.
		in := tview.NewInputField()
		in.Focus(func(tview.Primitive) {}) // sets in.HasFocus() == true
		ta := tview.NewTextArea()
		got := findFocusedInputField(ta, []*tview.InputField{in})
		if got != in {
			t.Errorf("got %v, want %v (the candidate whose HasFocus is true)", got, in)
		}
	})

	t.Run("TextArea focus with no matching candidate returns nil", func(t *testing.T) {
		t.Parallel()
		ta := tview.NewTextArea()
		other := tview.NewInputField() // never had Focus called
		got := findFocusedInputField(ta, []*tview.InputField{other})
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("non-text-entry primitive returns nil", func(t *testing.T) {
		t.Parallel()
		box := tview.NewBox()
		got := findFocusedInputField(box, []*tview.InputField{tview.NewInputField()})
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("nil focused returns nil", func(t *testing.T) {
		t.Parallel()
		got := findFocusedInputField(nil, []*tview.InputField{tview.NewInputField()})
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("nil candidate entries are skipped", func(t *testing.T) {
		t.Parallel()
		ta := tview.NewTextArea()
		in := tview.NewInputField()
		in.Focus(func(tview.Primitive) {})
		got := findFocusedInputField(ta, []*tview.InputField{nil, in, nil})
		if got != in {
			t.Errorf("got %v, want %v", got, in)
		}
	})
}
