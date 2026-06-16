// Package sound is the studio Sound space. Sound is voice-scoped
// editing of the currently selected Area's voice. The space is a 2D
// grid of subsystems (DCA, DCF, LFO, Sample, Loops) by cells. Each
// cell exposes its fields through editor.go; the App routes
// navigation actions to Apply.
package sound

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
	"github.com/philipcunningham/fizzle/pkg/studio/nav"
	"github.com/philipcunningham/fizzle/pkg/studio/theme"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/envelopevisual"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/hint"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/lfovisual"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/samplevisual"
)

// msgEditCancelled is the status line shown when the user backs out
// of a field edit. Centralised so all cancel paths emit the same text.
const msgEditCancelled = "Edit cancelled"

// labelVis is the short label for a cell's visual sub-pane (used by
// the row strip and the cell heading). Centralised because every row
// has one.
const labelVis = "[vis]"

// row identifies the subsystem the cursor is on.
type row int

const (
	rowDCA row = iota
	rowDCF
	rowLFO
	rowSample
	rowLoops
	numRows
)

func (r row) String() string {
	switch r {
	case rowDCA:
		return "DCA"
	case rowDCF:
		return "DCF"
	case rowLFO:
		return "LFO"
	case rowSample:
		return "Sample"
	case rowLoops:
		return "Loops"
	case numRows:
		// Sentinel value; falls through to the default "?".
	}
	return "?"
}

// rowHint returns the contextual hint for the focused row. Narrowed
// to the row under the cursor so the message describes what the user
// is actually editing, not all five rows at once.
func rowHint(r row) string {
	switch r {
	case rowDCA:
		return "DCA: per-stage envelope shaping the voice's amplitude over time."
	case rowDCF:
		return "DCF: filter cutoff, resonance, key-follow, and the filter's own ADSR envelope."
	case rowLFO:
		return "LFO: low-frequency oscillator routed to pitch and filter modulation."
	case rowSample:
		return "Sample: the raw audio mapping, including the playback root note and overall pitch."
	case rowLoops:
		return "Loops: start, end, and crossfade settings for the sustaining loop region."
	case numRows:
		// Sentinel value; falls through to "".
	}
	return ""
}

// cellCount returns the number of cells in a given row.
func cellCount(r row) int {
	switch r {
	case rowDCA:
		return 11 // visual, level KF/VF, rate KF/VF, 8 stages
	case rowDCF:
		return 12 // visual, cutoff/res/vRes, level KF/VF, rate KF/VF, 8 stages
	case rowLFO:
		return 3 // visual, shape, depths
	case rowSample:
		return 6 // visual, rate, gen, root, tune, mode (name lives in Layout's `r` gesture)
	case rowLoops:
		return 10 // visual, sus/release pointers, 8 per-loop cells
	case numRows:
		// Sentinel value; falls through to the default of 1.
	}
	return 1
}

// Model is the Sound space state.
type Model struct {
	m        *model.Model
	bankIdx  int
	areaIdx  int
	hasVoice bool

	row row
	col int

	// In-cell editor state.
	editMode   bool   // true once user has entered a cell's field editor
	fieldIdx   int    // which field within the current cell is focused
	draft      string // text-edit buffer (only used for fieldText)
	draftFresh bool   // true when the draft was just pre-loaded; the next
	// printable keystroke clears it (mimics the "select
	// all text" feel on text-editor focus). Cleared once
	// the user backspaces or types a char, so further
	// edits accumulate in-place.
	numericDraft string // direct-entry buffer for numeric fields; "" when stepping

	// Cached on Bind.
	voiceOff       int
	voiceArea      int
	audioArea      int // byte offset of the start of the shared wave area
	voiceName      string
	containerBytes []byte // alias to m.Bytes(); refreshed on every Apply
}

// New returns an empty Sound space.
func New() Model { return Model{} }

