// Package layout is the studio Layout space. Layout edits the
// in-focus container: banks and Areas. The space has two views:
//
//   - Bank list (default): shows the 8 banks, navigable with NavUp /
//     NavDown. Pressing Confirm on a bank drills into it.
//   - Area list (after drill-in): shows the bank's Areas with their
//     per-Area summary (voice name, key range, velocity range).
//     NavLeft returns to the bank list; Confirm on an Area emits an
//     Intent the App routes to Sound.
//
// Layout does not load files; the App sets the in-focus container
// via SetContainer and clears it via ClearContainer.
package layout

import (
	"encoding/binary"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/fznote"
	"github.com/philipcunningham/fizzle/pkg/studio/loader"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
	"github.com/philipcunningham/fizzle/pkg/studio/nav"
	"github.com/philipcunningham/fizzle/pkg/studio/theme"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/hint"
)

// IntentKind tags App-level transitions Layout can request.
type IntentKind int

const (
	// IntentNone is the zero value; the App takes no transition.
	IntentNone IntentKind = iota
	// IntentOpenSound is emitted when the user Confirms an Area; the
	// App should switch to Sound bound to that Area.
	IntentOpenSound
	// IntentExtractToPool is emitted when the user presses Ctrl-E on an
	// Area; the App copies the Area's voice into the Pool.
	IntentExtractToPool
	// IntentDeleteArea is emitted when the user presses Delete on an
	// Area; the App must show a confirmation modal first, then on
	// confirm zero the Area's voice header.
	IntentDeleteArea
	// IntentStartPicker is emitted when the user presses `i` on an
	// Area. The App opens the pool-picker modal scoped to that
	// (bank, area); the picker's selection assigns into that slot.
	IntentStartPicker
	// IntentRenameVoice is emitted when the user presses `r` or F2
	// on an Area. The App opens an inline rename modal scoped to
	// the voice header at that slot.
	IntentRenameVoice
	// IntentRenameBank is emitted when the user presses `r` or F2
	// on a row in the bank list. The App opens an inline rename
	// modal scoped to the bank's name field. AreaIdx is unused in
	// this case; only BankIdx matters.
	IntentRenameBank
	// IntentExportArea is emitted when the user presses `e` on an
	// Area in the area list. The App extracts that Area's voice
	// (full FZV with audio) and writes it to the workspace
	// directory.
	IntentExportArea
	// IntentSwapAreas is emitted when the user completes an `m`
	// swap inside one bank: the first `m` press marks a source
	// Area; the second press marks the target and emits this
	// intent. SourceArea + TargetArea carry both indices.
	IntentSwapAreas
	// IntentEditArea is emitted when the user presses `a` on an
	// Area in the area list. The App opens the spatial Area editor
	// modal (piano visualisation + live-updating key range fields).
	IntentEditArea
	// IntentEditEffects is emitted when the user presses `f` on a
	// bank in the bank list. The App opens the per-bank effects
	// editor modal (bend + 3x7 controller modulation matrix).
	IntentEditEffects
	// IntentDeleteBank is emitted when the user presses Delete /
	// Backspace on a bank in the bank list. The App shows a
	// confirmation modal first, then on confirm zeroes the bank
	// sector. Subsequent save-time compaction drops the trailing
	// empties; middle-gap empties survive as empty banks.
	IntentDeleteBank
	// IntentDuplicateArea is emitted when the user presses Ctrl-D on
	// an Area in the area list. The App clones the source Area's
	// voice header into a new voice slot (audio shared with the
	// source; wave / gen pointers stay valid against the same
	// audio area), appends an Area at bstep with copied per-area
	// metadata (key range, vel range, root, MIDI chan, audio out,
	// volume), and bumps bstep.
	IntentDuplicateArea
)

// Intent carries the data the App needs when the user wants to leave
// Layout for another space.
type Intent struct {
	Kind    IntentKind
	BankIdx int // 0..7
	AreaIdx int // 0..63
	// SourceArea is the source Area index for IntentSwapAreas (the
	// first `m` press); AreaIdx carries the target (the second `m`
	// press). Unused for other intent kinds.
	SourceArea int
}

