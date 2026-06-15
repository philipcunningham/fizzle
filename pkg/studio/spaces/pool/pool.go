// Package pool is the studio Pool space. The Pool accumulates voice
// entries the user gathers during a session: imports from .fzv files,
// imports from .wav files (wrapped on the fly via voiceimport.Encode),
// and extractions from an open disk's bank. Each entry carries the
// fzv-shaped bytes (192-byte header in the first sector plus appended
// 16-bit mono PCM); the App routes a confirmed entry to the in-focus
// container's Area on demand.
//
// Pool state lives in memory only for the session. Persistence to disk
// is the App's job (Save), not the Pool's.
package pool

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/studio/nav"
	"github.com/philipcunningham/fizzle/pkg/studio/theme"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/hint"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
)

// ErrStereoNeedsChoice signals that AddWAV was called on a stereo
// source without a channel choice. The App surfaces a confirmation
// prompt and calls AddWAV again with the chosen channel.
var ErrStereoNeedsChoice = errors.New("pool: stereo source; channel choice required")

// Source label values for Entry.Source. Pool exposes these as constants
// so the App can compare against them without re-typing string literals.
const (
	sourceFZV = "fzv"
	sourceWAV = "wav"
)

// Entry is one voice in the pool. Bytes holds the voice's full
// fzv-shaped representation: a 1024-byte first sector containing the
// 192-byte voice header (padded to a sector boundary) followed by the
// sample audio padded to a sector boundary.
type Entry struct {
	Name   string
	Source string // "fzv", "wav", or "bank N"
	Bytes  []byte // fzv-shaped bytes
}

// IntentKind tags App-level transitions Pool can request.
type IntentKind int

const (
	// IntentNone is the zero value; the App takes no transition.
	IntentNone IntentKind = iota
	// IntentAssignToArea signals the user confirmed an entry while in
	// picker mode; the App assigns the entry to whichever (bank, area)
	// it remembered when it opened the picker.
	IntentAssignToArea
	// IntentAuditionPoolEntry signals the user pressed Space on an
	// entry; the App routes to the audition path.
	IntentAuditionPoolEntry
	// IntentCancelPicker signals the user pressed Esc while the pool
	// was in picker mode; the App should close the picker without
	// assigning and return focus to where it came from.
	IntentCancelPicker
)

// Intent carries data the App needs to act on a Pool gesture.
type Intent struct {
	Kind  IntentKind
	Entry *Entry
}

// Model is the Pool space state.
type Model struct {
	entries []Entry
	cursor  int
	// pickerTarget is the human-readable Layout target an assignment
	// will land on when the user confirms (e.g. "Bank 1 / Area 3").
	// Non-empty iff the App has entered picker mode; the Pool's
	// Confirm action only emits IntentAssignToArea while in picker
	// mode.
	pickerTarget string
}

// SetPickerTarget puts the pool into picker mode if target is non-
// empty, displaying a banner and routing Confirm to an assign Intent.
// Pass "" to exit picker mode (browse only).
func (m *Model) SetPickerTarget(target string) { m.pickerTarget = target }

// InPickerMode reports whether the pool is currently the assignment
// picker (i.e., the App is mid-import-from-Layout).
func (m *Model) InPickerMode() bool { return m.pickerTarget != "" }

// New returns an empty Pool.
func New() Model { return Model{} }

// AddFZV reads the file at path, validates it as an FZ voice header,
// and adds it as a pool entry. The voice name comes from the header's
// 12-byte name field; if that field is blank or unprintable, the file
// stem is used instead.
func (m *Model) AddFZV(path string) error {
	data, err := fzutil.ReadFZV(path)
	if err != nil {
		return err
	}
	if !disk.IsPlausibleVoiceHeader(data) {
		return fmt.Errorf("pool: %q does not look like an FZV voice header", path)
	}
	name := disk.TrimPadded(data[disk.VoiceNameOffset : disk.VoiceNameOffset+disk.LabelSize])
	if name == "" {
		name = stemFromPath(path)
	}
	m.entries = append(m.entries, Entry{
		Name:   name,
		Source: sourceFZV,
		Bytes:  data,
	})
	return nil
}