// Bind points Sound at a specific (bank, area) within the Model.
// audioAreaStart is the byte offset where the shared wave area begins
// (after the bank sectors and the voice header sectors). The caller is
// responsible for deriving it from the FZF header so Sound does not
// have to walk bank bsteps itself.
func (sm *Model) Bind(m *model.Model, _, voiceAreaStart, audioAreaStart, bankIdx, areaIdx int) {
	sm.m = m
	sm.bankIdx = bankIdx
	sm.areaIdx = areaIdx
	sm.row = rowDCA
	sm.col = 1 // skip visual cell on first land
	sm.editMode = false
	sm.fieldIdx = 0
	sm.draft = ""

	data := m.Bytes()
	slotIdx, ok := disk.BankVPLookup(data, bankIdx, areaIdx)
	sm.voiceArea = voiceAreaStart
	sm.audioArea = audioAreaStart
	if !ok {
		sm.hasVoice = false
		return
	}
	sm.voiceOff = disk.VoiceSlotOffset(voiceAreaStart, slotIdx)
	sm.hasVoice = sm.voiceOff+disk.VoiceHeaderUsed <= len(data)

	if sm.hasVoice {
		raw := data[sm.voiceOff+disk.VoiceNameOffset : sm.voiceOff+disk.VoiceNameOffset+disk.VoiceNameFieldSize]
		sm.voiceName = sanitizeVoiceName(raw)
		if sm.voiceName == "" {
			sm.voiceName = fmt.Sprintf("VOICE %d", slotIdx+1)
		}
	}
	sm.containerBytes = data
}

// sanitizeVoiceName strips non-printable ASCII from a raw 14-byte
// voice name field and trims surrounding whitespace + nulls. Some
// corpus files (notably casio-fz1-soundwaves entries) carry
// embedded control bytes inside the name region; rendering those
// raw breaks terminal layout and trips the corpus-sweep width
// canary. Non-printable bytes (anything outside 0x20..0x7E) become
// '?' so the name still surfaces shape and length, just without
// the control-char weirdness.
func sanitizeVoiceName(raw []byte) string {
	var b strings.Builder
	b.Grow(len(raw))
	for _, c := range raw {
		switch {
		case c >= 0x20 && c <= 0x7E:
			b.WriteByte(c)
		case c == 0x00:
			// drop; embedded nulls are the usual padding
		default:
			b.WriteByte('?')
		}
	}
	return strings.TrimSpace(b.String())
}

// Unbind clears the Sound space.
func (sm *Model) Unbind() {
	sm.m = nil
	sm.hasVoice = false
	sm.editMode = false
}

// HasVoice reports whether a voice is bound for editing.
func (sm *Model) HasVoice() bool { return sm.hasVoice }

// InEditMode reports whether the user is currently editing a field
// within the current cell (so the App should keep navigation local
// to Sound).
func (sm *Model) InEditMode() bool { return sm.editMode }

// Apply handles a navigation action.
func (sm *Model) Apply(a nav.Action) string {
	if !sm.hasVoice {
		return ""
	}
	if sm.editMode {
		return sm.applyEdit(a)
	}
	return sm.applyNav(a)
}

func (sm *Model) applyNav(a nav.Action) string {
	switch a { //nolint:exhaustive // grid nav only consumes a subset of nav actions; default is no-op
	case nav.NavUp:
		if sm.row > 0 {
			sm.row--
			sm.col = clampInt(sm.col, 0, cellCount(sm.row)-1)
		}
	case nav.NavDown:
		if sm.row < numRows-1 {
			sm.row++
			sm.col = clampInt(sm.col, 0, cellCount(sm.row)-1)
		}
	case nav.NavLeft:
		if sm.col > 0 {
			sm.col--
		}
	case nav.NavRight:
		if sm.col < cellCount(sm.row)-1 {
			sm.col++
		}
	case nav.Confirm:
		fields := cellFields(sm.row, sm.col, sm.voiceOff)
		if len(fields) == 0 {
			return ""
		}
		sm.editMode = true
		sm.fieldIdx = 0
		if fields[0].kind == fieldText {
			sm.draft = fields[0].readText(sm.containerBytes)
			sm.draftFresh = true
		}
		return fmt.Sprintf("Editing %s: Up/Down adjusts, Tab next field, Enter commit, Esc cancel", fields[0].label)
	default:
		// Other nav actions are not meaningful while navigating Sound's grid.
	}
	return ""
}

// commit applies a patch batch to the model and refreshes the
// containerBytes alias. Every edit path goes through here so the alias
// (documented as "refreshed on every Apply") can never drift out of
// sync with the model's bytes.
func (sm *Model) commit(patches []model.Patch) error {
	if err := sm.m.ApplyBatch(patches); err != nil {
		return err
	}
	sm.containerBytes = sm.m.Bytes()
	return nil
}