// Model is the Layout space state.
type Model struct {
	m    *model.Model
	info loader.ContainerInfo

	bankCursor int  // 0..7
	areaCursor int  // 0..63
	inBank     bool // true when drilled into a bank
	// areaScrollOffset is the top row of the area-list viewport.
	// Adjusted on Up/Down/PageUp/PageDown so the cursor stays visible
	// inside the pane's available rows.
	areaScrollOffset int
	// swapSource is the Area index marked as the first half of an
	// in-bank `m` swap, or -1 if no swap is pending. Set by the
	// first `m` press; cleared (with an IntentSwapAreas emission)
	// by the second.
	swapSource int
}

// New returns a Layout pointing at no container. Sets must be called
// before View can show anything meaningful.
func New() Model { return Model{swapSource: -1} }

// SetContainer points Layout at the given Model and info. Resets the
// cursors. Used by the App when a fresh container is opened.
func (lm *Model) SetContainer(m *model.Model, info loader.ContainerInfo) {
	lm.m = m
	lm.info = info
	lm.bankCursor = 0
	lm.areaCursor = 0
	lm.areaScrollOffset = 0
	lm.inBank = false
}

// RefreshContainer updates Layout's view of the container without
// resetting cursors. Used after in-place edits (assign / extract /
// delete) so the user stays on the page they were just looking at.
// Cursors are clamped if the new container has fewer banks.
func (lm *Model) RefreshContainer(m *model.Model, info loader.ContainerInfo) {
	lm.m = m
	lm.info = info
	if info.BankCount > 0 && lm.bankCursor >= info.BankCount {
		lm.bankCursor = info.BankCount - 1
	}
}

// HasContainer reports whether a container is loaded.
func (lm *Model) HasContainer() bool { return lm.m != nil }

// InBankList reports whether the Layout space is currently showing
// the bank list (true) versus an area list inside a drilled-into
// bank (false). The App uses this to route the `n` key to "new
// bank" when on the bank list, falling back to "new disk" otherwise.
func (lm *Model) InBankList() bool { return !lm.inBank }

// BankName returns the trimmed display name of the bank at bankIdx,
// or "" if the bank is unmaterialised (bankIdx >= info.BankCount).
// The App seeds the rename modal with this value.
func (lm Model) BankName(bankIdx int) string {
	if lm.m == nil || bankIdx < 0 || bankIdx >= lm.info.BankCount {
		return ""
	}
	return strings.TrimSpace(lm.bankName(bankIdx))
}

// SetPathLabel overrides the path string shown in the header without
// re-loading the container. Used by snapshot tests to mask the random
// tempdir path so snapshots are stable across runs.
func (lm *Model) SetPathLabel(label string) { lm.info.Path = label }

// SelectedArea returns the currently selected (bank, area) and
// whether one is selected. Used by the App to gate the transition
// to Sound on NavDown.
func (lm *Model) SelectedArea() (bankIdx, areaIdx int, ok bool) {
	if lm.m == nil || !lm.inBank {
		return 0, 0, false
	}
	return lm.bankCursor, lm.areaCursor, true
}

// Apply handles a navigation action. Returns a status message (for
// UI feedback) or an Intent (for App-routed transitions).
func (lm *Model) Apply(a nav.Action) (statusMsg string, intent Intent) {
	if lm.m == nil {
		return "", Intent{}
	}
	if !lm.inBank {
		return lm.applyBankList(a)
	}
	return lm.applyAreaList(a)
}

