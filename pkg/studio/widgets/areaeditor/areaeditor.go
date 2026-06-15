// Package areaeditor is the spatial Area editor modal: a piano
// visualisation paired with editable low/high MIDI key fields,
// a velocity strip, and numeric cells for key-cent, audio out, and
// volume. As the user steps each field, the relevant visualisation
// (piano band or velocity strip) updates live.
package areaeditor

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/fznote"
	"github.com/philipcunningham/fizzle/pkg/studio/theme"
)

// Field identifies which editable cell currently has focus.
type Field int

// Field values identify each editable cell in the Area editor modal.
// numFields is a count sentinel and is never a valid focus target.
const (
	FieldKeyLow Field = iota
	FieldKeyHigh
	FieldKeyOrig // root key (MIDI note at which sample plays at original pitch)
	FieldVelLow
	FieldVelHigh
	FieldVolume
	FieldAudioOut
	FieldMIDIChan
	numFields
)

// SeedValues carries the per-Area state the caller pulls from the
// bank sector to seed the modal.
type SeedValues struct {
	KeyLow, KeyHigh int
	KeyOrig         int // MIDI 0..127; stored at BankKeyCentOffset (FZ-1 spec field `cent[]` = "root key")
	VelLow, VelHigh int
	Volume          int // 0..127
	AudioOut        int // gchn bitmask 0..255; studio cycles canonical states only
	MIDIChan        int // 0..15 on disk; displayed 1..16
}

// Model is the area-editor modal state.
type Model struct {
	open    bool
	bankIdx int
	areaIdx int

	keyLow, keyHigh int
	keyOrig         int
	velLow, velHigh int
	volume          int
	audioOut        int
	midiChan        int

	field Field

	// origin* hold the values at modal open, so commit can detect
	// no-ops and skip writing.
	originLow, originHigh       int
	originKeyOrig               int
	originVelLow, originVelHigh int
	originVolume                int
	originAudioOut              int
	originMIDIChan              int

	// audioOutExtra is set when the loaded gchn byte isn't one of
	// the canonical cycle values (poly / single-bit outputs). The
	// step function then treats this extra value as an additional
	// member of the cycle, so an exploratory Up/Down doesn't snap
	// the user out of a hand-edited multi-bit setting.
	audioOutExtra    uint8
	audioOutHasExtra bool
}

// New returns a closed modal.
func New() Model { return Model{} }

// Open binds the modal to (bankIdx, areaIdx) and seeds every editable
// value from the supplied SeedValues. Focus starts on FieldKeyLow.
func (m *Model) Open(bankIdx, areaIdx int, vals SeedValues) {
	m.open = true
	m.bankIdx = bankIdx
	m.areaIdx = areaIdx
	m.keyLow = clampKey(vals.KeyLow)
	m.keyHigh = clampKey(vals.KeyHigh)
	m.keyOrig = clampKey(vals.KeyOrig)
	m.velLow = clampByte(vals.VelLow)
	m.velHigh = clampByte(vals.VelHigh)
	m.volume = clampByte(vals.Volume)
	// Enforce the cross-field invariants at seed time. A corrupt
	// bank sector could carry low > high; without this, the
	// invariant only holds after the first HandleKey, and any
	// commit before then would write the inverted pair back to
	// disk. step() drags the other endpoint toward the edited
	// one, so we use the same drag-up rule here for symmetry.
	if m.keyLow > m.keyHigh {
		m.keyHigh = m.keyLow
	}
	if m.velLow > m.velHigh {
		m.velHigh = m.velLow
	}
	m.audioOut = clampAudioOut(vals.AudioOut)
	m.audioOutExtra, m.audioOutHasExtra = audioOutExtraFor(uint8(m.audioOut)) //nolint:gosec // G115: m.audioOut clamped to 0..255 by clampAudioOut above
	m.midiChan = clampMIDIChan(vals.MIDIChan)
	m.originLow = m.keyLow
	m.originHigh = m.keyHigh
	m.originKeyOrig = m.keyOrig
	m.originVelLow = m.velLow
	m.originVelHigh = m.velHigh
	m.originVolume = m.volume
	m.originAudioOut = m.audioOut
	m.originMIDIChan = m.midiChan
	m.field = FieldKeyLow
}

// Close clears modal state. Caller is responsible for committing
// the new values if it wants them to persist.
func (m *Model) Close() {
	m.open = false
}

// IsOpen reports whether the modal is currently shown.
func (m Model) IsOpen() bool { return m.open }

// BankIdx returns the index of the bank the modal is bound to.
func (m Model) BankIdx() int { return m.bankIdx }