func (sm *Model) applyEdit(a nav.Action) string {
	fields := cellFields(sm.row, sm.col, sm.voiceOff)
	if len(fields) == 0 {
		sm.editMode = false
		return ""
	}
	f := fields[sm.fieldIdx]

	switch a { //nolint:exhaustive // field edit only consumes a subset of nav actions; default is no-op
	case nav.Cancel:
		sm.editMode = false
		sm.draft = ""
		return msgEditCancelled
	case nav.Confirm:
		// Commit the current field and exit edit mode, so a following
		// arrow navigates the grid rather than adjusting the just-
		// committed value (UXA). Move between fields within a multi-field
		// cell with Left/Right while editing; Enter ends the cell edit.
		var patches []model.Patch
		if f.kind == fieldText {
			patches = f.patchText(sm.containerBytes, sm.draft)
		} else {
			patches = f.patch(sm.containerBytes, f.read(sm.containerBytes))
		}
		// f.patch returned no patches: nothing changed; that's ok.
		if len(patches) > 0 {
			if err := sm.commit(patches); err != nil {
				return fmt.Sprintf("Commit failed: %v", err)
			}
		}
		sm.editMode = false
		sm.draft = ""
		return "Committed"
	case nav.NavUp:
		return sm.adjustField(fields, +1)
	case nav.NavDown:
		return sm.adjustField(fields, -1)
	case nav.NavLeft:
		// Move to previous field within the cell, or exit edit mode.
		if sm.fieldIdx > 0 {
			sm.fieldIdx--
			next := fields[sm.fieldIdx]
			if next.kind == fieldText {
				sm.draft = next.readText(sm.containerBytes)
				sm.draftFresh = true
			}
			return fmt.Sprintf("Editing %s", next.label)
		}
		sm.editMode = false
		return ""
	case nav.NavRight:
		if sm.fieldIdx+1 < len(fields) {
			sm.fieldIdx++
			next := fields[sm.fieldIdx]
			if next.kind == fieldText {
				sm.draft = next.readText(sm.containerBytes)
				sm.draftFresh = true
			}
			return fmt.Sprintf("Editing %s", next.label)
		}
		return ""
	default:
		// Other nav actions are not meaningful inside a field edit.
	}
	return ""
}

// InTextEditMode reports whether the user is currently editing a
// text-typed field. The App uses this to route raw key presses to
// ConsumeTextKey instead of through the nav.Action layer (which has
// no encoding for "this is a typed character").
func (sm *Model) InTextEditMode() bool {
	if !sm.editMode {
		return false
	}
	fields := cellFields(sm.row, sm.col, sm.voiceOff)
	if sm.fieldIdx < 0 || sm.fieldIdx >= len(fields) {
		return false
	}
	return fields[sm.fieldIdx].kind == fieldText
}

// InNumericEditMode reports whether the user is currently editing an
// integer-typed field (signed or unsigned). The App routes raw key
// presses to ConsumeNumericKey so the editor can offer modifier-step
// adjusts (Shift / PgUp / Alt) and direct numeric entry; neither of
// which can be expressed through the nav.Action layer.
func (sm *Model) InNumericEditMode() bool {
	if !sm.editMode {
		return false
	}
	fields := cellFields(sm.row, sm.col, sm.voiceOff)
	if sm.fieldIdx < 0 || sm.fieldIdx >= len(fields) {
		return false
	}
	k := fields[sm.fieldIdx].kind
	return k == fieldUnsigned || k == fieldSigned
}

// NumericDraft returns the in-progress direct-entry string, or "" when
// the user has not started typing digits.
func (sm *Model) NumericDraft() string { return sm.numericDraft }

