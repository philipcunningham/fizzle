package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/philipcunningham/fizzle/pkg/studio/widgets/minimap"
)

const pianoFixture = "corpus/casio-fz-1-factory-library/casio-fz-sound-disk-fl-a-piano/Piano.fzf"

// TestApp_SoundCancelReturnsToLayout pins F-03: entering the Sound
// space from a Layout Area and pressing Esc returns to Layout focused
// on the originating Area, instead of leaving Esc inert (the nav
// trap). The user drills Layout (Enter on a bank, Enter on an Area),
// which routes into Sound; Esc must reverse that.
func TestApp_SoundCancelReturnsToLayout(t *testing.T) {
	st := newJourneyWithFixture(t, pianoFixture)
	// Post-open state: container loaded, focus on Layout's bank list.
	st.a.current = minimap.Layout
	st.a.minimap.Current = minimap.Layout

	// Enter #1 drills into Bank 0; Enter #2 opens Area 0 into Sound.
	st.a = pump(t, st.a, keyPress(testKeyEnter), keyPress(testKeyEnter))
	if st.a.current != minimap.Sound {
		t.Fatalf("setup: expected to be in Sound after two Enters, got %v", st.a.current)
	}

	st.a = pump(t, st.a, keyPress(testKeyEsc))

	if st.a.current != minimap.Layout {
		t.Fatalf("Esc in Sound: current = %v, want Layout (F-03 nav trap)", st.a.current)
	}
	bank, area, ok := st.a.layout.SelectedArea()
	if !ok {
		t.Fatal("after Esc, Layout has no selected Area; expected to land on the originating Area")
	}
	if bank != 0 || area != 0 {
		t.Errorf("returned to Bank %d / Area %d, want Bank 0 / Area 0 (originating Area)", bank, area)
	}
}

// TestApp_PlainQKey_DoesNotQuit pins the decision that quit is Ctrl-Q
// only: a bare `q` must not emit tea.Quit, since it is far too easy to
// hit by accident.
func TestApp_PlainQKey_DoesNotQuit(t *testing.T) {
	a, _ := newTestAppEmpty(t)
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})

	_, cmd := a.Update(keyPress("q"))
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, ok := msg.(tea.QuitMsg); ok {
				t.Fatal("plain q emitted tea.Quit; quit must be Ctrl-Q only")
			}
		}
	}
}
