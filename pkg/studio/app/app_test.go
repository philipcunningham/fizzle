package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
)

// Voice-name fixtures reused across the table-driven tests below; named
// constants keep golangci-lint's goconst rule quiet and make the
// intent ("two-voice fixture") explicit.
const (
	voiceAlpha = "ALPHA"
	voiceBravo = "BRAVO"
)

// newCleanModel loads a fresh model without spinning up the widget tree.
// Useful for tests that want to inspect headerBar / info / help with a
// model whose dirty state is determined by the test, not by widget
// constructors that write back into the model.
func newCleanModel(path string) (*model.Model, error) {
	return model.New(path)
}

// newTestApp builds an App ready for white-box assertions. The
// underlying tview.Application is constructed but not run; tests
// inspect widget state directly rather than driving the event loop.
func newTestApp(t *testing.T, voiceNames []string) *App {
	t.Helper()
	_, p := fzfbuilder.MakeTestFZF(t, voiceNames)
	a, err := newApp(p, tview.NewApplication())
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}
	t.Cleanup(a.cleanup)
	return a
}

func TestNewAppLoadsModel(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha, voiceBravo})
	if a.m == nil {
		t.Fatal("model is nil")
	}
	if a.m.Header().NVoice != 2 {
		t.Errorf("NVoice = %d, want 2", a.m.Header().NVoice)
	}
	if a.voiceList == nil || a.voiceDetail == nil ||
		a.loopDetail == nil || a.globalFX == nil {
		t.Errorf("widgets not constructed")
	}
	if len(a.banks) != a.m.Header().NBankSectors {
		t.Errorf("len(banks) = %d, want %d", len(a.banks), a.m.Header().NBankSectors)
	}
}

func TestHeaderRendersFilename(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha})
	got := a.header.render()
	base := filepath.Base(a.m.Path())
	if !strings.Contains(got, "File: "+base) {
		t.Errorf("header missing filename; got %q", got)
	}
}

func TestHeaderModifiedReflectsDirty(t *testing.T) {
	t.Parallel()
	// Bypass the App builder so we don't drag in banktab's
	// init-time gchn write (which leaves the model dirty before any
	// real edit). We test the headerBar directly against a clean
	// model; the bare-model path is the one most users see post-Save.
	_, p := fzfbuilder.MakeTestFZF(t, []string{voiceAlpha})
	m, err := newCleanModel(p)
	if err != nil {
		t.Fatalf("newCleanModel: %v", err)
	}
	h := newHeaderBar(m)
	if strings.Contains(h.render(), "[modified]") {
		t.Errorf("header shows [modified] on clean model; got %q", h.render())
	}
	if err := m.Apply(voiceedit.Patch{Offset: 0, Size: 1, Value: 0x42}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	h.Refresh()
	if !strings.Contains(h.render(), "[modified]") {
		t.Errorf("header missing [modified] after edit; got %q", h.render())
	}
}

func TestUpperTabSwitching(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha})
	// Initial tab is 0 = Voices.
	if a.upperTab != 0 {
		t.Errorf("initial upperTab = %d, want 0", a.upperTab)
	}
	if len(a.banks) > 0 {
		a.switchUpperTab(1)
		if a.upperTab != 1 {
			t.Errorf("after switchUpperTab(1) tab = %d, want 1", a.upperTab)
		}
	}
	a.switchUpperTab(0)
	if a.upperTab != 0 {
		t.Errorf("after switchUpperTab(0) tab = %d, want 0", a.upperTab)
	}
}

func TestLowerTabSwitching(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha})
	a.switchLowerTab(1)
	if a.lowerTab != 1 {
		t.Errorf("lowerTab = %d, want 1", a.lowerTab)
	}
	a.switchLowerTab(2)
	if a.lowerTab != 2 {
		t.Errorf("lowerTab = %d, want 2", a.lowerTab)
	}
}

func TestFollowsFocusVoiceList(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha, voiceBravo})
	// Simulate the voicelist selection-changed callback. The wiring
	// must call voiceDetail.Bind and loopDetail.Bind with the new slot.
	a.voiceList.SetSelectedSlot(1) // SetSelectedSlot fires the callback.
	// We can't directly inspect voiceDetail's internal slot, but we
	// CAN observe that Bind doesn't panic and the model query succeeds.
	if v, err := a.m.Voice(1); err != nil || v == nil {
		t.Errorf("model.Voice(1) failed: %v", err)
	}
}