// ConsumeTextKey handles a key press during text-field edit mode.
// Returns a status message (for App's status line) or "" when the
// key was consumed silently. Supports printable ASCII, Backspace,
// Enter (commit), and Esc (cancel).
func (sm *Model) ConsumeTextKey(keyStr string) string {
	if !sm.InTextEditMode() {
		return ""
	}
	fields := cellFields(sm.row, sm.col, sm.voiceOff)
	f := fields[sm.fieldIdx]
	switch keyStr {
	case "esc":
		sm.editMode = false
		sm.draft = ""
		return msgEditCancelled
	case "enter":
		patches := f.patchText(sm.containerBytes, sm.draft)
		if len(patches) > 0 {
			if err := sm.commit(patches); err != nil {
				return fmt.Sprintf("Commit failed: %v", err)
			}
		}
		sm.editMode = false
		sm.draft = ""
		return fmt.Sprintf("Saved %s", f.label)
	case "backspace":
		// Backspace switches the user into in-place edit mode.
		sm.draftFresh = false
		if len(sm.draft) > 0 {
			sm.draft = sm.draft[:len(sm.draft)-1]
		}
		return ""
	}
	// keyStr is what String() reports; useful for named keys but it
	// returns "space" for the spacebar (we want " "). We don't have
	// the raw msg here, so accept both forms.
	if keyStr == "space" {
		keyStr = " "
	}
	if len(keyStr) == 1 {
		ch := normaliseVoiceNameByte(keyStr[0])
		if ch == 0 {
			return ""
		}
		// The FZ-1 voice-name field is uppercase ASCII; auto-
		// uppercase what the user types. If the draft is "fresh
		// from pre-load," the first printable keystroke clears it
		// (matches the "selected text" feel users expect), so renames
		// don't require pre-backspacing 12 chars.
		if sm.draftFresh {
			sm.draft = ""
			sm.draftFresh = false
		}
		if len(sm.draft) < disk.LabelSize {
			sm.draft += string(ch)
		}
	}
	return ""
}

// normaliseVoiceNameByte returns the FZ-1-legal uppercase form of b,
// or 0 to mean "drop this character". Allowed: A-Z, 0-9, space, dash.
// Lowercase letters auto-uppercase.
func normaliseVoiceNameByte(b byte) byte {
	switch {
	case b >= 'A' && b <= 'Z':
		return b
	case b >= 'a' && b <= 'z':
		return b - 32
	case b >= '0' && b <= '9':
		return b
	case b == ' ' || b == '-':
		return b
	}
	return 0
}

// ConsumeNumericKey is the App's entry point for keys pressed while a
// numeric field is in edit mode. It handles:
//
//   - Modifier-stepped arrow adjusts: Up/Down = ±1, Shift+Up/Down =
//     ±10, PgUp/Dn = ±100, Alt+Up/Down = ±1000.
//   - Left/Right cycle to the previous/next field within the cell.
//   - Direct numeric entry: typing a digit (or `-` for signed fields)
//     starts a draft, Backspace deletes a character, Enter validates
//     against the field's [min, max] and commits, Esc cancels the
//     draft (or exits edit mode when no draft is active).
//
// Returns a status-line string; "" when the key was consumed silently.
func (sm *Model) ConsumeNumericKey(keyStr string) string {
	if !sm.InNumericEditMode() {
		return ""
	}
	fields := cellFields(sm.row, sm.col, sm.voiceOff)
	f := fields[sm.fieldIdx]

	switch keyStr {
	case "esc":
		if sm.numericDraft != "" {
			sm.numericDraft = ""
			return "Direct entry cancelled"
		}
		sm.editMode = false
		return msgEditCancelled
	case "enter":
		if sm.numericDraft != "" {
			return sm.commitNumericDraft(f)
		}
		// No draft: the value is already in the buffer from live step
		// adjusts; just end the edit (UXA).
		return sm.endEdit()
	case "backspace":
		if sm.numericDraft != "" && len(sm.numericDraft) > 0 {
			sm.numericDraft = sm.numericDraft[:len(sm.numericDraft)-1]
		}
		return ""
	case "left":
		if sm.numericDraft != "" {
			sm.numericDraft = ""
		}
		if sm.fieldIdx > 0 {
			sm.fieldIdx--
			return fmt.Sprintf("Editing %s", fields[sm.fieldIdx].label)
		}
		sm.editMode = false
		return ""
	case "right":
		if sm.numericDraft != "" {
			sm.numericDraft = ""
		}
		if sm.fieldIdx+1 < len(fields) {
			sm.fieldIdx++
			return fmt.Sprintf("Editing %s", fields[sm.fieldIdx].label)
		}
		return ""
	case "up":
		return sm.stepIfNotTyping(f, +1)
	case "down":
		return sm.stepIfNotTyping(f, -1)
	case "shift+up":
		return sm.stepIfNotTyping(f, +10)
	case "shift+down":
		return sm.stepIfNotTyping(f, -10)
	case "pgup":
		return sm.stepIfNotTyping(f, +100)
	case "pgdown":
		return sm.stepIfNotTyping(f, -100)
	case "alt+up":
		return sm.stepIfNotTyping(f, +1000)
	case "alt+down":
		return sm.stepIfNotTyping(f, -1000)
	}
	// Direct entry: digits, or '-' (only at the head of a signed
	// field's draft).
	if len(keyStr) == 1 {
		c := keyStr[0]
		if c >= '0' && c <= '9' {
			sm.numericDraft += keyStr
			return ""
		}
		if c == '-' && f.kind == fieldSigned && sm.numericDraft == "" {
			sm.numericDraft = "-"
			return ""
		}
	}
	return ""
}

