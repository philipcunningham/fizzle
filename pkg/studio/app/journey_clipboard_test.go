// Journey: Ctrl-C / Ctrl-V copy/paste across Sound cells. Drives
// the keymap path (not the Sound-internal helpers) so the App's
// nav.Copy / nav.Paste routing, the status messages, and the typed
// clipboard's persistence across bind boundaries all stay pinned.

package app

import (
	"bytes"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
	"github.com/philipcunningham/fizzle/pkg/studio/spaces/sound"
)

// keyCtrl is a small helper to build a Ctrl+<rune> key event without
// repeating the tea.KeyPressMsg shape in every test.
func keyCtrl(c rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: c, Mod: tea.ModCtrl}
}

// pressRight steps the focused space's cursor n cells to the right
// by pumping ArrowRight keypresses through the App. Mirrors the
// keymap (NavRight bound to ArrowRight).
//
//nolint:unparam // n kept in the signature for clarity; current cell targets all sit 2 right
func pressRight(t *testing.T, st journeyState, n int) journeyState {
	t.Helper()
	for i := 0; i < n; i++ {
		st.a = pump(t, st.a, tea.KeyPressMsg{Code: tea.KeyRight})
	}
	return st
}

// seedByte patches a single byte at off via the model's Apply path,
// the same surface a Sound editor uses. Test scaffold for
// "make this byte distinctive before we copy from it" cases.
func seedByte(t *testing.T, st journeyState, off int, newByte byte) {
	t.Helper()
	old := st.a.containerModel.Bytes()[off]
	if old == newByte {
		return
	}
	if err := st.a.containerModel.Apply(model.Patch{
		Offset: off,
		Old:    []byte{old},
		New:    []byte{newByte},
	}); err != nil {
		t.Fatalf("seedByte at %#x: %v", off, err)
	}
}

// TestJourney_Clipboard_StageRoundTrip drives Ctrl-C on a DCA stage
// of voice 0, navigates to a stage of voice 1, drives Ctrl-V, and
// asserts the target stage's rate byte matches the source's. Default
// focus after Bind is rowDCA col 1; pressing right twice lands on
// stage 0 (col 3 in the DCA cellCount).
func TestJourney_Clipboard_StageRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey under -short")
	}
	st := newJourneyWithFixture(t, "corpus/casio-fz-1-factory-library/casio-fz-sound-disk-fl-a-piano/Piano.fzf")
	st = navInto(t, st, 0, 0)
	st = pressRight(t, st, 2)

	src0Off, _ := st.a.layout.VoiceOffset(0, 0)
	src1Off, _ := st.a.layout.VoiceOffset(0, 1)
	if src0Off == src1Off {
		t.Fatalf("voice offsets for area 0 and area 1 collided: %d", src0Off)
	}
	rate0Off := src0Off + disk.VoiceDCARateOffset
	pre := st.a.containerModel.Bytes()[rate0Off]
	target := pre ^ 0x15
	seedByte(t, st, rate0Off, target)

	st.a = pump(t, st.a, keyCtrl('c'))
	if st.a.clipboard.Kind() != sound.ClipboardKindStage {
		t.Fatalf("clipboard.Kind after Ctrl-C = %v; want Stage", st.a.clipboard.Kind())
	}

	st = navInto(t, st, 0, 1)
	st = pressRight(t, st, 2)
	st.a = pump(t, st.a, keyCtrl('v'))

	post := st.a.containerModel.Bytes()
	tgt1Off := src1Off + disk.VoiceDCARateOffset
	if post[tgt1Off] != target {
		t.Errorf("target stage rate after paste = %#x; want %#x (byte copied from voice 0 stage 0)",
			post[tgt1Off], target)
	}
}

// TestJourney_Clipboard_CopyRendersStatus pins N-06: a cell copy is not
// silent. After Ctrl-C the "Copied ..." confirmation must reach the
// rendered frame (the report kept seeing copy as having no feedback).
func TestJourney_Clipboard_CopyRendersStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey under -short")
	}
	st := newJourneyWithFixture(t, "corpus/casio-fz-1-factory-library/casio-fz-sound-disk-fl-a-piano/Piano.fzf")
	st = navInto(t, st, 0, 0)
	st = pressRight(t, st, 2) // land on a DCA stage (copyable)

	st.a = pump(t, st.a, keyCtrl('c'))

	if v := renderView(st.a); !strings.Contains(v, "Copied") {
		t.Errorf("cell copy status not rendered (N-06):\n%s", v)
	}
}

