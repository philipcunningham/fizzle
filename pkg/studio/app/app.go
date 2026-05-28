// Package app is the integration layer for fizzle studio. It assembles
// the model, widget packages (voicelist, banktab, voicedetails,
// loopdetails, globaleffect), modal stack, status line, and key-action
// registry into a runnable tview.Application.
//
// Layout: a four-row Flex (header, upper tabbed section, lower tabbed
// section, status line). The modal stack layers save-confirm,
// quit-confirm, info, help, and terminal-too-small dialogs on top of
// the main layout. Universal shortcuts live in the application-level
// SetInputCapture and consult a KeyRegistry so the Ctrl+H help overlay
// is generated from a single source of truth.
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/philipcunningham/fizzle/pkg/audioplayer"
	"github.com/philipcunningham/fizzle/pkg/studio/helpers"
	"github.com/philipcunningham/fizzle/pkg/studio/modal"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/banktab"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/globaleffect"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/loopdetails"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/statusline"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/voicedetails"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/voicelist"
)

// MinCols and MinRows are the spec §6 minimum terminal size. Below this
// the studio shows a centred Modal asking the user to resize.
const (
	MinCols = 120
	MinRows = 30
)

// Page names for the underlying tview.Pages. The "main" page is the app
// layout; the modal Stack layers modals on top of it.
const (
	pageMain        = "main"
	pageSaveConfirm = "save-confirm"
	pageQuitConfirm = "quit-confirm"
	pageInfo        = "info"
	pageHelp        = "help"
	pageTooSmall    = "too-small"
	pageSaveError   = "save-error"
)

// Upper-tab page names. pageVoices is fixed; bank pages are named per
// index so the Pages container can switch between them by number key.
const (
	pageVoices  = "upper-voices"
	pageBankFmt = "upper-bank-%d"
)

// Lower-tab page names.
const (
	pageVoiceDetails = "lower-voice"
	pageLoopDetails  = "lower-loop"
	pageGlobalEffect = "lower-global"
)

// App is the studio application shell. Construct via newApp, drive
// via Run.
type App struct {
	tApp  *tview.Application
	pages *tview.Pages
	stack *modal.Stack

	m *model.Model

	tmpDir string

	// Layout pieces.
	header     *headerBar
	upperPages *tview.Pages
	lowerPages *tview.Pages
	upperTabs  *tview.TextView
	lowerTabs  *tview.TextView
	status     *statusline.Widget

	// Widgets.
	voiceList   *voicelist.Widget
	banks       []*banktab.Widget
	voiceDetail *voicedetails.Widget
	loopDetail  *loopdetails.Widget
	globalFX    *globaleffect.Widget

	// Tab indices. upperTab is 0 = Voices, 1..N = banks. lowerTab is
	// 0=VoiceDetails, 1=Loop, 2=GE.
	upperTab int
	lowerTab int

	// focusInUpperPane is true when keyboard focus currently lives in
	// the upper section (voice list or a bank tab), false when in the
	// lower section (voice details / loop details / global effect).
	// The Shift+Tab handler reads this to decide whether the next
	// "section" of the cycle is the upper pane or a lower-widget
	// section. Maintained by focusUpperPane / focusLowerPane.
	focusInUpperPane bool

	// Key registry seeds the help overlay.
	keys *helpers.KeyRegistry

	// Audition state.
	player       audioplayer.Player
	playCancelMu sync.Mutex
	playCancel   context.CancelFunc
	playGen      uint64
	canPlay      bool
}

// Run loads path, builds the UI, and runs the tview application loop.
// Returns when the user quits or on a fatal error.
func Run(path string) error {
	// Apply the FZ-10M LCD palette before any primitive is constructed.
	// tview.Styles is read by each widget at init time; setting it later
	// has no effect.
	applyTheme()
	tApp := tview.NewApplication()
	a, err := newApp(path, tApp)
	if err != nil {
		return err
	}
	defer a.cleanup()
	return tApp.Run()
}