func (lm *Model) applyBankList(a nav.Action) (string, Intent) {
	switch a { //nolint:exhaustive // bank-list only consumes a subset of nav actions; default is no-op
	case nav.NavUp:
		if lm.bankCursor > 0 {
			lm.bankCursor--
		}
	case nav.NavDown:
		if lm.bankCursor < disk.MaxBanks-1 {
			lm.bankCursor++
		}
	case nav.NavTop, nav.NavPageUp:
		lm.bankCursor = 0
	case nav.NavBottom, nav.NavPageDown:
		lm.bankCursor = disk.MaxBanks - 1
	case nav.Confirm:
		// All 8 banks are reachable. Drilling into an unmaterialised
		// bank lands the user on its area list (all "(empty)"); the
		// bank sectors get inserted on the first assignment via
		// assignPoolEntryToArea's auto-grow.
		lm.inBank = true
		lm.areaCursor = 0
		lm.areaScrollOffset = 0
		return fmt.Sprintf("Opened Bank %d", lm.bankCursor+1), Intent{}
	case nav.Rename:
		return "", Intent{Kind: IntentRenameBank, BankIdx: lm.bankCursor}
	case nav.EditEffects:
		return "", Intent{Kind: IntentEditEffects, BankIdx: lm.bankCursor}
	case nav.Delete:
		if lm.bankCursor >= lm.info.BankCount {
			return fmt.Sprintf("Bank %d is already empty", lm.bankCursor+1), Intent{}
		}
		return "", Intent{Kind: IntentDeleteBank, BankIdx: lm.bankCursor}
	default:
		// Other nav actions are not meaningful on the bank list.
	}
	return "", Intent{}
}

func (lm *Model) applyAreaList(a nav.Action) (string, Intent) {
	switch a { //nolint:exhaustive // area-list only consumes a subset of nav actions; default is no-op
	case nav.NavUp:
		if lm.areaCursor > 0 {
			lm.areaCursor--
		}
		lm.keepCursorInView()
	case nav.NavDown:
		if lm.areaCursor < 63 {
			lm.areaCursor++
		}
		lm.keepCursorInView()
	case nav.NavTop:
		lm.areaCursor = 0
		lm.keepCursorInView()
	case nav.NavBottom:
		lm.areaCursor = 63
		lm.keepCursorInView()
	case nav.NavPageUp:
		lm.areaCursor = max(0, lm.areaCursor-lm.areaListVisibleRows())
		lm.keepCursorInView()
	case nav.NavPageDown:
		lm.areaCursor = min(63, lm.areaCursor+lm.areaListVisibleRows())
		lm.keepCursorInView()
	case nav.NavLeft:
		lm.inBank = false
		lm.areaScrollOffset = 0
		return "Returned to bank list", Intent{}
	case nav.Cancel:
		// Esc cancels a pending swap before falling back to "return
		// to bank list", so the user can back out of an m-marked
		// state without leaving the bank.
		if lm.swapSource != -1 {
			lm.swapSource = -1
			return "Swap cancelled", Intent{}
		}
		lm.inBank = false
		lm.areaScrollOffset = 0
		return "Returned to bank list", Intent{}
	case nav.Confirm:
		return "", Intent{Kind: IntentOpenSound, BankIdx: lm.bankCursor, AreaIdx: lm.areaCursor}
	case nav.Extract:
		return "", Intent{Kind: IntentExtractToPool, BankIdx: lm.bankCursor, AreaIdx: lm.areaCursor}
	case nav.Delete:
		return "", Intent{Kind: IntentDeleteArea, BankIdx: lm.bankCursor, AreaIdx: lm.areaCursor}
	case nav.Import:
		return "", Intent{Kind: IntentStartPicker, BankIdx: lm.bankCursor, AreaIdx: lm.areaCursor}
	case nav.Rename:
		return "", Intent{Kind: IntentRenameVoice, BankIdx: lm.bankCursor, AreaIdx: lm.areaCursor}
	case nav.Export:
		return "", Intent{Kind: IntentExportArea, BankIdx: lm.bankCursor, AreaIdx: lm.areaCursor}
	case nav.EditArea:
		return "", Intent{Kind: IntentEditArea, BankIdx: lm.bankCursor, AreaIdx: lm.areaCursor}
	case nav.Duplicate:
		return "", Intent{Kind: IntentDuplicateArea, BankIdx: lm.bankCursor, AreaIdx: lm.areaCursor}
	case nav.Move:
		// Two-step swap: first `m` marks the cursor as source; second
		// `m` (after navigating) emits IntentSwapAreas with both
		// indices. A second press on the SAME area cancels.
		if lm.swapSource == -1 {
			lm.swapSource = lm.areaCursor
			return fmt.Sprintf("Move: A%02d marked; press m on the target Area to swap (Esc to cancel)",
				lm.swapSource+1), Intent{}
		}
		if lm.swapSource == lm.areaCursor {
			lm.swapSource = -1
			return "Swap cancelled", Intent{}
		}
		src := lm.swapSource
		lm.swapSource = -1
		return "", Intent{
			Kind:       IntentSwapAreas,
			BankIdx:    lm.bankCursor,
			SourceArea: src,
			AreaIdx:    lm.areaCursor,
		}
	default:
		// Other nav actions are not meaningful on the area list.
	}
	return "", Intent{}
}