// stepIfNotTyping refuses to apply arrow-step adjusts while a direct-
// entry draft is open, so a user halfway through typing doesn't have
// the stored value shift under them. Arrows resume their step
// behaviour once the draft is committed (Enter) or cancelled (Esc).
func (sm *Model) stepIfNotTyping(f field, delta int) string {
	if sm.numericDraft != "" {
		return "Press Enter to commit the typed value, or Esc to cancel"
	}
	return sm.adjustNumericField(f, delta)
}

func (sm *Model) adjustNumericField(f field, delta int) string {
	current := f.read(sm.containerBytes)
	next := clampInt(current+delta, f.min, f.max)
	if next == current {
		return ""
	}
	patches := f.patch(sm.containerBytes, next)
	if len(patches) == 0 {
		return ""
	}
	if err := sm.commit(patches); err != nil {
		return fmt.Sprintf("Adjust failed: %v", err)
	}
	return ""
}

func (sm *Model) commitNumericDraft(f field) string {
	draft := sm.numericDraft
	sm.numericDraft = ""
	n, err := strconv.Atoi(draft)
	if err != nil {
		return fmt.Sprintf("Invalid number %q", draft)
	}
	if n < f.min || n > f.max {
		return fmt.Sprintf("%s out of range (%d..%d)", f.label, f.min, f.max)
	}
	patches := f.patch(sm.containerBytes, n)
	if len(patches) > 0 {
		if err := sm.commit(patches); err != nil {
			return fmt.Sprintf("Commit failed: %v", err)
		}
	}
	return sm.endEdit()
}

// endEdit returns the editor to nav mode after a commit, so a following
// arrow navigates the grid rather than adjusting the just-committed value
// (UXA). Field-to-field movement within a multi-field cell stays on
// Left/Right while editing.
func (sm *Model) endEdit() string {
	sm.editMode = false
	sm.draft = ""
	return "Committed"
}

// adjustField increments or decrements the focused field's value by
// delta (typically +1 / -1) and commits immediately so the user sees
// the change reflected on the rendered cell.
func (sm *Model) adjustField(fields []field, delta int) string {
	f := fields[sm.fieldIdx]
	if f.kind == fieldText {
		return "" // text fields are typed; ConsumeTextKey handles them
	}
	current := f.read(sm.containerBytes)
	next := current + delta
	// Wrap enum values; clamp numeric ranges.
	switch f.kind {
	case fieldEnum:
		nOpts := len(f.options)
		if nOpts > 0 {
			next = ((next % nOpts) + nOpts) % nOpts
		}
	case fieldUnsigned, fieldSigned:
		next = clampInt(next, f.min, f.max)
	case fieldText:
		// Unreachable; the early return above handles fieldText.
	default:
		// Future field kinds: leave next unchanged.
	}
	patches := f.patch(sm.containerBytes, next)
	if len(patches) == 0 {
		return ""
	}
	if err := sm.commit(patches); err != nil {
		return fmt.Sprintf("Adjust failed: %v", err)
	}
	return ""
}

// View renders the Sound pane.
func (sm Model) View(width, height int) string {
	if !sm.hasVoice {
		header := theme.Heading.Render("Sound")
		body := theme.SilverText.Render(
			"Select an Area in Layout first.\n" +
				"Sound is reachable only when an Area is selected.")
		return header + "\n\n" + body
	}

	header := theme.Heading.Render("Sound") +
		theme.DimText.Render(fmt.Sprintf("   Bank %d / Area %d  ", sm.bankIdx+1, sm.areaIdx+1)) +
		theme.PrimaryText.Render(sm.voiceName)

	rowLabels := []string{}
	for r := row(0); r < numRows; r++ {
		marker := "  "
		style := theme.SilverText
		if r == sm.row {
			marker = "▶ "
			style = theme.Heading
		}
		rowLabels = append(rowLabels, marker+style.Render(padRight(r.String(), 7)))
	}

	strip := sm.renderRowStrip(sm.row, sm.col)
	cell := sm.renderCell(sm.row, sm.col)

	hints := "up/down switch row  •  left/right switch cell  •  enter to edit  •  esc back to layout"
	hintText := rowHint(sm.row)
	if sm.editMode {
		if sm.InNumericEditMode() {
			hints = "up/down ±1  •  shift ±10  •  pgup/dn ±100  •  alt ±1000  •  type digits to set  •  enter commit  •  esc close"
			hintText = "Editing a number; step with the modifiers or type the value directly, then commit."
		} else {
			hints = "up/down adjusts  •  enter commit  •  esc close"
			hintText = "Editing a value; cycle through the choices and commit when you're settled."
		}
	}
	hintBlock := hint.View(width, hintText)
	footer := theme.DimText.Render(hints)

	body := strings.Join(rowLabels, "\n") + "\n\n" +
		strip + "\n\n" +
		cell

	_ = height
	return header + "\n\n" + body + "\n\n" + footer + "\n\n" + hintBlock
}