// AddWAV reads the file at path, optionally reducing stereo to mono
// per the chosen channel, wraps the samples as an FZ voice via
// voiceimport.Encode with default envelope, loops, and key range, and
// adds the result as a pool entry.
//
// channel is 0 for left, 1 for right, 2 for sum-to-mono. For a mono
// WAV channel is ignored. Pass -1 when the caller has not chosen a
// channel; a stereo source then returns ErrStereoNeedsChoice so the
// App can prompt the user.
func (m *Model) AddWAV(path string, channel int) error {
	f, err := fzutil.ReadWAV(path)
	if err != nil {
		return err
	}

	if len(f.Samples) == 0 {
		return fmt.Errorf("pool: WAV %q contains no samples", path)
	}

	// Stereo: pick a channel (or mix) before downstream resample.
	// channel semantics: 0=left, 1=right, 2=sum-to-mono, -1=ask.
	if f.Channels >= 2 {
		switch channel {
		case -1:
			return ErrStereoNeedsChoice
		case 0:
			f.Samples = f.ExtractChannel(0)
		case 1:
			f.Samples = f.ExtractChannel(1)
		case 2:
			f.Samples = f.MixChannels()
		default:
			return fmt.Errorf("pool: invalid stereo channel %d (want -1, 0, 1, or 2)", channel)
		}
		f.Channels = 1
	}

	// Resample to a supported FZ rate so the audition path plays back
	// at the source's true pitch. Picking the source's native rate (or
	// the closest FZ rate at or below it) preserves audio fidelity
	// without forcing every voice up to 36 kHz when 18 or 9 will do.
	targetRate := ChooseFZRate(f.SampleRate)
	samples, err := fzutil.Resample(f, targetRate)
	if err != nil {
		return fmt.Errorf("pool: resample %q: %w", path, err)
	}
	rateIdx, ok := disk.RateIndexFor(targetRate)
	if !ok {
		rateIdx = 0
	}
	name := fzutil.VoiceName(path)
	bytes := voiceimport.Encode(samples, rateIdx, name, 0, voiceimport.NoLoop())
	m.entries = append(m.entries, Entry{
		Name:   name,
		Source: sourceWAV,
		Bytes:  bytes,
	})
	return nil
}

// AddFromAreaVoice copies the supplied voice bytes (a 192-byte header
// extended into a sector plus appended sample data, fzv-shaped) into
// the pool under the given source label (e.g. "bank 3"). The Pool
// stores the bytes verbatim; the caller owns the slice ownership
// handoff.
func (m *Model) AddFromAreaVoice(name, source string, voiceBytes []byte) {
	cp := make([]byte, len(voiceBytes))
	copy(cp, voiceBytes)
	m.entries = append(m.entries, Entry{
		Name:   name,
		Source: source,
		Bytes:  cp,
	})
}

// MirrorContainerVoices replaces any "bank N" sourced entries with
// fresh extractions from the supplied container bytes, then appends
// each voice the container holds as an FZV-shaped pool entry. Lets
// the user audition / extract / re-assign the disk's voices directly
// from the Pool without first drilling into Layout.
//
// Entries whose Source does not start with "bank " (imports from
// Workspace, explicit extractions) are left untouched.
func (m *Model) MirrorContainerVoices(extracted [][]byte) {
	kept := m.entries[:0]
	for _, e := range m.entries {
		if !strings.HasPrefix(e.Source, "bank ") {
			kept = append(kept, e)
		}
	}
	m.entries = kept
	for _, vbytes := range extracted {
		if len(vbytes) < disk.VoicePackSize {
			continue
		}
		name := disk.TrimPadded(vbytes[disk.VoiceNameOffset : disk.VoiceNameOffset+disk.LabelSize])
		if name == "" {
			name = "(unnamed)"
		}
		cp := make([]byte, len(vbytes))
		copy(cp, vbytes)
		m.entries = append(m.entries, Entry{
			Name:   name,
			Source: "bank",
			Bytes:  cp,
		})
	}
	if m.cursor >= len(m.entries) {
		if len(m.entries) == 0 {
			m.cursor = 0
		} else {
			m.cursor = len(m.entries) - 1
		}
	}
}

// Remove removes the entry at the given index. Out-of-bounds is a
// no-op. The cursor is clamped so it never points past the last
// remaining entry.
func (m *Model) Remove(index int) {
	if index < 0 || index >= len(m.entries) {
		return
	}
	m.entries = append(m.entries[:index], m.entries[index+1:]...)
	if m.cursor >= len(m.entries) && m.cursor > 0 {
		m.cursor = len(m.entries) - 1
	}
}

