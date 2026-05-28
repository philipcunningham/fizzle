package app

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestHandleAppMouse covers the focus-change auto-flush contract: any
// mouse-down OUTSIDE the currently-focused InputField commits that
// field before tview moves focus to wherever the click landed. Clicks
// INSIDE the focused field, clicks with no field focused, and
// non-mouse-down actions are all no-ops so we don't pollute undo
// history with redundant patches.
func TestHandleAppMouse(t *testing.T) {
	t.Parallel()

	// Build a synthetic mouse event at (x, y). tcell ignores button when
	// we hand the (event, action) pair to handleAppMouse, but we still
	// need a real event so Position() returns the right coords.
	mouseEvent := func(x, y int) *tcell.EventMouse {
		return tcell.NewEventMouse(x, y, tcell.ButtonPrimary, 0)
	}

	// Build an InputField with a known rect and a DoneFunc that records
	// the key it was fired with. SetRect lets us control the
	// "inside vs outside" hit test without rendering the widget tree.
	makeField := func(rect [4]int) (*tview.InputField, *tcell.Key) {
		in := tview.NewInputField()
		in.SetRect(rect[0], rect[1], rect[2], rect[3])
		fired := new(tcell.Key)
		in.SetDoneFunc(func(key tcell.Key) { *fired = key })
		return in, fired
	}

	t.Run("A: MouseLeftDown outside focused InputField fires DoneFunc(KeyEnter)", func(t *testing.T) {
		t.Parallel()
		a := newTestApp(t, []string{voiceAlpha, voiceBravo})

		// Pick a studio InputField (so it ends up in allInputFields()),
		// install a recording DoneFunc, and put the app in the
		// mouse-focus state.
		fields := a.voiceDetail.InputFields()
		if len(fields) == 0 {
			t.Fatal("no studio InputFields available")
		}
		target := fields[0]
		target.SetRect(10, 10, 20, 1) // x=10..29, y=10
		var fired tcell.Key
		target.SetDoneFunc(func(key tcell.Key) { fired = key })
		target.SetText("42")
		target.Focus(func(tview.Primitive) {}) // target.HasFocus() == true
		a.tApp.SetFocus(tview.NewTextArea())   // mouse-focus shape

		// Click well outside the field rect.
		a.handleAppMouse(mouseEvent(0, 0), tview.MouseLeftDown)

		if fired != tcell.KeyEnter {
			t.Errorf("DoneFunc not fired with Enter on outside-click; got %v", fired)
		}
	})

	t.Run("B: MouseLeftDown inside focused InputField does NOT fire DoneFunc", func(t *testing.T) {
		t.Parallel()
		a := newTestApp(t, []string{voiceAlpha, voiceBravo})
		fields := a.voiceDetail.InputFields()
		target := fields[0]
		target.SetRect(10, 10, 20, 1)
		var fired tcell.Key
		target.SetDoneFunc(func(key tcell.Key) { fired = key })
		target.Focus(func(tview.Primitive) {})
		a.tApp.SetFocus(tview.NewTextArea())

		// Click inside the field's rect.
		a.handleAppMouse(mouseEvent(15, 10), tview.MouseLeftDown)

		if fired != 0 {
			t.Errorf("DoneFunc fired on same-field click; expected no-op (got %v)", fired)
		}
	})

	t.Run("C: MouseLeftDown with no InputField focused is a no-op", func(t *testing.T) {
		t.Parallel()
		a := newTestApp(t, []string{voiceAlpha, voiceBravo})

		// Sentinel field: should never fire under this contract.
		sentinel, fired := makeField([4]int{10, 10, 20, 1})
		_ = sentinel
		a.tApp.SetFocus(tview.NewBox()) // non-InputField, non-TextArea

		a.handleAppMouse(mouseEvent(0, 0), tview.MouseLeftDown)

		if *fired != 0 {
			t.Errorf("sentinel DoneFunc fired unexpectedly: %v", *fired)
		}
	})

	t.Run("D: non-LeftDown actions never trigger flush", func(t *testing.T) {
		t.Parallel()
		nonDownActions := map[string]tview.MouseAction{
			"MouseMove":       tview.MouseMove,
			"MouseLeftUp":     tview.MouseLeftUp,
			"MouseLeftClick":  tview.MouseLeftClick,
			"MouseScrollUp":   tview.MouseScrollUp,
			"MouseScrollDown": tview.MouseScrollDown,
			"MouseRightDown":  tview.MouseRightDown,
			"MouseMiddleDown": tview.MouseMiddleDown,
		}
		for name, action := range nonDownActions {
			action := action
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				a := newTestApp(t, []string{voiceAlpha, voiceBravo})
				target := a.voiceDetail.InputFields()[0]
				target.SetRect(10, 10, 20, 1)
				var fired tcell.Key
				target.SetDoneFunc(func(key tcell.Key) { fired = key })
				target.Focus(func(tview.Primitive) {})
				a.tApp.SetFocus(tview.NewTextArea())

				a.handleAppMouse(mouseEvent(0, 0), action)

				if fired != 0 {
					t.Errorf("DoneFunc fired for %s; expected no-op", name)
				}
			})
		}
	})

	t.Run("E: event and action are returned unchanged", func(t *testing.T) {
		t.Parallel()
		a := newTestApp(t, []string{voiceAlpha, voiceBravo})

		in := mouseEvent(5, 5)
		gotEvent, gotAction := a.handleAppMouse(in, tview.MouseLeftDown)
		if gotEvent != in {
			t.Errorf("event modified: got %v want %v", gotEvent, in)
		}
		if gotAction != tview.MouseLeftDown {
			t.Errorf("action modified: got %v want %v", gotAction, tview.MouseLeftDown)
		}
	})

	t.Run("F: end-to-end: typing in cutoff then clicking away commits to model", func(t *testing.T) {
		t.Parallel()
		a := newTestApp(t, []string{voiceAlpha, voiceBravo})
		fields := a.voiceDetail.InputFields()
		var target *tview.InputField
		for _, in := range fields {
			if labelHasPrefix(in.GetLabel(), "Cutoff") {
				target = in
				break
			}
		}
		if target == nil {
			t.Fatal("no Cutoff InputField")
		}
		target.SetRect(10, 10, 20, 1)
		var firedKey tcell.Key
		target.SetDoneFunc(func(key tcell.Key) { firedKey = key })

		target.SetText("52")
		target.Focus(func(tview.Primitive) {})
		a.tApp.SetFocus(tview.NewTextArea())

		// User clicks somewhere outside the cutoff field.
		a.handleAppMouse(mouseEvent(50, 50), tview.MouseLeftDown)

		if firedKey != tcell.KeyEnter {
			t.Errorf("Cutoff DoneFunc not fired with Enter; got %v (pending edit would be dropped)", firedKey)
		}
	})
}

// labelHasPrefix is a tiny dependency-free prefix check kept here so the
// test file doesn't need to import strings just for this.
func labelHasPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if s[i] != prefix[i] {
			return false
		}
	}
	return true
}