func (sm Model) renderRowStrip(r row, focusCol int) string {
	n := cellCount(r)
	cells := make([]string, n)
	for i := 0; i < n; i++ {
		label := stripCellLabel(r, i)
		if i == focusCol {
			label = "[" + label + "]"
		}
		cells[i] = label
	}
	return table.New().
		Border(lipgloss.HiddenBorder()).
		BorderTop(false).BorderBottom(false).
		BorderLeft(false).BorderRight(false).
		BorderHeader(false).BorderColumn(false).BorderRow(false).
		Rows(cells).
		StyleFunc(func(_, col int) lipgloss.Style {
			if col == focusCol {
				return theme.Heading.Padding(0, 2)
			}
			return theme.SilverText.Padding(0, 2)
		}).
		Render()
}

// stripCellLabel returns the short cell-strip label without the
// surrounding brackets used in cellLabel's longer form. Focus is
// communicated by re-bracketing only the focused cell, which keeps
// the strip scannable in ANSI-stripped output.
func stripCellLabel(r row, idx int) string {
	full := cellLabel(r, idx)
	return strings.TrimSuffix(strings.TrimPrefix(full, "["), "]")
}

func cellLabel(r row, idx int) string {
	switch r {
	case rowDCA:
		switch idx {
		case 0:
			return labelVis
		case 1:
			return "[lvlKF/VF]"
		case 2:
			return "[rateKF/VF]"
		default:
			return fmt.Sprintf("[s%d]", idx-3)
		}
	case rowDCF:
		switch idx {
		case 0:
			return labelVis
		case 1:
			return "[cut/res/vR]"
		case 2:
			return "[lvlKF/VF]"
		case 3:
			return "[rateKF/VF]"
		default:
			return fmt.Sprintf("[s%d]", idx-4)
		}
	case rowLFO:
		switch idx {
		case 0:
			return labelVis
		case 1:
			return "[shape]"
		case 2:
			return "[depths]"
		}
	case rowSample:
		labels := []string{labelVis, "[rate]", "[gen]", "[root]", "[tune]", "[mode]"}
		if idx >= 0 && idx < len(labels) {
			return labels[idx]
		}
	case rowLoops:
		switch idx {
		case 0:
			return labelVis
		case 1:
			return "[ptrs]"
		default:
			return fmt.Sprintf("[L%d]", idx-2)
		}
	case numRows:
		// Sentinel value; falls through to "[?]".
	}
	return "[?]"
}

func (sm Model) renderCell(r row, col int) string {
	voice := sm.containerBytes[sm.voiceOff : sm.voiceOff+disk.VoiceHeaderUsed]

	switch r {
	case rowDCA, rowDCF:
		return sm.renderEnvelopeCell(voice, r, col)
	case rowLFO:
		return sm.renderLFOCell(voice, col)
	case rowSample:
		return sm.renderSampleCell(voice, col)
	case rowLoops:
		return sm.renderLoopsCell(voice, col)
	case numRows:
		// Sentinel value; falls through to "".
	}
	return ""
}