// areaListVisibleRows is the number of Area rows the viewport renders
// at once. Picked to fit comfortably inside the body pane on the
// default terminal sizes we target (40-row terminal yields ~32 lines
// minus chrome). All 64 Areas remain reachable via scrolling.
func (lm Model) areaListVisibleRows() int {
	return 20
}

// keepCursorInView nudges areaScrollOffset so that areaCursor falls
// within [top, top+visible-1]. Call after every cursor move.
func (lm *Model) keepCursorInView() {
	visible := lm.areaListVisibleRows()
	if lm.areaCursor < lm.areaScrollOffset {
		lm.areaScrollOffset = lm.areaCursor
	} else if lm.areaCursor >= lm.areaScrollOffset+visible {
		lm.areaScrollOffset = lm.areaCursor - visible + 1
	}
	if lm.areaScrollOffset < 0 {
		lm.areaScrollOffset = 0
	}
	if lm.areaScrollOffset > 64-visible {
		lm.areaScrollOffset = 64 - visible
	}
}

// decorateScrollIndicators tags the rendered table with arrow hints
// when the viewport doesn't show the full 64-row list. ▲/▼ render in
// dim text so they don't compete with the cursor highlight.
func (lm Model) decorateScrollIndicators(body string, top, end int) string {
	up := "  "
	if top > 0 {
		up = theme.DimText.Render("▲ more above (" + fmt.Sprintf("%d", top) + " hidden)")
	}
	down := "  "
	if end < 64 {
		down = theme.DimText.Render("▼ more below (" + fmt.Sprintf("%d", 64-end) + " hidden)")
	}
	if up == "  " && down == "  " {
		return body
	}
	return up + "\n" + body + "\n" + down
}

// VoiceOffset returns the absolute byte offset of the voice header for
// the given Area, and whether it is in range.
func (lm Model) VoiceOffset(bankIdx, areaIdx int) (int, bool) {
	_, off, ok := lm.VoiceSlot(bankIdx, areaIdx)
	return off, ok
}

// VoicePointerOffset returns the byte offset of the voice header that
// this Area's stored vp[] pointer references (the voice the list
// actually displays), and whether it is in bounds. Unlike VoiceOffset,
// which uses the cumulative-bstep allocation slot that *writers* assign
// into, this follows vp[areaIdx], so read-side operations (rename,
// extract-to-pool) target the correct voice on disks whose voice-table
// order differs from area order. Mirrors areaSummary's resolution.
func (lm Model) VoicePointerOffset(bankIdx, areaIdx int) (int, bool) {
	if lm.m == nil {
		return 0, false
	}
	data := lm.m.Bytes()
	slotIdx, ok := disk.BankVPLookup(data, bankIdx, areaIdx)
	if !ok {
		return 0, false
	}
	voiceAreaStart := lm.info.BankCount * disk.SectorSize
	off := disk.VoiceSlotOffset(voiceAreaStart, slotIdx)
	if off+disk.VoicePackSize > len(data) {
		return 0, false
	}
	return off, true
}

// VoiceSlotIndex returns the voice-area slot index this Area maps
// to under the cumulative-bstep allocation: sum of prior banks'
// bsteps + areaIdx. Pure math, never bounds-checks the buffer; safe
// for callers that grow the voice area before writing. The returned
// slot is what writers must set in bank's vp[areaIdx].
func (lm Model) VoiceSlotIndex(bankIdx, areaIdx int) int {
	if lm.m == nil {
		return 0
	}
	slot := 0
	for b := 0; b < bankIdx; b++ {
		slot += lm.bankAreaCount(b)
	}
	return slot + areaIdx
}