func TestSaveConfirmText(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha})
	got := a.saveConfirmText()
	base := filepath.Base(a.m.Path())
	if !strings.Contains(got, base) {
		t.Errorf("saveConfirmText = %q, missing filename %q", got, base)
	}
	if !strings.Contains(got, "[y/N]") {
		t.Errorf("saveConfirmText = %q, missing [y/N] prompt", got)
	}
}

func TestQuitConfirmText(t *testing.T) {
	t.Parallel()
	if !strings.Contains(quitConfirmText, "Discard") {
		t.Errorf("quitConfirmText = %q, missing 'Discard'", quitConfirmText)
	}
	if !strings.Contains(quitConfirmText, "[y/N]") {
		t.Errorf("quitConfirmText = %q, missing [y/N]", quitConfirmText)
	}
}

func TestShowSaveConfirmPushesModal(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha})
	a.showSaveConfirm()
	if a.stack.Top() != pageSaveConfirm {
		t.Errorf("Top after showSaveConfirm = %q, want %q",
			a.stack.Top(), pageSaveConfirm)
	}
}

func TestShowInfoPushesModal(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha})
	a.showInfo()
	if a.stack.Top() != pageInfo {
		t.Errorf("Top after showInfo = %q, want %q", a.stack.Top(), pageInfo)
	}
}

func TestShowHelpPushesModal(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha})
	a.showHelp()
	if a.stack.Top() != pageHelp {
		t.Errorf("Top after showHelp = %q, want %q", a.stack.Top(), pageHelp)
	}
}

func TestShowQuitConfirmPushesModal(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha})
	// We can't actually call quit() because it would Stop the app;
	// just verify the dirty branch goes through showQuitConfirm.
	a.showQuitConfirm()
	if a.stack.Top() != pageQuitConfirm {
		t.Errorf("Top after showQuitConfirm = %q, want %q",
			a.stack.Top(), pageQuitConfirm)
	}
}

func TestTerminalSizeStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		cols      int
		rows      int
		wantSmall bool
	}{
		{"too narrow", 80, 40, true},
		{"too short", 200, 24, true},
		{"both too small", 50, 10, true},
		{"exactly minimum", MinCols, MinRows, false},
		{"large", 200, 60, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := terminalSizeStatus(tc.cols, tc.rows)
			if got != tc.wantSmall {
				t.Errorf("terminalSizeStatus(%d, %d) = %v, want %v",
					tc.cols, tc.rows, got, tc.wantSmall)
			}
		})
	}
}

func TestRegisterKeysHasCoreShortcuts(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha})
	for _, name := range []string{"Ctrl+S", "Ctrl+Q", "Ctrl+Z", "Ctrl+H", "Ctrl+I", "Alt+1", "Alt+2", "Alt+3", "Shift+Tab"} {
		if _, ok := a.keys.Lookup(name); !ok {
			t.Errorf("KeyRegistry missing %q", name)
		}
	}
}

func TestRenderHelpListsKeys(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha})
	got := renderHelp(a.keys)
	for _, want := range []string{"Ctrl+S", "Ctrl+Z", "Ctrl+H", "Alt+1", "Alt+2", "Alt+3", "Space"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderHelp output missing %q\n%s", want, got)
		}
	}
}

func TestCurrentVoiceSlotFromVoiceList(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha, voiceBravo})
	a.upperTab = 0
	a.voiceList.SetSelectedSlot(1)
	if got := a.currentVoiceSlot(); got != 1 {
		t.Errorf("currentVoiceSlot on voicelist tab = %d, want 1", got)
	}
}

func TestVoiceSlotForArea(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha, voiceBravo})
	// Bank 0, area 0: should resolve to whatever vp[0] points at.
	// The test fixture maps slot 0 -> area 0 for bank 0.
	got := voiceSlotForArea(a.m, 0, 0)
	if got < 0 || got >= a.m.Header().NVoice {
		t.Errorf("voiceSlotForArea(0,0) = %d, out of range [0,%d)", got, a.m.Header().NVoice)
	}
}