// AreaIdx returns the index of the area within the bank the modal is bound to.
func (m Model) AreaIdx() int { return m.areaIdx }

// KeyLow returns the current low MIDI key of the area's key range.
func (m Model) KeyLow() int { return m.keyLow }

// KeyHigh returns the current high MIDI key of the area's key range.
func (m Model) KeyHigh() int { return m.keyHigh }

// KeyOrig returns the current root (original) MIDI key at which the sample plays at original pitch.
func (m Model) KeyOrig() int { return m.keyOrig }

// VelLow returns the current low velocity threshold of the area's velocity range.
func (m Model) VelLow() int { return m.velLow }

// VelHigh returns the current high velocity threshold of the area's velocity range.
func (m Model) VelHigh() int { return m.velHigh }

// Volume returns the current per-area volume value (0..127).
func (m Model) Volume() int { return m.volume }

// AudioOut returns the current audio-output gchn bitmask (0..255).
func (m Model) AudioOut() int { return m.audioOut }

// MIDIChan returns the current MIDI channel (0..15 on disk; displayed 1..16).
func (m Model) MIDIChan() int { return m.midiChan }

// Changed reports whether anything diverged from the seed. The App
// uses it to skip a no-op commit.
func (m Model) Changed() bool {
	return m.keyLow != m.originLow ||
		m.keyHigh != m.originHigh ||
		m.keyOrig != m.originKeyOrig ||
		m.velLow != m.originVelLow ||
		m.velHigh != m.originVelHigh ||
		m.volume != m.originVolume ||
		m.audioOut != m.originAudioOut ||
		m.midiChan != m.originMIDIChan
}

// HandleKey advances modal state for a single keypress. Tab cycles
// fields forward; Shift+Tab cycles back. Up/Down step the focused
// field by 1; Shift+Up/Down step by the field's "big" amount (12
// for key-related fields, 10 for byte-range fields, 1 for the small
// enum fields where 10 would overshoot).
func (m *Model) HandleKey(s string) {
	switch s {
	case "tab":
		m.field = (m.field + 1) % numFields
	case "shift+tab":
		m.field = (m.field - 1 + numFields) % numFields
	case "up":
		m.step(+1)
	case "down":
		m.step(-1)
	case "shift+up":
		m.step(+m.bigStep())
	case "shift+down":
		m.step(-m.bigStep())
	}
}

// bigStep returns the shift+arrow stride for the focused field.
func (m Model) bigStep() int {
	switch m.field {
	case FieldKeyLow, FieldKeyHigh, FieldKeyOrig:
		return 12 // octave
	case FieldVelLow, FieldVelHigh, FieldVolume:
		return 10
	case FieldAudioOut:
		return 1 // cycle has 9 states; small step = big step
	case FieldMIDIChan:
		return 4 // 16 channels; quarter-jump
	case numFields:
		// Count sentinel; never a valid focus target.
		return 1
	default:
		return 1
	}
}

func (m *Model) step(delta int) {
	switch m.field {
	case FieldKeyLow:
		m.keyLow = clampKey(m.keyLow + delta)
		if m.keyLow > m.keyHigh {
			m.keyHigh = m.keyLow
		}
	case FieldKeyHigh:
		m.keyHigh = clampKey(m.keyHigh + delta)
		if m.keyHigh < m.keyLow {
			m.keyLow = m.keyHigh
		}
	case FieldVelLow:
		m.velLow = clampByte(m.velLow + delta)
		if m.velLow > m.velHigh {
			m.velHigh = m.velLow
		}
	case FieldVelHigh:
		m.velHigh = clampByte(m.velHigh + delta)
		if m.velHigh < m.velLow {
			m.velLow = m.velHigh
		}
	case FieldKeyOrig:
		m.keyOrig = clampKey(m.keyOrig + delta)
	case FieldAudioOut:
		m.audioOut = int(audioOutStep(uint8(m.audioOut), delta, m.audioOutExtra, m.audioOutHasExtra)) //nolint:gosec // G115: m.audioOut held in 0..255 by clampAudioOut at every mutation site
	case FieldVolume:
		m.volume = clampByte(m.volume + delta)
	case FieldMIDIChan:
		m.midiChan = clampMIDIChan(m.midiChan + delta)
	case numFields:
		// Count sentinel; never a valid focus target.
	default:
	}
}

func clampKey(k int) int {
	if k < 0 {
		return 0
	}
	if k > 127 {
		return 127
	}
	return k
}

func clampByte(k int) int {
	if k < 0 {
		return 0
	}
	if k > 127 {
		return 127
	}
	return k
}