// newApp constructs the App without entering the tview run loop. Exposed
// for tests; production code uses Run.
func newApp(path string, tApp *tview.Application) (*App, error) {
	m, err := model.New(path)
	if err != nil {
		return nil, fmt.Errorf("studio: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "fizzle-studio-")
	if err != nil {
		return nil, fmt.Errorf("studio: tmpdir: %w", err)
	}

	player := audioplayer.NewPlayer()
	a := &App{
		tApp:    tApp,
		m:       m,
		tmpDir:  tmpDir,
		keys:    helpers.NewKeyRegistry(),
		player:  player,
		canPlay: player != nil && player.Available(),
	}

	a.buildLayout()
	a.registerKeys()
	a.wireFollowsFocus()

	a.tApp.SetInputCapture(a.handleAppKey)
	a.tApp.SetMouseCapture(a.handleAppMouse)
	a.tApp.SetRoot(a.pages, true)
	a.tApp.SetAfterDrawFunc(a.checkTerminalSize)
	a.tApp.EnableMouse(true)

	// Initial selection / bindings.
	if a.m.Header().NVoice > 0 {
		a.voiceList.SetSelectedSlot(0)
		a.voiceDetail.Bind(0)
		a.loopDetail.Bind(0)
	}

	return a, nil
}

// buildLayout assembles the three-zone layout (header / upper / lower /
// status), constructs every widget, and wires it into a tview.Pages so
// the modal stack can overlay dialogs.
func (a *App) buildLayout() {
	// Widgets.
	a.voiceList = voicelist.New(a.m)
	a.voiceDetail = voicedetails.New(a.m)
	a.loopDetail = loopdetails.New(a.m)
	a.globalFX = globaleffect.New(a.m)

	// Inject the tview.Application so lower panels can advance focus
	// on Tab. Without this their commitOnDone / SetInputCapture
	// handlers no-op safely.
	a.voiceDetail.SetApp(a.tApp)
	a.loopDetail.SetApp(a.tApp)
	a.globalFX.SetApp(a.tApp)

	// Wire the cross-pane Shift+Tab cycle: when a lower widget's
	// CycleSection wraps past its last section, hand focus to the
	// upper pane. The next Shift+Tab from there comes back via
	// shiftTabCycle (which routes upper -> lower section 0).
	a.voiceDetail.SetOnCycleOut(a.focusUpperPane)
	a.loopDetail.SetOnCycleOut(a.focusUpperPane)
	a.globalFX.SetOnCycleOut(a.focusUpperPane)

	nBanks := a.m.Header().NBankSectors
	a.banks = make([]*banktab.Widget, nBanks)
	for i := 0; i < nBanks; i++ {
		a.banks[i] = banktab.New(a.m, i)
		// Bank tab lives in the upper pane; Shift+Tab from any of its
		// fields exits to the lower pane's section 0 via shiftTabCycle.
		// SetApp is required for the Tab field-cycling helpers.
		a.banks[i].SetApp(a.tApp)
		a.banks[i].SetOnShiftTab(a.shiftTabCycle)
	}

	// Upper pages: voicelist + one per bank.
	a.upperPages = tview.NewPages()
	a.upperPages.AddPage(pageVoices, a.voiceList.Primitive(), true, true)
	for i, bw := range a.banks {
		a.upperPages.AddPage(fmt.Sprintf(pageBankFmt, i), bw.Primitive(), true, false)
	}

	// Lower pages.
	a.lowerPages = tview.NewPages()
	a.lowerPages.AddPage(pageVoiceDetails, a.voiceDetail.Primitive(), true, true)
	a.lowerPages.AddPage(pageLoopDetails, a.loopDetail.Primitive(), true, false)
	a.lowerPages.AddPage(pageGlobalEffect, a.globalFX.Primitive(), true, false)

	// Tab rows.
	a.upperTabs = tview.NewTextView()
	a.upperTabs.SetDynamicColors(true)
	a.upperTabs.SetTextAlign(tview.AlignLeft)
	a.lowerTabs = tview.NewTextView()
	a.lowerTabs.SetDynamicColors(true)
	a.lowerTabs.SetTextAlign(tview.AlignLeft)

	a.refreshTabLabels()

	// Header + status.
	a.header = newHeaderBar(a.m)
	a.status = statusline.New()

	upperBox := tview.NewFlex().SetDirection(tview.FlexRow)
	upperBox.AddItem(a.upperTabs, 1, 0, false)
	upperBox.AddItem(a.upperPages, 0, 1, true)

	lowerBox := tview.NewFlex().SetDirection(tview.FlexRow)
	lowerBox.AddItem(a.lowerTabs, 1, 0, false)
	lowerBox.AddItem(a.lowerPages, 0, 1, false)

	root := tview.NewFlex().SetDirection(tview.FlexRow)
	root.AddItem(a.header.Primitive(), 3, 0, false)
	root.AddItem(upperBox, 0, 9, true)
	root.AddItem(lowerBox, 0, 10, false)
	root.AddItem(a.status.Primitive(), 1, 0, false)

	a.pages = tview.NewPages()
	a.pages.AddPage(pageMain, root, true, true)
	a.stack = modal.NewStack(a.pages)

	// Hook validation errors to the status line.
	a.voiceDetail.SetOnError(func(err error) {
		if err != nil {
			a.status.SetError(err.Error())
		}
	})

	// Refresh the header on every model change (mainly to update the
	// [modified] indicator and the byte counts).
	a.m.Subscribe(func() {
		a.header.Refresh()
	})
}

// cleanup releases external resources held by the app. Called by Run on
// exit. Multiple invocations are safe.
func (a *App) cleanup() {
	a.stopPlayback()
	if a.voiceList != nil {
		a.voiceList.Close()
	}
	if a.voiceDetail != nil {
		a.voiceDetail.Close()
	}
	if a.loopDetail != nil {
		a.loopDetail.Close()
	}
	if a.globalFX != nil {
		a.globalFX.Close()
	}
	for _, b := range a.banks {
		if b != nil {
			b.Close()
		}
	}
	if a.tmpDir != "" {
		_ = os.RemoveAll(a.tmpDir) //nolint:errcheck // best effort cleanup
	}
}

// wireFollowsFocus connects voicelist / banktab selection-changed
// callbacks so the lower-section panels rebind to the in-focus voice
// (spec §2.2.2 last paragraph). When the upper tab is a bank tab,
// banktab.OnAreaChanged reports the area's vp[] target slot, which is
// the voice the lower section should bind to (NOT the area index).
func (a *App) wireFollowsFocus() {
	a.voiceList.SetOnSelectionChanged(func(slot int) {
		a.voiceDetail.Bind(slot)
		a.loopDetail.Bind(slot)
	})
	for _, b := range a.banks {
		// Capture loop variable for the closure.
		b.SetOnAreaChanged(func(_, voiceSlot int) {
			a.voiceDetail.Bind(voiceSlot)
			a.loopDetail.Bind(voiceSlot)
		})
	}
}

// currentVoiceSlot returns the slot the lower-section panels are
// currently bound to. If the upper tab is Voices, that's voicelist's
// selection; otherwise it's the bank's selected area's vp[] target,
// computed directly from the bank-sector bytes since banktab does not
// expose the vp[] lookup itself.
func (a *App) currentVoiceSlot() int {
	if a.upperTab == 0 {
		return a.voiceList.SelectedSlot()
	}
	bIdx := a.upperTab - 1
	if bIdx < 0 || bIdx >= len(a.banks) {
		return 0
	}
	b := a.banks[bIdx]
	return voiceSlotForArea(a.m, b.BankIdx(), b.SelectedArea())
}

// tempFile assembles a path inside the app's tmpDir.
func (a *App) tempFile(name string) string {
	return filepath.Join(a.tmpDir, name)
}

// flushFocusedInputField fires the focused InputField's DoneFunc as if
// the user had pressed Enter, so any typed-but-uncommitted text gets
// committed before the next action takes focus away. Call before
// shortcuts that open modals or trigger global ops (Ctrl+S, Ctrl+Q);
// otherwise the user's pending edit is silently discarded.
//
// tview's InputField doesn't expose a public commit method, so we
// synthesise an Enter event through the field's InputHandler. The
// handler dispatches it to the configured SetDoneFunc, which in our
// widgets parses the text and calls the model's Apply path.
func (a *App) flushFocusedInputField() {
	if a.tApp == nil {
		return
	}
	in := a.focusedInputField()
	if in == nil {
		return
	}
	h := in.InputHandler()
	if h == nil {
		return
	}
	h(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
}

// inputFieldFocused reports whether the user is currently editing
// text. Used by the number-key and short-rune shortcut dispatchers so
// keystrokes don't get stolen from an active InputField.
func (a *App) inputFieldFocused() bool {
	return a.focusedInputField() != nil
}

// focusedInputField returns the InputField the user is currently
// editing, or nil if none. Handles both the programmatic-focus shape
// (GetFocus() returns the *tview.InputField directly, e.g. after Tab)
// and the mouse-focus shape (GetFocus() returns the InputField's
// embedded *tview.TextArea, because tview's mouse dispatch calls
// setFocus on the TextArea from inside InputField.MouseHandler).
//
// In the TextArea case it walks the studio's known InputFields and
// returns the one whose HasFocus() reports true; HasFocus() is true
// for an InputField whenever either its wrapper Box or its embedded
// TextArea has focus.
func (a *App) focusedInputField() *tview.InputField {
	if a.tApp == nil {
		return nil
	}
	return findFocusedInputField(a.tApp.GetFocus(), a.allInputFields())
}

// allInputFields collects every InputField the studio's widgets
// currently own. Used by focusedInputField to resolve the
// mouse-focus-via-TextArea case.
func (a *App) allInputFields() []*tview.InputField {
	var out []*tview.InputField
	if a.voiceDetail != nil {
		out = append(out, a.voiceDetail.InputFields()...)
	}
	if a.loopDetail != nil {
		out = append(out, a.loopDetail.InputFields()...)
	}
	if a.globalFX != nil {
		out = append(out, a.globalFX.InputFields()...)
	}
	for _, b := range a.banks {
		if b != nil {
			out = append(out, b.InputFields()...)
		}
	}
	return out
}