func TestStatusLineErrorOnVoiceDetailError(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha})
	// VoiceDetail.SetOnError is wired in buildLayout; bind on a slot
	// that doesn't exist to trigger an error.
	a.voiceDetail.Bind(99)
	if !strings.Contains(a.status.Text(), "[red]") {
		t.Errorf("status text should be red after voicedetails error; got %q", a.status.Text())
	}
}

func TestUndoRedoPathExists(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha})
	// Save so the existing init-time gchn writes become the baseline
	// (avoids the banktab refresh-rewrites-on-undo loop).
	if err := a.m.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	patches, err := voiceedit.BuildNamePatch("EDITED")
	if err != nil {
		t.Fatalf("BuildNamePatch: %v", err)
	}
	if err := a.m.ApplyVoicePatch(0, patches[0]); err != nil {
		t.Fatalf("ApplyVoicePatch: %v", err)
	}
	if !a.m.CanUndo() {
		t.Errorf("CanUndo false after Apply")
	}
}

func TestInfoBodyMentionsCounts(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha, voiceBravo})
	body := a.infoBody()
	if !strings.Contains(body, "Voices: 2") {
		t.Errorf("info body missing voice count; got %q", body)
	}
	if !strings.Contains(body, "Banks:") {
		t.Errorf("info body missing bank count; got %q", body)
	}
}

// TestFlushFocusedInputFieldMouseFocus reproduces the manual-QA bug:
// after a mouse click, GetFocus() returns the InputField's embedded
// TextArea rather than the InputField itself, so the old
// flushFocusedInputField type-asserted to nil and silently dropped
// any pending edit on Ctrl+S. flushFocusedInputField must now locate
// the parent InputField via HasFocus() and fire its DoneFunc anyway.
func TestFlushFocusedInputFieldMouseFocus(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha, voiceBravo})

	// Pick any real studio InputField; override its DoneFunc to
	// record whether flush reached it. (We don't need the actual
	// model write; what we're proving is the dispatch path.)
	fields := a.voiceDetail.InputFields()
	if len(fields) == 0 {
		t.Fatal("voiceDetail has no InputFields")
	}
	target := fields[0]
	var fired tcell.Key
	target.SetDoneFunc(func(key tcell.Key) { fired = key })

	target.SetText("42")
	target.Focus(func(tview.Primitive) {}) // makes target.HasFocus() true
	a.tApp.SetFocus(tview.NewTextArea())   // mimics post-click GetFocus()

	a.flushFocusedInputField()

	if fired != tcell.KeyEnter {
		t.Errorf("DoneFunc not fired with Enter; pending edit was dropped (got %v)", fired)
	}
}

// TestInputFieldFocused covers both focus shapes produced by tview:
// programmatic SetFocus(inputField) leaves the *tview.InputField as the
// focused primitive, but a mouse click dispatches through the
// InputField's embedded TextArea and calls setFocus on it. The helper
// must treat both as "user is editing text" so number-key and
// short-rune shortcuts don't steal keystrokes from the field.
func TestInputFieldFocused(t *testing.T) {
	t.Parallel()
	a := newTestApp(t, []string{voiceAlpha, voiceBravo})

	a.tApp.SetFocus(nil)
	if a.inputFieldFocused() {
		t.Error("no focus set: expected false")
	}

	a.tApp.SetFocus(tview.NewInputField())
	if !a.inputFieldFocused() {
		t.Error("InputField focused (Tab/programmatic path): expected true")
	}

	// Mouse-focus state: GetFocus() returns a TextArea, but one of the
	// studio's known InputFields has HasFocus() == true (which is what
	// happens when tview's mouse dispatch lands on an InputField).
	studioField := a.voiceDetail.InputFields()[0]
	studioField.Focus(func(tview.Primitive) {})
	a.tApp.SetFocus(tview.NewTextArea())
	if !a.inputFieldFocused() {
		t.Error("TextArea focused + studio InputField has HasFocus: expected true")
	}

	a.tApp.SetFocus(tview.NewBox())
	if a.inputFieldFocused() {
		t.Error("Box focused (non-text primitive): expected false")
	}
}
