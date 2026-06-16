package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/philipcunningham/fizzle/pkg/studio/widgets/minimap"
)

const pianoFixture = "corpus/casio-fz-1-factory-library/casio-fz-sound-disk-fl-a-piano/Piano.fzf"

// TestApp_SoundEscCancelsEnumEditStaysInSound pins that Esc while
// editing an enum field cancels the edit and stays in Sound, rather
// than escaping to Layout. Enum fields aren't consumed by the text /
// numeric edit-mode guards, so the F-03 back-route must be gated on
// InEditMode, not assume the row list.
func TestApp_SoundEscCancelsEnumEditStaysInSound(t *testing.T) {
	st := newJourneyWithFixture(t, pianoFixture)
	st.a.current = minimap.Layout
	st.a.minimap.Current = minimap.Layout

	// Enter Sound on Bank 0 / Area 0 (two Enters: drill bank, open area).
	st.a = pump(t, st.a, keyPress(testKeyEnter), keyPress(testKeyEnter))
	if st.a.current != minimap.Sound {
		t.Fatalf("setup: expected Sound, got %v", st.a.current)
	}
	// Walk to the Loops row (4 down: DCA -> DCF -> LFO -> Sample -> Loops);
	// its cell 1 leads with the "Sustain loop" enum field.
	st.a = pump(t, st.a, keyPress("down"), keyPress("down"), keyPress("down"), keyPress("down"))
	st.a = pump(t, st.a, keyPress(testKeyEnter)) // edit the focused enum field

	if !st.a.sound.InEditMode() {
		t.Fatalf("setup: expected to be editing a field after Enter")
	}
	if st.a.sound.InTextEditMode() || st.a.sound.InNumericEditMode() {
		t.Fatalf("setup: expected an enum field edit, not text/numeric")
	}

	st.a = pump(t, st.a, keyPress(testKeyEsc))

	if st.a.current != minimap.Sound {
		t.Errorf("Esc during an enum edit exited Sound (current=%v); it should cancel the edit and stay", st.a.current)
	}
	if st.a.sound.InEditMode() {
		t.Error("Esc did not cancel the enum edit")
	}
}

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