// VoiceSlot returns the voice-area slot index this assignment lands
// at, the byte offset of that slot in the current buffer, and an ok
// flag. ok=false means the slot lies past the current voice area
// (the buffer needs to grow before writing). Read paths should
// return early on ok=false; the assignment path computes the slot
// via VoiceSlotIndex and grows the buffer when needed.
func (lm Model) VoiceSlot(bankIdx, areaIdx int) (slotIdx, off int, ok bool) {
	if lm.m == nil {
		return 0, 0, false
	}
	voiceAreaStart := lm.info.BankCount * disk.SectorSize
	slotIdx = lm.VoiceSlotIndex(bankIdx, areaIdx)
	off = disk.VoiceSlotOffset(voiceAreaStart, slotIdx)
	if off+disk.VoicePackSize > len(lm.m.Bytes()) {
		return slotIdx, 0, false
	}
	return slotIdx, off, true
}

// VoiceName returns the trimmed voice name at the given Area, or
// "VOICE n" if the field is blank.
func (lm Model) VoiceName(bankIdx, areaIdx int) string {
	return lm.areaSummary(bankIdx, areaIdx).voiceName
}

// View renders the Layout pane.
func (lm Model) View(width, _ int) string {
	if lm.m == nil {
		header := theme.Heading.Render("Layout")
		body := theme.SilverText.Render(
			"No disk in focus.\n" +
				"Open a .img or .fzf from the Workspace to begin editing.")
		return header + "\n\n" + body
	}
	if !lm.inBank {
		return lm.viewBankList(width)
	}
	return lm.viewAreaList(width)
}

func (lm Model) viewBankList(width int) string {
	// Identity line: prefer the FZ disk label (what the hardware shows);
	// keep the file path alongside it, dimmed. .fzf dumps have no label,
	// so they show the path as before.
	var ident string
	switch {
	case lm.info.DiskLabel != "":
		ident = theme.PrimaryText.Render(fmt.Sprintf("%q", lm.info.DiskLabel))
		if lm.info.Path != "" {
			ident += "  " + theme.DimText.Render(lm.info.Path)
		}
	case lm.info.Path != "":
		ident = theme.PrimaryText.Render(lm.info.Path)
	default:
		ident = theme.DimText.Render("*untitled")
	}
	header := theme.Heading.Render("Layout") + "   " + ident +
		theme.DimText.Render(fmt.Sprintf("   (%d %s, %d %s)",
			lm.info.BankCount, plural(lm.info.BankCount, "bank", "banks"),
			lm.info.VoiceCount, plural(lm.info.VoiceCount, "voice", "voices")))

	// Always render all 8 banks. Banks past info.BankCount are
	// unmaterialised (they don't yet have a sector in the on-disk
	// buffer) and render as "(empty)". The first assignment to any
	// Area in an unmaterialised bank grows the buffer to include it.
	rows := make([][]string, 0, disk.MaxBanks)
	for i := 0; i < disk.MaxBanks; i++ {
		marker := ""
		if i == lm.bankCursor {
			marker = "▶"
		}
		if i >= lm.info.BankCount {
			rows = append(rows, []string{
				marker,
				fmt.Sprintf("Bank %d", i+1),
				emptyLabel,
				"",
			})
			continue
		}
		// A materialised bank with zero areas reads "(empty)" too, so
		// empty banks look the same whether or not they have a sector
		// yet; the materialised/unmaterialised split is an internal
		// detail the user shouldn't have to infer (F-C).
		areaLabel := emptyLabel
		if c := lm.bankAreaCount(i); c > 0 {
			areaLabel = fmt.Sprintf("(%d %s)", c, plural(c, "area", "areas"))
		}
		rows = append(rows, []string{
			marker,
			fmt.Sprintf("Bank %d", i+1),
			strings.TrimSpace(lm.bankName(i)),
			areaLabel,
		})
	}
	cursor := lm.bankCursor
	tableBody := newTable().
		Headers("", "bank", "name", "areas").
		Rows(rows...).
		StyleFunc(func(rowIdx, col int) lipgloss.Style {
			return cellStyle(rowIdx, col, cursor, 2)
		}).
		Render()
	hintBlock := hint.View(width,
		"A bank groups up to 64 Areas, each mapping a key and velocity range to a voice. Empty banks fill in on first assignment.")
	footer := theme.DimText.Render(
		"up/down move  •  enter open  •  r rename  •  f effects  •  del clear bank")
	return header + "\n\n" + tableBody + "\n\n" + footer + "\n\n" + hintBlock
}