// Entries returns the current pool entries. The returned slice
// aliases the Pool's storage; callers must treat it as read-only.
func (m *Model) Entries() []Entry { return m.entries }

// Export writes the focused entry's FZV bytes to <toDir>/<name>.fzv,
// returning the absolute path written. The voice's Name (already
// space-trimmed) becomes the filename stem; non-portable characters
// are replaced with '_' so the result is round-trippable through the
// Workspace browser. Returns an error if the pool is empty or the
// write fails. The pool itself is not mutated.
func (m *Model) Export(toDir string) (string, error) {
	target, err := m.ExportTarget(toDir)
	if err != nil {
		return "", err
	}
	if err := m.ExportTo(target); err != nil {
		return "", err
	}
	return target, nil
}

// ExportTarget returns the absolute path Export would write to for the
// focused entry, without writing anything. The App uses it to check
// for an existing file and prompt before overwriting (N-05).
func (m *Model) ExportTarget(toDir string) (string, error) {
	entry := m.Selected()
	if entry == nil {
		return "", errors.New("pool is empty; nothing to export")
	}
	stem := sanitizeFZVStem(entry.Name)
	if stem == "" {
		stem = "VOICE"
	}
	return filepath.Join(toDir, stem+".fzv"), nil
}

// ExportTo writes the focused entry's FZV bytes to target. The pool is
// not mutated.
func (m *Model) ExportTo(target string) error {
	entry := m.Selected()
	if entry == nil {
		return errors.New("pool is empty; nothing to export")
	}
	if err := os.WriteFile(target, entry.Bytes, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	return nil
}

// sanitizeFZVStem turns a voice display name into a safe filename
// stem: ASCII letters / digits / space / dash survive; everything
// else is dropped. Trailing whitespace is stripped. Falls back to ""
// when nothing usable remains (Export then uses "VOICE").
func sanitizeFZVStem(name string) string {
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		ch := name[i]
		switch {
		case ch >= 'A' && ch <= 'Z',
			ch >= 'a' && ch <= 'z',
			ch >= '0' && ch <= '9',
			ch == '-', ch == '_':
			b.WriteByte(ch)
		case ch == ' ':
			b.WriteByte('_')
		}
	}
	return strings.TrimRight(b.String(), "_")
}

// Selected returns the focused entry, or nil if the pool is empty.
func (m *Model) Selected() *Entry {
	if len(m.entries) == 0 {
		return nil
	}
	return &m.entries[m.cursor]
}

// Cursor returns the cursor index for tests and the App. When the
// pool is empty the returned value is 0 but Selected() is the only
// safe reader.
func (m *Model) Cursor() int { return m.cursor }

// Apply handles a navigation action. Returns a status message and an
// Intent the App routes:
//
//   - NavUp / NavDown move the cursor; no intent.
//   - Audition emits IntentAuditionPoolEntry pointing at the focused
//     entry; no status message.
//   - Confirm in picker mode emits IntentAssignToArea pointing at the
//     focused entry. Outside picker mode Confirm is a no-op with a
//     status hint pointing the user at the Layout `i` gesture.
//   - Cancel in picker mode emits IntentCancelPicker so the App can
//     close the picker without assigning. Outside picker mode it is
//     a no-op (the App's modal stack handles Esc otherwise).
//   - Delete removes the focused entry directly and reports a status
//     message naming it.
//   - Other actions (including NavNone) are no-ops.
func (m *Model) Apply(a nav.Action) (statusMsg string, intent Intent) {
	if a == nav.Cancel && m.InPickerMode() {
		return "", Intent{Kind: IntentCancelPicker}
	}
	if len(m.entries) == 0 {
		return "", Intent{}
	}
	switch a { //nolint:exhaustive // pool only consumes a subset of nav actions; default is no-op
	case nav.NavUp:
		if m.cursor > 0 {
			m.cursor--
		}
	case nav.NavDown:
		if m.cursor < len(m.entries)-1 {
			m.cursor++
		}
	case nav.Audition:
		entry := &m.entries[m.cursor]
		return "", Intent{Kind: IntentAuditionPoolEntry, Entry: entry}
	case nav.Confirm:
		if !m.InPickerMode() {
			return "Press 'i' on an Area in Layout to import a voice into it.", Intent{}
		}
		entry := &m.entries[m.cursor]
		return "", Intent{Kind: IntentAssignToArea, Entry: entry}
	case nav.Delete:
		name := m.entries[m.cursor].Name
		m.Remove(m.cursor)
		return fmt.Sprintf("Removed %q from pool", name), Intent{}
	default:
		// Other nav actions (including NavNone) are no-ops in the pool.
	}
	return "", Intent{}
}