func (sm Model) renderEnvelopeCell(voice []byte, r row, col int) string {
	var sus, end uint8
	var rates, levels []uint8
	if r == rowDCA {
		sus = voice[disk.VoiceDCASusOffset]
		end = voice[disk.VoiceDCAEndOffset]
		rates = voice[disk.VoiceDCARateOffset : disk.VoiceDCARateOffset+8]
		levels = voice[disk.VoiceDCAStopOffset : disk.VoiceDCAStopOffset+8]
	} else {
		sus = voice[disk.VoiceDCFSusOffset]
		end = voice[disk.VoiceDCFEndOffset]
		rates = voice[disk.VoiceDCFRateOffset : disk.VoiceDCFRateOffset+8]
		levels = voice[disk.VoiceDCFStopOffset : disk.VoiceDCFStopOffset+8]
	}

	if col == 0 {
		env := envelopevisual.Envelope{Sus: int(sus), End: int(end)}
		copy(env.Rates[:], rates)
		copy(env.StopLevels[:], levels)
		return cellHeading(fmt.Sprintf("%s envelope", r.String())) +
			theme.DimText.Render(fmt.Sprintf("    Sus = %d   End = %d", sus, end)) +
			"\n\n" + envelopevisual.View(env, -1, 60, 12)
	}
	return sm.renderFieldsList(r, col)
}

func (sm Model) renderLFOCell(voice []byte, col int) string {
	if col == 0 {
		wave := lfovisual.Waveform(voice[disk.VoiceLFONameOffset] & disk.LFOWaveformMask)
		return cellHeading("LFO waveform") + "\n\n" + lfovisual.View(wave, 60, 8)
	}
	return sm.renderFieldsList(rowLFO, col)
}

// genSamples extracts the voice's gen-range PCM (int16 frames) from the
// shared wave area, returning the decoded samples and the gen-start
// sample index (needed to translate absolute loop pointers into
// slice-relative indices). Sample pointers count int16 frames, so the
// byte offset is pointer*2 from the wave-area start. An out-of-bounds or
// empty range yields an empty slice.
func (sm Model) genSamples(voice []byte) (samples []int16, genStart int) {
	genStart = int(binary.LittleEndian.Uint32(voice[disk.VoiceWaveStartOffset:]))
	genEnd := int(binary.LittleEndian.Uint32(voice[disk.VoiceWaveEndOffset:]))
	startByte := sm.audioArea + genStart*2
	endByte := sm.audioArea + genEnd*2
	data := sm.containerBytes
	samples = []int16{}
	if startByte >= 0 && endByte <= len(data) && endByte > startByte {
		samples = make([]int16, (endByte-startByte)/2)
		for i := range samples {
			samples[i] = int16(binary.LittleEndian.Uint16(data[startByte+i*2:])) //nolint:gosec // G115: PCM samples are signed 16-bit; reinterpreting the unsigned read as int16 preserves the audio value.
		}
	}
	return samples, genStart
}

func (sm Model) renderSampleCell(voice []byte, col int) string {
	if col == 0 {
		// genStart is discarded here: only renderLoopsVisual needs it (to
		// translate absolute loop pointers into slice-relative indices).
		samples, _ := sm.genSamples(voice)
		return cellHeading("Sample waveform") + "\n\n" + samplevisual.View(samplevisual.Sample{
			Data:     samples,
			GenStart: 0,
			GenEnd:   len(samples),
		}, 60, 8)
	}
	return sm.renderFieldsList(rowSample, col)
}

func (sm Model) renderLoopsCell(voice []byte, col int) string {
	if col == 0 {
		return sm.renderLoopsVisual(voice)
	}
	return sm.renderFieldsList(rowLoops, col)
}