// clampMIDIChan clamps a MIDI channel value to the FZ-1's 0..15
// range (displayed as 1..16 to the user). 0xff would be valid on
// disk as "MIDI off" on some hardware but the FZ-1 spec doesn't
// reserve such a sentinel; we clamp to 0..15.
func clampMIDIChan(k int) int {
	if k < 0 {
		return 0
	}
	if k > 15 {
		return 15
	}
	return k
}

// audioOutCycle is the ordered list of canonical gchn states the
// editor steps through: 0xff (poly, all 8 generators) followed by
// the 8 single-bit values for outputs 1..8. Matches studio1's
// dropdown semantics; multi-bit / "none" values are readable in the
// display but not reachable by stepping (the first step from such
// a value snaps to a representable state).
var audioOutCycle = []uint8{0xff, 0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80}

// audioOutStep cycles cur through the canonical audioOutCycle by
// delta. When the modal was opened with a non-canonical value (a
// multi-bit gchn like 0x05 = outputs 1+3), the caller supplies it
// via (extra, hasExtra) and we splice it into the cycle so stepping
// past a multi-bit value is reversible. The extra slots in at the
// position between the canonical entries that bracket it numerically
// (e.g. 0x05 sits between 0x04 and 0x08), matching how the user reads
// the cycle visually.
func audioOutStep(cur uint8, delta int, extra uint8, hasExtra bool) uint8 {
	cycle := audioOutWithExtra(extra, hasExtra)
	idx := -1
	for i, v := range cycle {
		if v == cur {
			idx = i
			break
		}
	}
	if idx == -1 {
		// Still non-canonical and not the captured extra; snap to
		// an endpoint so the user can keep stepping.
		if delta >= 0 {
			return cycle[0]
		}
		return cycle[len(cycle)-1]
	}
	n := len(cycle)
	idx = (idx + delta%n + n) % n
	return cycle[idx]
}

// audioOutExtraFor returns the multi-bit gchn value to preserve as a
// cycle extra, or (0, false) if cur is one of the canonical entries.
func audioOutExtraFor(cur uint8) (uint8, bool) {
	for _, v := range audioOutCycle {
		if v == cur {
			return 0, false
		}
	}
	return cur, true
}

// audioOutWithExtra returns audioOutCycle with the extra value
// spliced in (sorted by underlying gchn byte after the poly entry).
// We keep poly (0xff) as index 0 since that's the user-facing
// "all outputs" anchor; the rest are sorted ascending so single-bit
// neighbours stay adjacent to a hand-edited multi-bit value.
func audioOutWithExtra(extra uint8, hasExtra bool) []uint8 {
	if !hasExtra {
		return audioOutCycle
	}
	rest := append([]uint8{}, audioOutCycle[1:]...)
	rest = append(rest, extra)
	for i := 1; i < len(rest); i++ {
		for j := i; j > 0 && rest[j-1] > rest[j]; j-- {
			rest[j-1], rest[j] = rest[j], rest[j-1]
		}
	}
	return append([]uint8{audioOutCycle[0]}, rest...)
}

// clampAudioOut clamps a stepped audio-out byte; treated as a raw
// uint8 so multi-bit values from the loaded file survive round-trip
// (the editor only mutates this byte via audioOutStep when the user
// actually cycles through it).
func clampAudioOut(k int) int {
	if k < 0 {
		return 0
	}
	if k > 255 {
		return 255
	}
	return k
}

// View renders the modal body. Caller composes it into the App's
// overlay layer (lipgloss compositor).
func (m Model) View() string {
	if !m.open {
		return ""
	}
	title := theme.Heading.Render(fmt.Sprintf(
		"Edit Area: Bank %d / A%02d",
		m.bankIdx+1, m.areaIdx+1))

	keyboard := renderKeyboard(m.keyLow, m.keyHigh)

	// All numeric fields stacked vertically beneath the keyboard.
	// Key Low/High sit directly under the keyboard since the keyboard
	// is their visualisation; the remaining fields follow with no
	// secondary visualisation.
	lines := []string{
		title,
		"",
		keyboard,
		"",
		theme.Field("Key Low  ", fmt.Sprintf("%d (%s)", m.keyLow, fznote.Name(m.keyLow)), m.field == FieldKeyLow),
		theme.Field("Key High ", fmt.Sprintf("%d (%s)", m.keyHigh, fznote.Name(m.keyHigh)), m.field == FieldKeyHigh),
		theme.Field("Key Orig ", fmt.Sprintf("%d (%s)", m.keyOrig, fznote.Name(m.keyOrig)), m.field == FieldKeyOrig),
		theme.Field("Vel Low  ", fmt.Sprintf("%d", m.velLow), m.field == FieldVelLow),
		theme.Field("Vel High ", fmt.Sprintf("%d", m.velHigh), m.field == FieldVelHigh),
		theme.Field("Volume   ", fmt.Sprintf("%d", m.volume), m.field == FieldVolume),
		theme.Field("Output   ", audioOutLabel(m.audioOut), m.field == FieldAudioOut),
		theme.Field("MIDI Chan", fmt.Sprintf("%d", m.midiChan+1), m.field == FieldMIDIChan),
		"",
		theme.DimText.Render("Tab cycle field  •  Up/Down step  •  Shift+Up/Down big step  •  Enter commit  •  Esc cancel"),
	}
	body := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 3).
		Render(body)
}