func (lm Model) viewAreaList(width int) string {
	bankName := lm.bankName(lm.bankCursor)
	header := theme.Heading.Render(fmt.Sprintf("Bank %d", lm.bankCursor+1)) +
		"  " + theme.PrimaryText.Render(bankName)

	// Build all 64 rows up front; the viewport slices a window of them
	// based on areaScrollOffset and the visible-row count.
	allRows := make([][]string, 64)
	for i := 0; i < 64; i++ {
		// Cursor takes precedence over the swap mark (they coincide on
		// the first `m` press); once the cursor moves to pick a target,
		// the source row keeps the ⇄ marker so it stays visible.
		marker := ""
		switch i {
		case lm.areaCursor:
			marker = "▶"
		case lm.swapSource:
			marker = "⇄"
		}
		area := lm.areaSummary(lm.bankCursor, i)
		if !area.populated {
			allRows[i] = []string{
				marker,
				fmt.Sprintf("A%02d", i+1),
				emptyLabel,
				"",
				"",
			}
			continue
		}
		allRows[i] = []string{
			marker,
			fmt.Sprintf("A%02d", i+1),
			area.voiceName,
			area.keyRange,
			area.velRange,
		}
	}

	visible := lm.areaListVisibleRows()
	top := lm.areaScrollOffset
	if top > 64-visible {
		top = 64 - visible
	}
	if top < 0 {
		top = 0
	}
	end := top + visible
	if end > 64 {
		end = 64
	}
	rows := allRows[top:end]

	cursorInWindow := lm.areaCursor - top
	tableBody := newTable().
		Headers("", "#", "voice", "keys", "vel").
		Rows(rows...).
		StyleFunc(func(rowIdx, col int) lipgloss.Style {
			return cellStyle(rowIdx, col, cursorInWindow, 2)
		}).
		Render()
	tableBody = lm.decorateScrollIndicators(tableBody, top, end)
	hintBlock := hint.View(width,
		"Each row is an Area mapping a voice to a key and velocity range; rename starts with the current name selected so the first key replaces it.")
	footer := theme.DimText.Render(
		"up/down move  •  enter open  •  a area  •  i import  •  ctrl-d dup  •  r rename  •  c pool  •  del clear  •  esc back")
	return header + "\n\n" + tableBody + "\n\n" + footer + "\n\n" + hintBlock
}

// emptyLabel is shown for a bank with no areas, whether or not it has
// a sector yet (F-C).
const emptyLabel = "(empty)"

// plural returns one when n == 1 and many otherwise, so count labels
// read "1 bank" / "2 banks" instead of the ungrammatical "1 banks".
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// newTable returns a borderless lipgloss table with the column padding
// and header style used across studio's list views. Callers add
// headers, rows, and a StyleFunc.
func newTable() *table.Table {
	return table.New().
		Border(lipgloss.HiddenBorder()).
		BorderTop(false).BorderBottom(false).
		BorderLeft(false).BorderRight(false).
		BorderHeader(false).BorderColumn(false).BorderRow(false)
}

// cellStyle returns the lipgloss style applied to one cell of a list
// table. rowIdx == table.HeaderRow (-1) is the header. rowIdx == cursor
// is the focused row. nameCol is the index of the column that should
// carry the heading style on focus (typically the column holding the
// voice or bank name).
func cellStyle(rowIdx, col, cursor, nameCol int) lipgloss.Style {
	if rowIdx == table.HeaderRow {
		return theme.DimText.Padding(0, 1)
	}
	if rowIdx == cursor {
		if col == nameCol {
			return theme.Heading.Padding(0, 1)
		}
		return theme.AccentText.Padding(0, 1)
	}
	if col == nameCol {
		return theme.PrimaryText.Padding(0, 1)
	}
	return theme.DimText.Padding(0, 1)
}