// renderLoopsVisual draws the sample waveform with every active loop
// pair overlaid as start/end markers. Inactive loops (those whose end
// address is zero or whose end is not strictly after the start) are
// skipped. The waveform itself reuses the same Gen-bounded slice the
// Sample visual renders, so the loop coordinates land at the correct
// columns when samplevisual is given LoopStarts/LoopEnds relative to
// that slice.
func (sm Model) renderLoopsVisual(voice []byte) string {
	samples, genStartSamples := sm.genSamples(voice)

	// Loop pointers in the header are absolute sample addresses into
	// the shared wave area (same coordinate space as wave_start /
	// gen_*). Translate them to indices into our locally-extracted
	// samples slice by subtracting genStartSamples.
	loopStarts := make([]int, 0, 8)
	loopEnds := make([]int, 0, 8)
	for i := 0; i < 8; i++ {
		stOff := disk.VoiceLoopSt0Offset + i*4
		edOff := disk.VoiceLoopEd0Offset + i*4
		rawSt := binary.LittleEndian.Uint32(voice[stOff:])
		rawEd := binary.LittleEndian.Uint32(voice[edOff:])
		ls := int(disk.LoopStartAddress(rawSt))
		le := int(disk.LoopEndAddress(rawEd))
		if le <= ls {
			continue
		}
		lsRel := ls - genStartSamples
		leRel := le - genStartSamples
		if lsRel < 0 || leRel > len(samples) || lsRel >= leRel {
			continue
		}
		loopStarts = append(loopStarts, lsRel)
		loopEnds = append(loopEnds, leRel)
	}

	// Annotate the heading with the active sustain loop and the
	// release-loop pointer so the user sees the pair the FZ-1 will use
	// without scrolling through every loop's fields.
	susIdx := voice[disk.VoiceLoopSusOffset]
	heading := "Loops"
	switch {
	case len(loopStarts) == 0:
		heading += "  (no active loops)"
	case susIdx >= disk.NoSustainLoop:
		heading += fmt.Sprintf("  (%d active, no sustain loop)", len(loopStarts))
	default:
		heading += fmt.Sprintf("  (%d active, sustain loop %d)", len(loopStarts), susIdx+1)
	}

	vis := samplevisual.View(samplevisual.Sample{
		Data:       samples,
		GenStart:   0,
		GenEnd:     len(samples),
		LoopStarts: loopStarts,
		LoopEnds:   loopEnds,
	}, 60, 8)

	return cellHeading(heading) + "\n\n" + vis
}

// renderFieldsList renders the labeled fields of the current cell as
// a table so the value column lines up across rows regardless of the
// label's length. In edit mode the focused field is highlighted.
func (sm Model) renderFieldsList(r row, col int) string {
	fields := cellFields(r, col, sm.voiceOff)
	if len(fields) == 0 {
		return cellHeading(cellLabel(r, col)) + "\n" + theme.DimText.Render("  (no editable fields)")
	}
	rows := make([][]string, 0, len(fields))
	for i, f := range fields {
		marker := ""
		focused := sm.editMode && i == sm.fieldIdx
		if focused {
			marker = "▶"
		}
		rows = append(rows, []string{
			marker,
			f.label,
			sm.formatFieldValue(f, focused),
		})
	}
	focus := -1
	if sm.editMode {
		focus = sm.fieldIdx
	}
	body := table.New().
		Border(lipgloss.HiddenBorder()).
		BorderTop(false).BorderBottom(false).
		BorderLeft(false).BorderRight(false).
		BorderHeader(false).BorderColumn(false).BorderRow(false).
		Rows(rows...).
		StyleFunc(func(rowIdx, col int) lipgloss.Style {
			if rowIdx == focus {
				if col == 1 {
					return theme.Heading.Padding(0, 1)
				}
				return theme.AccentText.Padding(0, 1)
			}
			if col == 1 {
				return theme.PrimaryText.Padding(0, 1)
			}
			return theme.SilverText.Padding(0, 1)
		}).
		Render()
	return cellHeading(cellLabel(r, col)) + "\n" + body
}

func (sm Model) formatFieldValue(f field, focused bool) string {
	switch f.kind {
	case fieldUnsigned, fieldSigned:
		// During direct entry, show what the user is typing (with a
		// trailing underscore as a "you're editing this" cue) instead
		// of the value still stored in the buffer.
		if focused && sm.numericDraft != "" {
			return sm.numericDraft + "_"
		}
		v := f.read(sm.containerBytes)
		if f.kind == fieldSigned && v >= 0 {
			return fmt.Sprintf("+%d", v)
		}
		return fmt.Sprintf("%d", v)
	case fieldEnum:
		v := f.read(sm.containerBytes)
		if v >= 0 && v < len(f.options) {
			return f.options[v]
		}
		return fmt.Sprintf("(%d)", v)
	case fieldText:
		s := f.readText(sm.containerBytes)
		if focused {
			// Show the typed draft. When the draft is fresh (just
			// pre-loaded on entry to edit mode), underline it so the
			// user sees it's "selected" and a single keystroke will
			// overwrite (same affordance text-editors use).
			if sm.draftFresh && sm.draft != "" {
				return theme.AccentText.Underline(true).Render(sm.draft)
			}
			if sm.draft == "" {
				return "_"
			}
			return sm.draft + "_"
		}
		if s == "" {
			return theme.DimText.Render("(empty)")
		}
		return s
	}
	return ""
}

func cellHeading(s string) string {
	return theme.AccentText.Render("◆ " + s)
}

func padRight(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}