// audioOutLabel formats the audio-output bitmask the same way studio1
// did: "poly" for 0xff (all generators, full polyphony), "1".."8" for
// a single bit (one of the 8 physical output jacks, also implicitly a
// mute group), "1,3" etc. for multi-bit, and "none" for 0x00. Studio1
// preferred "poly" over "all" as the canonical name; we follow.
func audioOutLabel(v int) string {
	if v == int(disk.PolyphonicAudioOut) {
		return "poly"
	}
	return disk.FormatAudioOut(uint8(v)) //nolint:gosec // G115: v is the gchn byte read from disk (range 0..255)
}

// renderKeyboard draws an "accurate" piano (no in-band coloring on
// the keyboard itself) followed by a thin range bar underneath that
// highlights the [keyLow..keyHigh] span. Separating the two surfaces
// keeps the keyboard reading as a keyboard while still showing the
// focused range distinctly.
//
// Each white key is 2 cells wide: a left-edge bar + a body. The
// leftmost edge bar and the trailing right edge bar are rendered
// WITHOUT a silver background; they're just thin vertical lines on
// the terminal backdrop, so the silver "key area" starts exactly at
// the leftmost C body and ends at the rightmost B body (no overflow
// to either side). Internal bars and white-key bodies share the
// silver background so they form a continuous keyboard surface.
// Black keys are 1 cell wide and sit ON the boundary bar column in
// the TOP row only; the body row at that column still shows the
// white-white boundary bar.
//
// 4 octaves visible. The window is anchored to keyHigh: the octave
// containing keyHigh sits as the RIGHTMOST octave of the visible
// window, so the octave labels along the bottom always reflect the
// "tip" position. As keyHigh climbs into a new octave, the window
// slides forward by 12 and the labels increment accordingly.
func renderKeyboard(keyLow, keyHigh int) string {
	const visibleSemitones = 48
	// Octave containing keyHigh: start MIDI of that octave.
	octaveStart := (keyHigh / 12) * 12
	// Place that octave at the right end of the window: 3 octaves of
	// lower context, then keyHigh's own octave.
	startKey := octaveStart - (visibleSemitones - 12)
	if startKey < 0 {
		startKey = 0
	}
	if startKey+visibleSemitones > 128 {
		startKey = 128 - visibleSemitones
	}
	endKey := startKey + visibleSemitones

	whites := make([]int, 0, visibleSemitones)
	blackAfter := map[int]int{}
	for k := startKey; k < endKey; k++ {
		if isBlackKey(k) {
			if len(whites) > 0 {
				blackAfter[len(whites)-1] = k
			}
			continue
		}
		whites = append(whites, k)
	}

	totalCols := 2*len(whites) + 1
	var topRow, bodyRow, railRow, rangeRow, labelRow strings.Builder

	for i, w := range whites {
		// Bar col (col 2i).
		if i == 0 {
			topRow.WriteString(edgeBar.Render("│"))
			bodyRow.WriteString(edgeBar.Render("│"))
			railRow.WriteString(edgeRail.Render("└"))
		} else if _, ok := blackAfter[i-1]; ok {
			topRow.WriteString(blackGlyph.Render("█"))
			bodyRow.WriteString(internalBar.Render("│"))
			railRow.WriteString(internalRail.Render("┴"))
		} else {
			topRow.WriteString(internalBar.Render("│"))
			bodyRow.WriteString(internalBar.Render("│"))
			railRow.WriteString(internalRail.Render("┴"))
		}
		// Body col (col 2i+1).
		topRow.WriteString(whiteBody.Render(" "))
		bodyRow.WriteString(whiteBody.Render(" "))
		railRow.WriteString(internalRail.Render("─"))
		_ = w
	}
	// Trailing edge bar (col 2*len(whites)).
	topRow.WriteString(edgeBar.Render("│"))
	bodyRow.WriteString(edgeBar.Render("│"))
	railRow.WriteString(edgeRail.Render("┘"))

	// Range bar: walk each col and check whether the key(s) at that
	// col fall inside [keyLow..keyHigh]. Emit a cyan upper-eighth
	// block for in-band cols, a space otherwise. The bar reads as
	// a continuous coloured strip underneath the keyboard rail.
	for c := 0; c < totalCols; c++ {
		if colInBand(c, whites, blackAfter, keyLow, keyHigh) {
			rangeRow.WriteString(rangeAccent.Render("▔"))
		} else {
			rangeRow.WriteString(" ")
		}
	}

	// Labels: stamp "C<n>" anchored under each C white-key's body.
	// The label spans the body col + the next bar col (2 chars), so
	// for C at white-index i the label sits at cols 2i+1, 2i+2.
	labelCells := make([]rune, totalCols)
	for i := range labelCells {
		labelCells[i] = ' '
	}
	for i, w := range whites {
		if w%12 == 0 {
			lbl := fmt.Sprintf("C%d", w/12-1)
			for j := 0; j < len(lbl) && 2*i+1+j < totalCols; j++ {
				labelCells[2*i+1+j] = rune(lbl[j])
			}
		}
	}
	labelRow.WriteString(theme.DimText.Render(string(labelCells)))

	return strings.Join([]string{
		topRow.String(),
		bodyRow.String(),
		railRow.String(),
		"", // gap so the range bar doesn't visually fuse with the rail
		rangeRow.String(),
		labelRow.String(),
	}, "\n")
}