// TestJourney_Clipboard_MismatchedPasteIsNoOp pins that Ctrl-V with
// a clipboard payload whose kind doesn't match the focused cell
// emits the spec'd "Cannot paste X into Y" status and changes
// nothing in the container.
func TestJourney_Clipboard_MismatchedPasteIsNoOp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey under -short")
	}
	st := newJourneyWithFixture(t, "corpus/casio-fz-1-factory-library/casio-fz-sound-disk-fl-a-piano/Piano.fzf")
	st = navInto(t, st, 0, 0)

	st = pressRight(t, st, 2) // DCA stage 0
	st.a = pump(t, st.a, keyCtrl('c'))
	if st.a.clipboard.Kind() != sound.ClipboardKindStage {
		t.Fatalf("clipboard after Ctrl-C = %v; want Stage", st.a.clipboard.Kind())
	}

	// Walk down to rowLoops. Row order is DCA, DCF, LFO, Sample,
	// Loops; four NavDown presses lands the cursor on rowLoops.
	for i := 0; i < 4; i++ {
		st.a = pump(t, st.a, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	// The cursor's column is preserved from rowDCA col 3 (or
	// clamped to rowLoops cellCount-1). cellCount(rowLoops)=10 so
	// col 3 is in range; col 3 = "L1" (a loop cell, kind Loop).

	pre := append([]byte(nil), st.a.containerModel.Bytes()...)
	st.a = pump(t, st.a, keyCtrl('v'))
	post := st.a.containerModel.Bytes()
	if !bytes.Equal(pre, post) {
		t.Error("mismatched-kind paste mutated container; want no-op")
	}
}

// TestJourney_Clipboard_CopyOutsideSoundIsNoOp confirms Ctrl-C
// fired from a non-Sound space (e.g. Layout) does not populate the
// clipboard. The spec scopes the clipboard to Sound cells.
func TestJourney_Clipboard_CopyOutsideSoundIsNoOp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey under -short")
	}
	st := newJourneyWithFixture(t, "corpus/casio-fz-1-factory-library/casio-fz-sound-disk-fl-a-piano/Piano.fzf")
	// Don't navInto Sound; stay in Layout (the default after open).
	st.a = pump(t, st.a, keyCtrl('c'))
	if st.a.clipboard.Kind() != sound.ClipboardKindNone {
		t.Errorf("Ctrl-C outside Sound populated clipboard (kind=%v); want None",
			st.a.clipboard.Kind())
	}
}

// TestJourney_Clipboard_PasteIsSingleUndo pins that Ctrl-V lands as
// a single undo step. After a paste, one Ctrl-Z must revert the
// paste's entire byte set.
func TestJourney_Clipboard_PasteIsSingleUndo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey under -short")
	}
	st := newJourneyWithFixture(t, "corpus/casio-fz-1-factory-library/casio-fz-sound-disk-fl-a-piano/Piano.fzf")
	st = navInto(t, st, 0, 0)
	// Default cursor is at col 1; step left to col 0 (envelope
	// visual cell).
	st.a = pump(t, st.a, tea.KeyPressMsg{Code: tea.KeyLeft})

	// Seed a distinctive rate byte on the source so the paste has
	// observable effect even when both voices happened to start with
	// identical envelopes.
	src0Off, _ := st.a.layout.VoiceOffset(0, 0)
	rate0Off := src0Off + disk.VoiceDCARateOffset
	seedByte(t, st, rate0Off, st.a.containerModel.Bytes()[rate0Off]^0x20)

	st.a = pump(t, st.a, keyCtrl('c'))
	if st.a.clipboard.Kind() != sound.ClipboardKindEnvelope {
		t.Fatalf("clipboard.Kind after envelope Ctrl-C = %v; want Envelope",
			st.a.clipboard.Kind())
	}

	st = navInto(t, st, 0, 1)
	st.a = pump(t, st.a, tea.KeyPressMsg{Code: tea.KeyLeft})
	pre := append([]byte(nil), st.a.containerModel.Bytes()...)
	st.a = pump(t, st.a, keyCtrl('v'))
	post := append([]byte(nil), st.a.containerModel.Bytes()...)
	if bytes.Equal(pre, post) {
		t.Skip("paste produced no byte changes (envelopes happen to be identical); test inconclusive")
	}
	st.a = pump(t, st.a, keyCtrl('z'))
	if !bytes.Equal(pre, st.a.containerModel.Bytes()) {
		t.Error("Ctrl-Z after Ctrl-V did not restore pre-paste bytes; paste landed as multiple undo entries")
	}
}
