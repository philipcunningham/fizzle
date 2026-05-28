package app

import (
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

// switchUpperTab routes to Voices (0) or bank N (1..NBankSectors). Out of
// range tab indices are clamped silently; the universal key handler
// already filters to legal numerals.
func (a *App) switchUpperTab(idx int) {
	maxTab := len(a.banks) // 0 = voices, 1..N = banks
	if idx < 0 {
		idx = 0
	}
	if idx > maxTab {
		idx = maxTab
	}
	a.upperTab = idx
	if idx == 0 {
		a.upperPages.SwitchToPage(pageVoices)
		a.rebindFromVoices()
	} else {
		a.upperPages.SwitchToPage(fmt.Sprintf(pageBankFmt, idx-1))
		a.rebindFromBank(idx - 1)
	}
	a.refreshTabLabels()
	a.focusUpperPane()
}

// focusUpperPane moves keyboard focus into the currently-active upper
// tab's widget. Calls the widget's Focus method (which targets a
// known-focusable inner primitive) rather than SetFocus(Primitive()),
// because a Flex with no focus-delegated child swallows the focus
// invisibly. Also marks the focus pane so the Shift+Tab cross-pane
// cycle knows which direction the next press should advance.
func (a *App) focusUpperPane() {
	a.focusInUpperPane = true
	if a.upperTab == 0 {
		a.voiceList.Focus(a.tApp)
		return
	}
	if a.upperTab-1 < len(a.banks) {
		a.banks[a.upperTab-1].Focus(a.tApp)
	}
}

// switchLowerTab routes the lower section to one of the three detail
// panels.
func (a *App) switchLowerTab(idx int) {
	switch idx {
	case 0:
		a.lowerPages.SwitchToPage(pageVoiceDetails)
	case 1:
		a.lowerPages.SwitchToPage(pageLoopDetails)
	case 2:
		a.lowerPages.SwitchToPage(pageGlobalEffect)
	default:
		return
	}
	a.lowerTab = idx
	a.refreshTabLabels()
	a.focusLowerPane()
}

// focusLowerPane moves keyboard focus into the currently-active lower
// tab's widget. Each widget exposes a Focus method that targets its
// preferred internal primitive (voice details -> DCA stage table; loop
// details -> stage table; global effect -> Bend Range field). Marks
// the focus pane for the Shift+Tab cross-pane cycle.
func (a *App) focusLowerPane() {
	a.focusInUpperPane = false
	switch a.lowerTab {
	case 1:
		a.loopDetail.Focus(a.tApp)
	case 2:
		a.globalFX.Focus(a.tApp)
	default:
		a.voiceDetail.Focus(a.tApp)
	}
}

// shiftTabCycle advances the cross-pane Shift+Tab cycle one step.
// The cycle treats the upper pane as one zone and each lower-widget
// section as its own zone, in the order:
//
//	upper pane -> lower section 0 -> lower section 1 -> ... -> upper pane
//
// When focus is in the upper pane, the next step focuses lower
// section 0. When focus is in the lower pane, we ask the active
// lower widget to advance its section; if it would wrap past the
// last section, the widget's onCycleOut callback (wired in
// buildLayout to focusUpperPane) hops focus back to the upper pane.
func (a *App) shiftTabCycle() {
	if a.focusInUpperPane {
		a.focusLowerPane()
		return
	}
	switch a.lowerTab {
	case 1:
		a.loopDetail.CycleSection(a.tApp)
	case 2:
		a.globalFX.CycleSection(a.tApp)
	default:
		a.voiceDetail.CycleSection(a.tApp)
	}
}

// rebindFromVoices reapplies the voicelist's current selection to the
// lower-section detail panels. Called when the user switches back to
// the Voices tab from a bank tab.
func (a *App) rebindFromVoices() {
	slot := a.voiceList.SelectedSlot()
	a.voiceDetail.Bind(slot)
	a.loopDetail.Bind(slot)
}

// rebindFromBank reapplies the active bank's selected area's vp[]
// target to the lower-section detail panels.
func (a *App) rebindFromBank(bankIdx int) {
	if bankIdx < 0 || bankIdx >= len(a.banks) {
		return
	}
	b := a.banks[bankIdx]
	slot := voiceSlotForArea(a.m, b.BankIdx(), b.SelectedArea())
	a.voiceDetail.Bind(slot)
	a.loopDetail.Bind(slot)
}

// refreshTabLabels renders the upper- and lower-tab rows. The active tab
// is highlighted in inverse video; the other labels are dim. Bank tab
// labels include the bank name (truncated; the rename InputField inside
// the bank tab itself owns full-width editing).
func (a *App) refreshTabLabels() {
	a.upperTabs.SetText(renderUpperTabs(a.m, a.upperTab))
	a.lowerTabs.SetText(renderLowerTabs(a.lowerTab))
}

// renderUpperTabs builds the tab-row text. Voices is tab 0; banks
// start at tab 1.
func renderUpperTabs(m *model.Model, active int) string {
	out := tabLabel("1 Voices", active == 0)
	for i := 0; i < m.Header().NBankSectors; i++ {
		name := m.BankName(i)
		if name == "" {
			name = "(unnamed)"
		}
		label := fmt.Sprintf("%d %s", i+2, name)
		out += "  " + tabLabel(label, active == i+1)
	}
	return out
}

// renderLowerTabs builds the lower tab-row text.
func renderLowerTabs(active int) string {
	parts := []string{
		tabLabel("Alt+1 Voice Details", active == 0),
		tabLabel("Alt+2 Loop Details", active == 1),
		tabLabel("Alt+3 Global Effect", active == 2),
	}
	return parts[0] + "  " + parts[1] + "  " + parts[2]
}

// tabLabel formats one tab entry, highlighting it if active.
// Inactive tabs use silver (not gray) so they stay readable on the
// black background; active tabs invert to black-on-darkcyan to match
// the theme's accent rather than the previous black-on-white.
func tabLabel(text string, active bool) string {
	if active {
		return "[black:darkcyan]" + text + "[-:-]"
	}
	return "[silver]" + text + "[-]"
}

// voiceSlotForArea is a stand-alone helper that mirrors banktab's
// private voiceSlotForArea so the app shell can derive the same
// vp[]-lookup value the bank tab uses, without modifying banktab's API.
func voiceSlotForArea(m *model.Model, bankIdx, area int) int {
	bank := fzutil.BankSliceAt(m.Bytes(), bankIdx)
	if bank == nil {
		return 0
	}
	off := disk.BankVoiceNumOffset + 2*area
	if off+2 > len(bank) {
		return 0
	}
	return int(bank[off])
}