// colInBand reports whether the keyboard column c (in the
// per-2-cells-per-white grid) sits over any in-band MIDI note.
// White_i occupies cols 2i and 2i+1; a black key between white_i and
// white_(i+1) occupies col 2(i+1). Trailing bar (col 2*len(whites))
// belongs to white_(n-1).
func colInBand(c int, whites []int, blackAfter map[int]int, keyLow, keyHigh int) bool {
	inRange := func(k int) bool { return k >= keyLow && k <= keyHigh }
	n := len(whites)
	if n == 0 {
		return false
	}
	// Body cols: c is odd maps to white_(c/2), zero-indexed.
	if c%2 == 1 {
		idx := c / 2
		if idx < n && inRange(whites[idx]) {
			return true
		}
		return false
	}
	// Bar cols. c == 0 is white_0's left edge (no black to the left).
	if c == 0 {
		return inRange(whites[0])
	}
	// c == 2*n is the trailing bar after white_(n-1).
	if c == 2*n {
		return inRange(whites[n-1])
	}
	// Internal bar at col 2i (i > 0): between white_(i-1) and white_i,
	// possibly hosting a black key.
	i := c / 2
	if inRange(whites[i-1]) || inRange(whites[i]) {
		return true
	}
	if bk, ok := blackAfter[i-1]; ok && inRange(bk) {
		return true
	}
	return false
}

func isBlackKey(midi int) bool {
	pc := ((midi % 12) + 12) % 12
	switch pc {
	case 1, 3, 6, 8, 10:
		return true
	}
	return false
}

// Styles. The keyboard is rendered as a pure piano shape (no in-band
// coloring on the keys themselves); the range bar underneath shows
// the focused [keyLow..keyHigh] segment in accent cyan. This keeps
// the keyboard looking like a keyboard and gives the band its own
// dedicated visual channel.
var (
	// Internal: bars and white-key bodies inside the keyboard share
	// the silver background, so the keyboard area is a continuous
	// surface bounded by the edges.
	internalBar  = lipgloss.NewStyle().Foreground(theme.Tertiary).Background(theme.ContrastSecondary)
	whiteBody    = lipgloss.NewStyle().Background(theme.ContrastSecondary)
	internalRail = lipgloss.NewStyle().Foreground(theme.Tertiary)
	// Edge: leftmost and trailing bars have NO background; just a
	// thin foreground line on the terminal backdrop, so the silver
	// area doesn't bleed past the leftmost C or the rightmost B.
	edgeBar  = lipgloss.NewStyle().Foreground(theme.Tertiary)
	edgeRail = lipgloss.NewStyle().Foreground(theme.Tertiary)
	// Black-key glyph: full block in true-black on no background.
	// Against the silver keyboard area, the cell reads as a dark
	// gap, the black key.
	blackGlyph = lipgloss.NewStyle().Foreground(theme.Background)
	// Range bar: cyan upper-eighth block under in-band cols, drawn
	// directly beneath the rail so it visually "underlines" the
	// focused span.
	rangeAccent = lipgloss.NewStyle().Foreground(theme.Secondary)
)