func (lm Model) bankName(idx int) string {
	if lm.m == nil {
		return ""
	}
	data := lm.m.Bytes()
	base := idx * disk.SectorSize
	if base+disk.BankNameOffset+disk.VoiceNameFieldSize > len(data) {
		return ""
	}
	raw := data[base+disk.BankNameOffset : base+disk.BankNameOffset+disk.VoiceNameFieldSize]
	return strings.TrimRight(strings.Trim(string(raw), "\x00"), " ")
}

func (lm Model) bankAreaCount(idx int) int {
	if lm.m == nil {
		return 0
	}
	data := lm.m.Bytes()
	base := idx * disk.SectorSize
	if base+disk.BankVoiceCountOffset+2 > len(data) {
		return 0
	}
	v := int(binary.LittleEndian.Uint16(data[base+disk.BankVoiceCountOffset:]))
	if v < 0 {
		return 0
	}
	return v
}

type areaSummary struct {
	populated bool
	voiceName string
	keyRange  string
	velRange  string
}

func (lm Model) areaSummary(bankIdx, areaIdx int) areaSummary {
	if lm.m == nil {
		return areaSummary{}
	}
	if areaIdx >= lm.bankAreaCount(bankIdx) {
		return areaSummary{}
	}
	data := lm.m.Bytes()

	// Voice header for this Area lives in the voice area. Use the
	// bank's vp[] table to map (bankIdx, areaIdx) to the voice-slot
	// index; cumulative bstep across prior banks is wrong on multi-
	// bank disks where vp[] entries repeat or reorder voices (e.g.
	// Solo-Tenor-Sax-2M-Byte.fzf, where Bank 2's areas reference
	// voices Bank 1 owns).
	voiceAreaStart := lm.info.BankCount * disk.SectorSize
	slotIdx, ok := disk.BankVPLookup(data, bankIdx, areaIdx)
	if !ok {
		return areaSummary{}
	}
	voiceOff := disk.VoiceSlotOffset(voiceAreaStart, slotIdx)
	if voiceOff+disk.VoiceHeaderUsed > len(data) {
		return areaSummary{}
	}
	voice := data[voiceOff : voiceOff+disk.VoiceHeaderUsed]
	// A NoSound (loop-mode word = 0) slot is the empty placeholder;
	// fresh-zero slots match too. Render as "(empty)" by returning a
	// non-populated summary so the view's empty-slot branch fires.
	// Without this check, every gap inside the bstep range that
	// resolved to a zeroed slot rendered as "VOICE 1": the fallback
	// when the name field is blank.
	mode := binary.LittleEndian.Uint16(voice[disk.VoiceLoopModeOffset:])
	if mode == disk.PlaybackModeNoSound {
		return areaSummary{}
	}
	rawName := voice[disk.VoiceNameOffset : disk.VoiceNameOffset+disk.VoiceNameFieldSize]
	name := strings.TrimRight(strings.Trim(string(rawName), "\x00"), " ")
	if name == "" {
		name = fmt.Sprintf("VOICE %d", slotIdx+1)
	}

	// Per-slot key/vel range fields live in the bank sector.
	base := bankIdx * disk.SectorSize
	keyHigh := int(data[base+disk.BankKeyHighOffset+areaIdx])
	keyLow := int(data[base+disk.BankKeyLowOffset+areaIdx])
	velHigh := int(data[base+disk.BankVelHighOffset+areaIdx])
	velLow := int(data[base+disk.BankVelLowOffset+areaIdx])

	return areaSummary{
		populated: true,
		voiceName: name,
		keyRange:  fmt.Sprintf("%s-%s", fznote.Name(keyLow), fznote.Name(keyHigh)),
		velRange:  fmt.Sprintf("%d-%d", velLow, velHigh),
	}
}
