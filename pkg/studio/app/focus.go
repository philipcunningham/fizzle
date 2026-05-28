package app

import "github.com/rivo/tview"

// findFocusedInputField returns the InputField currently holding focus,
// or nil if none. It accepts both shapes tview can produce:
//
//   - The InputField itself, as set by Tab or programmatic
//     SetFocus(inputField).
//   - The InputField's embedded TextArea, as set by tview's
//     mouse-click dispatch (the TextArea calls setFocus on itself).
//
// For the TextArea case the function walks `candidates` and returns the
// InputField whose HasFocus() reports true. tview's
// InputField.HasFocus() returns true whenever the wrapper Box OR the
// embedded TextArea has focus, so it correctly identifies the parent
// of the focused TextArea among a known set.
//
// Returns nil if focused is some other primitive type (Box, DropDown,
// etc.), or if no candidate has focus.
func findFocusedInputField(focused tview.Primitive, candidates []*tview.InputField) *tview.InputField {
	if focused == nil {
		return nil
	}
	if in, ok := focused.(*tview.InputField); ok {
		return in
	}
	if _, isTA := focused.(*tview.TextArea); !isTA {
		return nil
	}
	for _, in := range candidates {
		if in != nil && in.HasFocus() {
			return in
		}
	}
	return nil
}