// View renders the Pool pane as a vertical list, with the cursor row
// marked by a right-pointing triangle. The target hint above the
// list tells the user which Layout slot a Confirm will assign to.
func (m *Model) View(width, _ int) string {
	header := theme.Heading.Render("Pool") +
		theme.DimText.Render(fmt.Sprintf("   (%d %s)",
			len(m.entries), pluralise("entry", "entries", len(m.entries))))
	targetLine := m.renderTarget()
	if len(m.entries) == 0 {
		body := theme.SilverText.Render(
			"Pool is empty. Add voices from the Workspace's file browser,\n" +
				"or extract from an open disk's bank (Ctrl-E in Layout).")
		return joinVertical(header, "", targetLine, "", body)
	}
	rows := make([][]string, 0, len(m.entries))
	for i, e := range m.entries {
		marker := ""
		if i == m.cursor {
			marker = "▶"
		}
		rows = append(rows, []string{
			marker,
			fmt.Sprintf("%2d", i+1),
			e.Name,
			e.Source,
		})
	}
	cursor := m.cursor
	tableBody := table.New().
		Border(lipgloss.HiddenBorder()).
		BorderTop(false).BorderBottom(false).
		BorderLeft(false).BorderRight(false).
		BorderHeader(false).BorderColumn(false).BorderRow(false).
		Headers("", "#", "name", "source").
		Rows(rows...).
		StyleFunc(func(rowIdx, col int) lipgloss.Style {
			if rowIdx == table.HeaderRow {
				return theme.DimText.Padding(0, 1)
			}
			if rowIdx == cursor {
				if col == 2 {
					return theme.Heading.Padding(0, 1)
				}
				return theme.AccentText.Padding(0, 1)
			}
			if col == 2 {
				return theme.PrimaryText.Padding(0, 1)
			}
			return theme.DimText.Padding(0, 1)
		}).
		Render()
	// Mode-aware footer. In picker mode the banner above already
	// states "Enter assigns / Esc cancels"; the cheatsheet line below
	// only surfaces the always-available actions.
	footerText := "space to audition  •  e to export .fzv  •  del to remove"
	hintText := "The pool holds voices imported, copied from Layout, or read from the loaded disk; assign any of them to a Layout Area."
	if m.InPickerMode() {
		// Lead with the picker's two primary actions (assign / cancel)
		// and push the destructive del to the end, so the modal whose
		// whole purpose is "pick one" says which key picks (N-03).
		footerText = "enter to assign  •  esc to cancel  •  space to audition  •  del to remove"
		hintText = "Picker mode: pick the voice to drop into " + m.pickerTarget + "; auditioning still works for preview."
	} else {
		footerText += "  •  press 'i' on an area in layout to import"
	}
	hintBlock := hint.View(width, hintText)
	footer := theme.DimText.Render(footerText)
	return joinVertical(header, "", targetLine, "", tableBody, "", footer, "", hintBlock)
}

// renderTarget returns the picker-mode banner line. Empty in browse
// mode, so the pool reads as a simple list when not actively assigning.
func (m *Model) renderTarget() string {
	if m.pickerTarget == "" {
		return ""
	}
	return theme.Heading.Render("Picking voice for ") +
		theme.AccentText.Render(m.pickerTarget) +
		theme.DimText.Render("  •  enter assigns  •  esc cancels")
}

func joinVertical(parts ...string) string {
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func pluralise(singular, plural string, n int) string {
	if n == 1 {
		return singular
	}
	return plural
}

// stemFromPath returns the file's base name without its extension.
// Used as a fallback voice name when the FZV header carries an
// unprintable name field.
func stemFromPath(path string) string {
	base := filepath.Base(path)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

// ChooseFZRate picks the FZ rate (36000 / 18000 / 9000 Hz) to target
// when importing a WAV at sourceRate. We pick the highest FZ rate
// that is at or below the source so we never upsample (which would
// pretend we have detail the WAV doesn't carry). Sources above 36 kHz
// get downsampled to 36 kHz so they fit the engine's playback rate.
func ChooseFZRate(sourceRate uint32) uint32 {
	switch {
	case sourceRate >= 36000:
		return 36000
	case sourceRate >= 18000:
		return 18000
	default:
		return 9000
	}
}
