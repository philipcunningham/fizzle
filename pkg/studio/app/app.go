// Package app wires the studio spaces, widgets, and key map into a
// single Bubble Tea v2 program. App is the top-level tea.Model and
// orchestrates routing actions to the focused space, container
// load/save flow, undo/redo dispatch, modal stack (Help and
// Confirm), the minimap, and the status channel. See
// pkg/studio/README.md for the user-facing feature set and the
// editing model.
package app

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/fileutil"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/studio/audio"
	"github.com/philipcunningham/fizzle/pkg/studio/clock"
	"github.com/philipcunningham/fizzle/pkg/studio/container"
	"github.com/philipcunningham/fizzle/pkg/studio/loader"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
	"github.com/philipcunningham/fizzle/pkg/studio/nav"
	"github.com/philipcunningham/fizzle/pkg/studio/spaces/layout"
	"github.com/philipcunningham/fizzle/pkg/studio/spaces/pool"
	"github.com/philipcunningham/fizzle/pkg/studio/spaces/sound"
	"github.com/philipcunningham/fizzle/pkg/studio/spaces/workspace"
	"github.com/philipcunningham/fizzle/pkg/studio/theme"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/areaeditor"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/confirm"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/effectseditor"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/help"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/minimap"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/status"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/toast"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/topbar"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
)

const (
	// Minimum terminal dimensions. The footer hint band alone is
	// 135 columns; bumping the floor to fit it (with a few cols of
	// slack) means the layout doesn't get clipped or mid-rendered
	// at "officially supported but actually broken" sizes. Bigger
	// floor, fewer broken renders.
	minCols = 140
	minRows = 30
)

// Key-name string literals shared across switch arms. Bubble Tea
// reports keys by string; pulling these into constants keeps the
// dispatch sites uniform and stops goconst from grumbling about the
// repetition.
const (
	keyEsc   = "esc"
	keyEnter = "enter"
	keyLeft  = "left"
	keyRight = "right"
)

// UI label / title strings reused by multiple confirm prompts.
const (
	cancelLabel         = "Cancel"
	unsavedChangesTitle = "Unsaved changes"
)

// App is the studio top-level model.
type App struct {
	directory string

	current minimap.Space

	// In-focus container.
	containerModel *model.Model
	containerInfo  loader.ContainerInfo

	workspace workspace.Model
	pool      pool.Model
	layout    layout.Model
	sound     sound.Model

	minimap minimap.Model
	help    help.Model
	status  status.Model
	toast   toast.Model

	width, height int
	tooSmall      bool

	// Save-as modal state.
	saveAsActive bool
	saveAsBuffer string

	// Confirmation modal.
	confirm       *confirm.Model
	confirmResult <-chan int
	// pendingAction is invoked when the confirmation modal resolves.
	// May return a tea.Cmd (e.g. tea.Quit on a "Discard unsaved
	// changes" confirmation) so the App can hand control back to
	// Bubble Tea after the user decides.
	pendingAction func(result int) (App, tea.Cmd)

	// Autosave config. backupDir is the directory to write .bak files
	// into (defaults to os.TempDir()/fizzle-studio-backups). backupKeep
	// is the number of timestamped snapshots to retain per base name.
	// Tests override backupDir to t.TempDir() so they don't pollute
	// /tmp.
	backupDir  string
	backupKeep int

	// Picker mode: when non-nil, the Pool is acting as a voice picker
	// scoped to (BankIdx, AreaIdx). Confirm on a pool entry assigns
	// into that slot; Esc cancels. Set by Layout's `i` gesture.
	pickingFor *pickerTarget

	// Rename modal state. Set by Layout's `r` / F2 gesture; cleared on
	// Esc or after the new name is patched into the voice header or
	// bank name field. renameBank discriminates the target: true =
	// bank name field at renameTarget.BankIdx; false = voice header
	// at (BankIdx, AreaIdx).
	renameActive bool
	renameBank   bool
	renameTarget pickerTarget // reuse the (BankIdx, AreaIdx) shape
	renameBuffer string
	renameFresh  bool // first printable keystroke clears the buffer

	// Area editor modal: piano visualisation with live preview of
	// the key range as the user edits low/high. Spike scope.
	areaEditor areaeditor.Model

	// Effects editor modal: per-bank bend + 3x7 controller
	// modulation matrix. Opened from the bank list with `f`.
	effectsEditor effectseditor.Model

	// Test seam for tea.Tick-driven timers (autosave, toast
	// dismiss). Production wires clock.Real(); tests inject a
	// FakeClock that records Tick calls and returns a no-op Cmd.
	tick clock.TickFn

	// pendingStatusCmd carries the dismiss tick returned by the
	// most recent status.Set so the Update wrapper can hand it
	// back to Bubble Tea's cmd loop. Callers route through the
	// setStatus helper (not status.Set directly) so this field
	// stays in sync without every caller needing to thread a
	// tea.Cmd back through its return path.
	pendingStatusCmd tea.Cmd

	// Sound-space typed clipboard. Populated by Ctrl-C on a Sound
	// cell; consumed by Ctrl-V on a compatible Sound cell. Lives on
	// App so the clipboard survives binding / unbinding a voice in
	// Sound (the same clipboard can paste across voices in different
	// banks). Copy / Paste outside Sound are no-ops.
	clipboard sound.Clipboard
}

type pickerTarget struct {
	BankIdx, AreaIdx int
	ReturnFocus      minimap.Space
}

// defaultBackupKeep is the number of timestamped autosave snapshots to
// retain per base name. Older snapshots are pruned on each new write.
const defaultBackupKeep = 10

// New returns an App scanning the given directory. The App starts in
// Workspace with an untitled container ready (so Layout works
// without a load step). Autosave writes go to
// os.TempDir()/fizzle-studio-backups, not the workspace directory.
func New(directory string) App {
	m, info := loader.NewUntitled()
	a := App{
		directory:      directory,
		current:        minimap.Workspace,
		containerModel: m,
		containerInfo:  info,
		workspace:      workspace.New(directory),
		pool:           pool.New(),
		layout:         layout.New(),
		sound:          sound.New(),
		minimap:        minimap.New(),
		help:           help.New(),
		status:         status.New(),
		toast:          toast.New(),
		confirm:        confirm.New(),
		areaEditor:     areaeditor.New(),
		effectsEditor:  effectseditor.New(),
		// Autosave snapshots live next to the container they shadow,
		// under {workspace}/{base}.bak. Tests override backupDir to
		// isolate from the real workspace.
		backupDir:  directory,
		backupKeep: defaultBackupKeep,
		tick:       clock.Real(),
	}
	a.status.SetClock(a.tick)
	a.layout.SetContainer(m, info)
	return a
}

// setStatus is a thin wrapper around a.status.Set that captures the
// returned dismiss tick into pendingStatusCmd. The Update wrapper
// drains that field and batches the tick with whatever cmd the
// inner dispatch returned, so every status.Set callsite gets
// auto-dismiss wired without threading a tea.Cmd through 138
// return values. Pointer receiver so callsites in value-receiver
// methods (the common shape) can call a.setStatus(...) and have
// the field write land on the local copy.
func (a *App) setStatus(s status.Severity, text string) {
	a.pendingStatusCmd = a.status.Set(s, text)
}

// autoSaveTick is fired every 30 seconds by tea.Tick to drive the
// auto-save snapshot.
type autoSaveTick struct{}

// Init implements tea.Model.
func (a App) Init() tea.Cmd {
	return a.tick(30*time.Second, func(time.Time) tea.Msg { return autoSaveTick{} })
}

// Update implements tea.Model. It wraps the inner dispatch in
// update so any dismiss tick captured by setStatus during the
// update is batched with whatever cmd the dispatch returned. The
// status widget's tick wouldn't otherwise reach Bubble Tea's cmd
// loop, since most callsites of a.setStatus don't carry the
// tea.Cmd back through their (App, tea.Cmd) return path.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := a.update(msg)
	app, ok := m.(App)
	if !ok {
		return m, cmd
	}
	if app.pendingStatusCmd != nil {
		pending := app.pendingStatusCmd
		app.pendingStatusCmd = nil
		if cmd == nil {
			cmd = pending
		} else {
			cmd = tea.Batch(cmd, pending)
		}
	}
	return app, cmd
}

// update is the inner dispatch. The public Update wraps it so
// pendingStatusCmd is drained and batched. Anything new that
// schedules a tea.Cmd should still return it from update directly;
// only the status dismiss tick rides the pending-cmd seam.
func (a App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.tooSmall = msg.Width < minCols || msg.Height < minRows
		return a, nil

	case autoSaveTick:
		a.runAutoSave()
		return a, a.tick(30*time.Second, func(time.Time) tea.Msg { return autoSaveTick{} })

	case toast.DismissMsg:
		a.toast.Dismiss(msg)
		return a, nil

	case status.DismissMsg:
		a.status.Dismiss(msg)
		return a, nil

	case tea.KeyMsg:
		if a.tooSmall {
			if msg.String() == "ctrl+q" {
				return a, tea.Quit
			}
			return a, nil
		}

		// Save-as modal owns input when open.
		if a.saveAsActive {
			return a.handleSaveAsKey(msg)
		}

		// Rename modal owns input when open.
		if a.renameActive {
			return a.handleRenameKey(msg)
		}

		// Area editor modal owns input when open.
		if a.areaEditor.IsOpen() {
			return a.handleAreaEditorKey(msg)
		}

		// Effects editor modal owns input when open.
		if a.effectsEditor.IsOpen() {
			return a.handleEffectsEditorKey(msg)
		}

		// Confirmation modal owns input when open.
		if a.confirm.IsOpen() {
			return a.handleConfirmKey(msg)
		}

		// Sound text-edit mode: route raw key presses (printable
		// characters, Backspace, Enter, Esc) directly to Sound so the
		// user can type voice / sample names. The nav.Action layer
		// can't carry per-character input.
		if a.current == minimap.Sound && a.sound.InTextEditMode() {
			if msgStr := msg.String(); msgStr != "" {
				if s := a.sound.ConsumeTextKey(msgStr); s != "" {
					a.setStatus(status.Info, s)
				}
				return a, nil
			}
		}
		// Sound numeric-edit mode: raw keys go to ConsumeNumericKey
		// so modifier-step adjusts (Shift / PgUp / Alt) and direct
		// digit entry can read the raw modifier state, neither of
		// which fits through the nav.Action layer.
		if a.current == minimap.Sound && a.sound.InNumericEditMode() {
			if msgStr := msg.String(); msgStr != "" {
				if s := a.sound.ConsumeNumericKey(msgStr); s != "" {
					a.setStatus(status.Info, s)
				}
				return a, nil
			}
		}

		// Help modal owns input when open.
		if a.help.IsOpen() {
			switch msg.String() {
			case keyEsc, "?":
				a.help.Close()
			}
			return a, nil
		}

		action := nav.FromKey(msg)
		return a.routeAction(action)
	}
	return a, nil
}

func (a App) handleSaveAsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc:
		a.saveAsActive = false
		a.saveAsBuffer = ""
		a.setStatus(status.Info, "Save-as cancelled")
		return a, nil
	case keyEnter:
		return a.commitSaveAs()
	case "backspace":
		if len(a.saveAsBuffer) > 0 {
			a.saveAsBuffer = a.saveAsBuffer[:len(a.saveAsBuffer)-1]
		}
		return a, nil
	default:
		// Accept printable runes for filename input. Restrict to a
		// safe character set: letters, digits, underscore, hyphen,
		// period.
		r := msg.String()
		if len(r) == 1 {
			c := r[0]
			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
				(c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' {
				a.saveAsBuffer += r
			}
		}
		return a, nil
	}
}

func (a App) handleRenameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc:
		a.renameActive = false
		a.renameBank = false
		a.renameBuffer = ""
		a.renameFresh = false
		a.setStatus(status.Info, "Rename cancelled")
		return a, nil
	case keyEnter:
		return a.commitRename()
	case "backspace":
		a.renameFresh = false
		if len(a.renameBuffer) > 0 {
			a.renameBuffer = a.renameBuffer[:len(a.renameBuffer)-1]
		}
		return a, nil
	}
	// Append the typed character. Pull the text from msg.Key().Text
	// (which carries " " for the spacebar) rather than msg.String()
	// (which returns "space"). FZ voice names are uppercase ASCII:
	// letters, digits, space, dash; everything else is dropped so
	// what the user sees on screen matches what the FZ-1 LCD would
	// display.
	if kp, ok := msg.(tea.KeyPressMsg); ok {
		text := kp.Key().Text
		if len(text) == 1 {
			ch := normaliseRenameKey(text[0])
			if ch != 0 {
				if a.renameFresh {
					a.renameBuffer = ""
					a.renameFresh = false
				}
				if len(a.renameBuffer) < disk.LabelSize {
					a.renameBuffer += string(ch)
				}
			}
		}
	}
	return a, nil
}

// normaliseRenameKey filters a typed character to the FZ-1's voice-
// name character set: uppercase letters, digits, space, dash. Returns
// 0 to mean "ignore this key" (e.g. punctuation we don't allow).
// Lowercase letters auto-uppercase so the user doesn't have to hold
// shift.
func normaliseRenameKey(b byte) byte {
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

func (a App) commitRename() (tea.Model, tea.Cmd) {
	if a.renameBank {
		return a.commitBankRename()
	}
	off, ok := a.layout.VoiceOffset(a.renameTarget.BankIdx, a.renameTarget.AreaIdx)
	if !ok {
		a.setStatus(status.Warning, "Rename: voice slot out of bounds")
		a.renameActive = false
		return a, nil
	}
	data := a.containerModel.Bytes()
	old := make([]byte, disk.VoiceNameFieldSize)
	copy(old, data[off+disk.VoiceNameOffset:off+disk.VoiceNameOffset+disk.VoiceNameFieldSize])
	// Build a padded fixed-size name field: 12 chars (printable) +
	// two-byte null terminator.
	padded := disk.PadLabel(a.renameBuffer)
	newBytes := make([]byte, disk.VoiceNameFieldSize)
	copy(newBytes, padded[:])
	if err := a.containerModel.Apply(model.Patch{
		Offset: off + disk.VoiceNameOffset,
		Old:    old,
		New:    newBytes,
	}); err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Rename failed: %v", err))
		return a, nil
	}
	a.setStatus(status.Success,
		fmt.Sprintf("Renamed Bank %d / Area %d to %q",
			a.renameTarget.BankIdx+1, a.renameTarget.AreaIdx+1, strings.TrimRight(a.renameBuffer, " ")))
	a.renameActive = false
	a.renameBuffer = ""
	a.renameFresh = false
	return a, nil
}

// commitBankRename writes the new name into the bank's name field.
// Auto-grows BankCount up to and including bankIdx first if the bank
// is unmaterialised; growth failures (multi-disk first half,
// WrappedVoice, MaxBanks) bubble up the same way as assignment.
func (a App) commitBankRename() (tea.Model, tea.Cmd) {
	bankIdx := a.renameTarget.BankIdx
	if bankIdx >= a.containerInfo.BankCount {
		var ok bool
		a, ok = a.growBanksTo(bankIdx + 1)
		if !ok {
			a.renameActive = false
			a.renameBank = false
			a.renameBuffer = ""
			a.renameFresh = false
			return a, nil
		}
		a.layout.RefreshContainer(a.containerModel, a.containerInfo)
	}
	off := bankIdx*disk.SectorSize + disk.BankNameOffset
	data := a.containerModel.Bytes()
	if off+disk.VoiceNameFieldSize > len(data) {
		a.setStatus(status.Warning, "Rename bank: name field out of bounds")
		a.renameActive = false
		a.renameBank = false
		return a, nil
	}
	old := make([]byte, disk.VoiceNameFieldSize)
	copy(old, data[off:off+disk.VoiceNameFieldSize])
	padded := disk.PadLabel(a.renameBuffer)
	newBytes := make([]byte, disk.VoiceNameFieldSize)
	copy(newBytes, padded[:])
	if err := a.containerModel.Apply(model.Patch{
		Offset: off,
		Old:    old,
		New:    newBytes,
	}); err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Rename bank failed: %v", err))
		return a, nil
	}
	a.setStatus(status.Success,
		fmt.Sprintf("Renamed Bank %d to %q",
			bankIdx+1, strings.TrimRight(a.renameBuffer, " ")))
	a.renameActive = false
	a.renameBank = false
	a.renameBuffer = ""
	a.renameFresh = false
	return a, nil
}

func (a App) commitSaveAs() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(a.saveAsBuffer)
	if name == "" {
		a.setStatus(status.Warning, "Save-as: filename cannot be empty")
		return a, nil
	}
	if !strings.HasSuffix(strings.ToLower(name), ".img") &&
		!strings.HasSuffix(strings.ToLower(name), ".fzf") {
		name += ".img"
	}
	target := filepath.Join(a.directory, name)
	if _, err := os.Stat(target); err == nil {
		// Existing file: ask for confirmation.
		a.openConfirm(
			confirm.Prompt{
				Title: "Overwrite file?",
				Body:  fmt.Sprintf("%q already exists in this directory.", name),
				Options: []confirm.Option{
					{Label: cancelLabel, Result: 0},
					{Label: "Overwrite", Result: 1},
				},
			},
			func(result int) (App, tea.Cmd) {
				if result == 1 {
					return a.doSaveTo(target)
				}
				a.setStatus(status.Info, "Save-as cancelled")
				return a, nil
			})
		return a, nil
	}
	return a.doSaveTo(target)
}

func (a App) doSaveTo(target string) (App, tea.Cmd) {
	a = a.prepareForSave()
	if err := a.writeContainerToPath(target); err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Save-as failed: %v", err))
		return a, nil
	}
	a.containerInfo.Path = target
	// Re-classify the container by the new extension so subsequent
	// Ctrl-S goes through the right writer. A new-disk + save-as
	// "FOO.img" must flip the in-memory Format from FZF to IMG.
	switch strings.ToLower(filepath.Ext(target)) {
	case ".img":
		a.containerInfo.Format = loader.FormatIMG
		a.containerInfo.DiskEntryName = disk.FullDumpName
		a.containerInfo.WrappedVoice = false
	default:
		a.containerInfo.Format = loader.FormatFZF
	}
	a.saveAsActive = false
	a.saveAsBuffer = ""
	a.setStatus(status.Success, fmt.Sprintf("Saved %s", target))
	cmd := a.toast.Set("Saved!")
	return a, cmd
}

// writeContainerToPath dispatches save-as by file extension. A bare
// .fzf write is what containerModel.Save does today; .img wraps the
// FZF payload into a 1310720-byte FZ-1 floppy image (formatting one
// from scratch when the target doesn't exist) so the saved file is
// round-trippable through the loader. Other extensions fall through
// to the raw FZF write (the workspace browser won't list them, but
// scripted callers may name them anything).
func (a App) writeContainerToPath(target string) error {
	if strings.EqualFold(filepath.Ext(target), ".img") {
		return a.writeContainerAsImage(target)
	}
	return a.containerModel.Save(target)
}

// writeContainerAsImage wraps the in-memory FZF into an FZ-1 floppy
// image at target. When target doesn't exist we format a blank image
// first (diskformat.Format) and use diskadd.AddBytes to lay the FZF
// in as FULL-DATA-FZ. When target exists we route through the
// existing splice-in-place path (saveContainerToImage), which
// preserves anything else on the disk.
func (a App) writeContainerAsImage(target string) error {
	if _, err := os.Stat(target); err == nil {
		// Existing image: splice in place.
		return a.saveContainerToImage(target)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat target: %w", err)
	}
	label := sanitizeImageLabel(filepath.Base(target))
	if err := diskformat.Format(target, label); err != nil {
		return fmt.Errorf("format new image: %w", err)
	}
	payload := a.containerModel.Bytes()
	// An empty FZF (no voices in any bank) cannot be embedded as a
	// FULL-DATA-FZ entry: the FZ-1 firmware requires bstep>=1, and
	// the fzutil parser enforces that on reload. Skip AddBytes for
	// the empty case; the IMG is a valid blank floppy and the loader
	// returns a fresh editable container on reopen. As soon as the
	// user adds a voice and re-saves, AddBytes runs and a real
	// FULL-DATA-FZ lands in the directory.
	if a.containerInfo.VoiceCount > 0 {
		waveSectors := disk.SectorsNeeded(int(a.containerInfo.PCMBytes))
		name := disk.PadLabel(disk.FullDumpName)
		if err := diskadd.AddBytes(target, payload, name,
			disk.TypeFullDump, 0,
			a.containerInfo.BankCount,
			a.containerInfo.VoiceCount,
			waveSectors); err != nil {
			return fmt.Errorf("add FULL-DATA-FZ: %w", err)
		}
	}
	a.containerModel.ClearHistory()
	return nil
}

// sanitizeImageLabel turns a filename basename into a 12-char FZ-1
// disk label: uppercase, ASCII letters / digits / space / dash only,
// extension stripped, falling back to "FZ-DISK" when nothing usable
// survives. diskformat.Format rejects empty / non-printable-ASCII
// labels so we sanitise instead of letting the user's filename leak
// into the disk label field unchecked.
// defaultDiskLabel is the fallback disk label when a filename yields no
// usable FZ-name characters.
const defaultDiskLabel = "FZ-DISK"

func sanitizeImageLabel(basename string) string {
	stem := strings.TrimSuffix(basename, filepath.Ext(basename))
	var b strings.Builder
	for i := 0; i < len(stem) && b.Len() < disk.LabelSize; i++ {
		// Same FZ-name char rule as inline rename (upper-case, keep
		// A-Z/0-9/space/hyphen, drop the rest).
		if c := normaliseRenameKey(stem[i]); c != 0 {
			b.WriteByte(c)
		}
	}
	if b.Len() == 0 {
		return defaultDiskLabel
	}
	return b.String()
}

func (a App) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab", keyRight:
		a.confirm.Next()
	case "shift+tab", keyLeft:
		a.confirm.Prev()
	case keyEnter:
		a.confirm.Confirm()
		next, cmd := a.resolveConfirm()
		return next, cmd
	case keyEsc:
		a.confirm.Cancel()
		next, cmd := a.resolveConfirm()
		return next, cmd
	}
	return a, nil
}

func (a App) resolveConfirm() (App, tea.Cmd) {
	if a.pendingAction == nil {
		return a, nil
	}
	// The modal delivers exactly one value before closing the channel.
	// Receive synchronously because handleConfirmKey only calls Confirm
	// or Cancel which deliver to the buffered channel.
	result := <-a.confirmResult
	fn := a.pendingAction
	next, cmd := fn(result)
	next.confirmResult = nil
	next.pendingAction = nil
	return next, cmd
}

// openConfirm wires the confirm modal and stashes the callback the
// App's Update loop will invoke once a result lands. Pointer
// receiver: callers (typically value-receiver methods on App) have
// `a` as a local variable they return at the end; Go auto-
// addresses, and the field writes (confirmResult, pendingAction)
// land on that same local before it is returned. Switching to a
// value-and-return signature would force every callsite to
// `a = a.openConfirm(...)`, which the existing call sites consistently
// follow with `return a, nil` anyway.
func (a *App) openConfirm(p confirm.Prompt, onResult func(result int) (App, tea.Cmd)) {
	a.confirmResult = a.confirm.Show(p)
	a.pendingAction = onResult
}

// promptStereoChannelChoice opens a 3-option confirm modal so the
// user picks a channel for a stereo WAV import (left / right / mix).
// On result 0 / 1 / 2 the pool's AddWAV is re-run with the chosen
// channel. Cancel (-1) leaves the pool untouched and reports a
// status message.
func (a App) promptStereoChannelChoice(path string) App {
	a.openConfirm(
		confirm.Prompt{
			Title: "Stereo WAV",
			Body: fmt.Sprintf(
				"%q is stereo. Pick a channel for the pool entry:",
				filepath.Base(path)),
			Options: []confirm.Option{
				{Label: "Left", Result: 0},
				{Label: "Right", Result: 1},
				{Label: "Mix", Result: 2},
				{Label: cancelLabel, Result: -1},
			},
		},
		func(result int) (App, tea.Cmd) {
			if result < 0 {
				a.setStatus(status.Info, "Stereo import cancelled")
				return a, nil
			}
			if err := a.pool.AddWAV(path, result); err != nil {
				a.setStatus(status.Error, fmt.Sprintf("Add stereo WAV failed: %v", err))
				return a, nil
			}
			label := []string{"left", "right", "mix"}[result]
			a.setStatus(status.Success,
				fmt.Sprintf("Wrapped %s (%s channel) and added to pool",
					filepath.Base(path), label))
			return a, nil
		})
	return a
}

// runAutoSave writes a recovery snapshot of the dirty container as
// {workspace}/{base}.bak. One .bak per container: the previous
// snapshot is overwritten on each tick. A successful Save deletes
// the .bak; if the App crashes between autosave and Save, the next
// New(dir) detects the .bak as newer than its source and offers
// recovery. No-op when the container is clean, unset, or has no
// path (untitled, since autosave needs a base name).
func (a *App) runAutoSave() {
	if a.containerModel == nil || !a.containerModel.Dirty() {
		return
	}
	path := a.containerModel.Path()
	if path == "" {
		// Untitled container; nothing to autosave against. Save-as
		// would have to land first for autosave to gain a target.
		return
	}
	base := filepath.Base(path)
	if err := os.MkdirAll(a.backupDir, 0o755); err != nil {
		a.setStatus(status.Warning,
			fmt.Sprintf("Autosave: cannot create %s: %v", a.backupDir, err))
		return
	}
	bakPath := filepath.Join(a.backupDir, base+".bak")
	if err := os.WriteFile(bakPath, a.containerModel.Bytes(), 0o644); err != nil {
		a.setStatus(status.Warning,
			fmt.Sprintf("Autosave failed: %v", err))
		return
	}
	a.setStatus(status.Info, fmt.Sprintf("Autosaved %s", bakPath))
}

// clearAutoSaveBackup removes the .bak file (if any) corresponding
// to the named container. Called after a successful Save so the
// next launch doesn't offer recovery for a snapshot the user has
// already committed to disk.
func (a App) clearAutoSaveBackup(containerPath string) {
	if containerPath == "" {
		return
	}
	base := filepath.Base(containerPath)
	bakPath := filepath.Join(a.backupDir, base+".bak")
	_ = os.Remove(bakPath)
}

// findRecoveryCandidate returns the path of a .bak file in the
// workspace that is newer than its named container, or "" if no
// candidate exists. Used by IntentOpenContainer to offer
// recovery on launch.
func findRecoveryCandidate(workspaceDir, containerPath string) string {
	base := filepath.Base(containerPath)
	bakPath := filepath.Join(workspaceDir, base+".bak")
	bakInfo, err := os.Stat(bakPath)
	if err != nil {
		return ""
	}
	srcInfo, err := os.Stat(containerPath)
	if err != nil {
		// Container missing entirely; the bak IS the recovery.
		return bakPath
	}
	if bakInfo.ModTime().After(srcInfo.ModTime()) {
		return bakPath
	}
	return ""
}

// routeAction dispatches a navigation action to the App-level
// handler (universal actions) or to the focused space (everything
// else). Esc on a visible Error status acknowledges the error
// rather than falling through to the per-space Cancel handler, so
// the sticky-Error spec doesn't trap the user behind a message
// they cannot dismiss any other way.
func (a App) routeAction(action nav.Action) (tea.Model, tea.Cmd) {
	if action == nav.Cancel && a.status.HasMessage() && a.status.Severity() == status.Error {
		a.status.Cancel()
		return a, nil
	}
	switch action {
	case nav.Quit:
		return a.handleQuit()
	case nav.OpenHelp:
		a.help.Open()
		return a, nil
	case nav.Save:
		return a.handleSave()
	case nav.Undo:
		return a.handleUndo()
	case nav.Redo:
		return a.handleRedo()
	case nav.Audition:
		return a.handleAudition()
	case nav.NewDisk:
		return a.handleNewDisk()
	case nav.Export:
		// Export is space-scoped. Pool exports the focused entry;
		// Layout exports the focused Area's voice (full FZV via
		// voiceunpack); other spaces no-op so the `e` key stays
		// free for future per-space gestures.
		switch a.current {
		case minimap.Pool:
			return a.handleExport()
		case minimap.Layout:
			return a.forwardToSpace(action)
		case minimap.Workspace, minimap.Sound:
			// `e` is unbound in Workspace and Sound: no-op.
			return a, nil
		default:
			return a, nil
		}
	case nav.Refresh:
		return a.handleRefresh()
	case nav.Copy:
		return a.handleCopy()
	case nav.Paste:
		return a.handlePaste()
	case nav.SpaceUp:
		if a.pickingFor != nil {
			a.setStatus(status.Info, "Esc to cancel the import first")
			return a, nil
		}
		return a.navUp()
	case nav.SpaceDown:
		if a.pickingFor != nil {
			a.setStatus(status.Info, "Esc to cancel the import first")
			return a, nil
		}
		return a.navDown()
	case nav.NavNone:
		return a, nil
	case nav.NavUp, nav.NavDown, nav.NavLeft, nav.NavRight,
		nav.Rename, nav.Confirm, nav.Cancel,
		nav.Delete, nav.Duplicate, nav.Extract, nav.Import, nav.Move,
		nav.EditArea, nav.EditEffects:
		// Space-scoped actions: forward to the focused space.
		return a.forwardToSpace(action)
	default:
		// Future actions land here until they get an explicit case.
		return a.forwardToSpace(action)
	}
}

// handleCopy populates the App's typed clipboard from the focused
// Sound cell. No-op outside Sound (Copy is only meaningful for
// cell-scoped state) and no-op when Sound is bound to a voiceless
// slot. Reports the clipboard summary (e.g. "Clipboard: DCA
// envelope") on success so the user knows what they just grabbed.
func (a App) handleCopy() (tea.Model, tea.Cmd) {
	if a.current != minimap.Sound {
		return a, nil
	}
	msg := a.sound.Copy(&a.clipboard)
	if msg != "" {
		a.setStatus(status.Info, msg)
	}
	return a, nil
}

// handlePaste applies the App's clipboard to the focused Sound cell.
// No-op outside Sound. A type mismatch is non-destructive and emits
// the spec'd "Cannot paste X into Y" status; a no-payload clipboard
// reports "Clipboard is empty".
func (a App) handlePaste() (tea.Model, tea.Cmd) {
	if a.current != minimap.Sound {
		return a, nil
	}
	if a.clipboard.Kind() == sound.ClipboardKindNone {
		a.setStatus(status.Info, "Clipboard is empty")
		return a, nil
	}
	msg := a.sound.Paste(&a.clipboard)
	if msg != "" {
		a.setStatus(status.Info, msg)
	}
	return a, nil
}

// handleQuit emits tea.Quit immediately on a clean container; on a
// dirty one it opens a confirmation modal so the user doesn't lose
// edits to a stray Ctrl-Q. Cancel keeps the App running; Discard
// quits anyway.
func (a App) handleQuit() (tea.Model, tea.Cmd) {
	if a.containerModel == nil || !a.containerModel.Dirty() {
		return a, tea.Quit
	}
	a.openConfirm(
		confirm.Prompt{
			Title: unsavedChangesTitle,
			Body:  "Edits to this container haven't been saved.",
			Options: []confirm.Option{
				{Label: cancelLabel, Result: 0},
				{Label: "Quit anyway", Result: 1},
			},
		},
		func(result int) (App, tea.Cmd) {
			if result == 1 {
				return a, tea.Quit
			}
			a.setStatus(status.Info, "Quit cancelled")
			return a, nil
		})
	return a, nil
}

// handleExport writes the focused Pool entry to a file. Gated to
// Pool by the caller; Layout users go via `c` (copy to pool) then
// `e` (export from pool). A direct Layout export would duplicate the
// FZF voice-extract code path the pool already owns through
// MirrorContainerVoices.
func (a App) handleExport() (tea.Model, tea.Cmd) {
	path, err := a.pool.Export(a.workspace.Directory())
	if err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Export failed: %v", err))
		return a, nil
	}
	a.workspace.Refresh()
	a.setStatus(status.Success, fmt.Sprintf("Exported %s", path))
	return a, nil
}

// swapAreasInBank exchanges two Areas within the same bank: their
// vp[] entries plus per-Area metadata (key range, velocity range,
// key-cent, audio output, volume). The voice headers themselves
// don't move; only the bank's mapping from "Area index" to
// "voice slot" and the bank's per-Area parameters. All the byte
// patches land in one ApplyBatch so undo treats the swap as a
// single step.
func (a App) swapAreasInBank(bankIdx, srcArea, tgtArea int) App {
	if a.containerModel == nil {
		a.setStatus(status.Warning, "Swap: no container loaded")
		return a
	}
	if srcArea == tgtArea {
		return a // no-op, but reachable via tests
	}
	if bankIdx < 0 || bankIdx >= a.containerInfo.BankCount {
		a.setStatus(status.Warning, "Swap: bank not materialised")
		return a
	}
	data := a.containerModel.Bytes()
	base := bankIdx * disk.SectorSize
	if base+disk.SectorSize > len(data) {
		a.setStatus(status.Warning, "Swap: bank sector out of bounds")
		return a
	}
	patches := container.SwapAreaPatches(data, container.SwapAreaParams{
		Base: base, SrcArea: srcArea, TgtArea: tgtArea,
	})
	if err := a.containerModel.ApplyBatch(patches); err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Swap failed: %v", err))
		return a
	}
	a.setStatus(status.Success,
		fmt.Sprintf("Swapped Bank %d / A%02d <-> A%02d",
			bankIdx+1, srcArea+1, tgtArea+1))
	return a
}

// patchByte builds a single-byte Patch for the given absolute offset,
// reading the current byte from data to populate the Old field so
// the patch round-trips through undo/redo cleanly.
func patchByte(data []byte, off int, newVal uint8) model.Patch {
	return model.Patch{
		Offset: off,
		Old:    []byte{data[off]},
		New:    []byte{newVal},
	}
}

// openEffectsEditor seeds the per-bank effects modal from the
// current bank sector's effectdata block (24 bytes at BankEffectOffset
// = 0x3c0). Auto-grows the bank count if the target bank isn't
// materialised yet, the same way openAreaEditor does.
func (a App) openEffectsEditor(bankIdx int) App {
	if a.containerModel == nil {
		a.setStatus(status.Warning, "Edit effects: no container loaded")
		return a
	}
	if bankIdx >= a.containerInfo.BankCount {
		var ok bool
		a, ok = a.growBanksTo(bankIdx + 1)
		if !ok {
			return a
		}
	}
	var seed effectseditor.SeedValues
	data := a.containerModel.Bytes()
	base := bankIdx*disk.SectorSize + disk.BankEffectOffset
	for i := 0; i < len(seed.Cells); i++ {
		off := base + seed.OffsetAt(i)
		if off < len(data) {
			seed.Cells[i] = data[off]
		}
	}
	a.effectsEditor.Open(bankIdx, seed)
	a.setStatus(status.Info,
		"Edit effects: Tab cycles, arrows step, Shift+arrows ±10, Enter saves, Esc cancels")
	return a
}

// handleEffectsEditorKey routes keypresses to the modal.
func (a App) handleEffectsEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc:
		a.effectsEditor.Close()
		a.setStatus(status.Info, "Edit effects cancelled")
		return a, nil
	case keyEnter:
		return a.commitEffectsEditor()
	default:
		a.effectsEditor.HandleKey(msg.String())
		return a, nil
	}
}

// commitEffectsEditor patches the bank's effect block with whatever
// the modal holds. Skips writing when nothing changed.
func (a App) commitEffectsEditor() (tea.Model, tea.Cmd) {
	bankIdx := a.effectsEditor.BankIdx()
	if !a.effectsEditor.Changed() {
		a.effectsEditor.Close()
		a.setStatus(status.Info, "Edit effects: no changes")
		return a, nil
	}
	data := a.containerModel.Bytes()
	base := bankIdx*disk.SectorSize + disk.BankEffectOffset
	cells := a.effectsEditor.Cells()
	patches := make([]model.Patch, 0, len(cells))
	for i := 0; i < len(cells); i++ {
		off := base + a.effectsEditor.CellOffset(i)
		if off >= len(data) {
			continue
		}
		if data[off] == cells[i] {
			continue
		}
		patches = append(patches, patchByte(data, off, cells[i]))
	}
	if len(patches) == 0 {
		a.effectsEditor.Close()
		return a, nil
	}
	if err := a.containerModel.ApplyBatch(patches); err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Edit effects failed: %v", err))
		a.effectsEditor.Close()
		return a, nil
	}
	a.setStatus(status.Success,
		fmt.Sprintf("Saved effects for Bank %d", bankIdx+1))
	a.effectsEditor.Close()
	return a, nil
}

// confirmDeleteBank opens the confirmation modal for a delete-bank
// gesture. On confirm we zero the whole bank sector (bstep, vp[],
// per-area metadata, name, effect data, everything in that 1024
// bytes) so the bank reads as empty. Save-time compactEmptyBanks
// drops it from the file if it ends up as a trailing bank; middle
// banks stay materialised with bstep=0.
func (a App) confirmDeleteBank(bankIdx int) App {
	if bankIdx < 0 || bankIdx >= a.containerInfo.BankCount {
		return a
	}
	bankName := a.layout.BankName(bankIdx)
	body := fmt.Sprintf("Clear all area assignments, name, and effects on Bank %d?", bankIdx+1)
	if bankName != "" {
		body = fmt.Sprintf("Clear all area assignments, name, and effects on Bank %d (%q)?", bankIdx+1, bankName)
	}
	a.openConfirm(
		confirm.Prompt{
			Title:   fmt.Sprintf("Delete Bank %d", bankIdx+1),
			Body:    body,
			Options: []confirm.Option{{Label: cancelLabel, Result: 0}, {Label: "Delete", Result: 1}},
		},
		func(result int) (App, tea.Cmd) {
			if result != 1 {
				a.setStatus(status.Info, "Bank delete cancelled")
				return a, nil
			}
			return a.deleteBank(bankIdx), nil
		})
	return a
}

// deleteBank zeroes the bank's 1024-byte sector in one ApplyBatch
// patch.
func (a App) deleteBank(bankIdx int) App {
	data := a.containerModel.Bytes()
	base := bankIdx * disk.SectorSize
	if base+disk.SectorSize > len(data) {
		a.setStatus(status.Warning, "Delete bank: bank out of bounds")
		return a
	}
	old := make([]byte, disk.SectorSize)
	copy(old, data[base:base+disk.SectorSize])
	zeros := make([]byte, disk.SectorSize)
	if err := a.containerModel.Apply(model.Patch{
		Offset: base,
		Old:    old,
		New:    zeros,
	}); err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Delete bank failed: %v", err))
		return a
	}
	a.layout.RefreshContainer(a.containerModel, a.containerInfo)
	a.setStatus(status.Success, fmt.Sprintf("Cleared Bank %d", bankIdx+1))
	return a
}

// openAreaEditor seeds the modal from the current bank-sector
// values for the focused Area and opens it. Each per-Area field
// (key range, vel range, key-cent, audio out, volume) lives at a
// known offset in the bank sector. If the bank is unmaterialised,
// we open with sensible defaults; saving auto-grows as needed.
func (a App) openAreaEditor(bankIdx, areaIdx int) App {
	if a.containerModel == nil {
		a.setStatus(status.Warning, "Edit area: no container loaded")
		return a
	}
	seed := areaeditor.SeedValues{
		KeyLow:   0,
		KeyHigh:  127,
		KeyOrig:  60, // C4
		VelLow:   0,
		VelHigh:  127,
		Volume:   0,
		AudioOut: 0xff, // poly (matches studio1 default)
		MIDIChan: 0,    // channel 1 (1-indexed for display)
	}
	if bankIdx < a.containerInfo.BankCount {
		data := a.containerModel.Bytes()
		base := bankIdx * disk.SectorSize
		if base+disk.BankVolumeOffset+areaIdx < len(data) {
			seed.KeyLow = int(data[base+disk.BankKeyLowOffset+areaIdx])
			seed.KeyHigh = int(data[base+disk.BankKeyHighOffset+areaIdx])
			// Key Orig (FZ-1 spec `cent[]`) is the root key: a
			// MIDI note number 0..127 indicating the pitch at which
			// the sample plays at its natural speed.
			seed.KeyOrig = int(data[base+disk.BankKeyCentOffset+areaIdx])
			seed.VelLow = int(data[base+disk.BankVelLowOffset+areaIdx])
			seed.VelHigh = int(data[base+disk.BankVelHighOffset+areaIdx])
			seed.Volume = int(data[base+disk.BankVolumeOffset+areaIdx])
			seed.AudioOut = int(data[base+disk.BankAudioOutOffset+areaIdx])
			seed.MIDIChan = int(data[base+disk.BankMIDIRecvChanOffset+areaIdx])
		}
	}
	a.areaEditor.Open(bankIdx, areaIdx, seed)
	a.setStatus(status.Info,
		"Edit area: Tab cycles, arrows step, Shift+arrows big step, Enter saves, Esc cancels")
	return a
}

// handleAreaEditorKey routes keypresses to the modal. Enter commits,
// Esc cancels; everything else goes to the modal's stepper.
func (a App) handleAreaEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc:
		a.areaEditor.Close()
		a.setStatus(status.Info, "Edit area cancelled")
		return a, nil
	case keyEnter:
		return a.commitAreaEditor()
	default:
		a.areaEditor.HandleKey(msg.String())
		return a, nil
	}
}

// commitAreaEditor patches the bank sector's per-Area key range
// fields with whatever the modal currently holds. Skips the patch
// when nothing changed. Auto-grows BankCount through growBanksTo if
// the target bank wasn't materialised yet.
func (a App) commitAreaEditor() (tea.Model, tea.Cmd) {
	bankIdx := a.areaEditor.BankIdx()
	areaIdx := a.areaEditor.AreaIdx()
	if !a.areaEditor.Changed() {
		a.areaEditor.Close()
		a.setStatus(status.Info, "Edit area: no changes")
		return a, nil
	}
	if bankIdx >= a.containerInfo.BankCount {
		var ok bool
		a, ok = a.growBanksTo(bankIdx + 1)
		if !ok {
			a.areaEditor.Close()
			return a, nil
		}
	}
	data := a.containerModel.Bytes()
	base := bankIdx * disk.SectorSize
	if base+disk.BankVolumeOffset+areaIdx >= len(data) {
		a.setStatus(status.Warning, "Edit area: bank sector out of bounds")
		a.areaEditor.Close()
		return a, nil
	}
	// Build a patch per per-Area byte field. Key-cent is signed; the
	// disk stores it as a byte (int8 round-trip).
	patches := []model.Patch{
		patchByte(data, base+disk.BankKeyLowOffset+areaIdx, uint8(a.areaEditor.KeyLow())),         //nolint:gosec // G115: KeyLow clamped to [0,127] by area editor.
		patchByte(data, base+disk.BankKeyHighOffset+areaIdx, uint8(a.areaEditor.KeyHigh())),       //nolint:gosec // G115: KeyHigh clamped to [0,127] by area editor.
		patchByte(data, base+disk.BankVelLowOffset+areaIdx, uint8(a.areaEditor.VelLow())),         //nolint:gosec // G115: VelLow clamped to [0,127] by area editor.
		patchByte(data, base+disk.BankVelHighOffset+areaIdx, uint8(a.areaEditor.VelHigh())),       //nolint:gosec // G115: VelHigh clamped to [0,127] by area editor.
		patchByte(data, base+disk.BankKeyCentOffset+areaIdx, uint8(a.areaEditor.KeyOrig())),       //nolint:gosec // G115: KeyOrig clamped to [0,127] by area editor.
		patchByte(data, base+disk.BankAudioOutOffset+areaIdx, uint8(a.areaEditor.AudioOut())),     //nolint:gosec // G115: AudioOut clamped to [0,255] by area editor.
		patchByte(data, base+disk.BankVolumeOffset+areaIdx, uint8(a.areaEditor.Volume())),         //nolint:gosec // G115: Volume clamped to [0,127] by area editor.
		patchByte(data, base+disk.BankMIDIRecvChanOffset+areaIdx, uint8(a.areaEditor.MIDIChan())), //nolint:gosec // G115: MIDIChan clamped to [0,15] by area editor.
	}
	if err := a.containerModel.ApplyBatch(patches); err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Edit area failed: %v", err))
		a.areaEditor.Close()
		return a, nil
	}
	a.setStatus(status.Success,
		fmt.Sprintf("Bank %d / A%02d key range: %d..%d",
			bankIdx+1, areaIdx+1, a.areaEditor.KeyLow(), a.areaEditor.KeyHigh()))
	a.areaEditor.Close()
	return a, nil
}

// exportAreaToWorkspace writes the focused Area's voice (full FZV
// with audio data) to <workspace>/<voice-name>.fzv. Walks the bank's
// vp[] table via voiceunpack so the audio pointers in the written
// FZV are 0-relative (the format other FZ tools expect for a
// standalone voice). Errors surface as a status message; the
// container itself is untouched.
func (a App) exportAreaToWorkspace(bankIdx, areaIdx int) App {
	if a.containerModel == nil {
		a.setStatus(status.Warning, "Export: no container loaded")
		return a
	}
	data := a.containerModel.Bytes()
	slot, ok := disk.BankVPLookup(data, bankIdx, areaIdx)
	if !ok {
		a.setStatus(status.Warning, "Export: vp[] lookup out of bounds")
		return a
	}
	voices, slotIndices, err := voiceunpack.UnpackDataFromBytes(data)
	if err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Export failed: %v", err))
		return a
	}
	// Find the FZV whose origin slot matches our target.
	var fzv []byte
	for i, s := range slotIndices {
		if s == slot {
			fzv = voices[i]
			break
		}
	}
	if fzv == nil {
		a.setStatus(status.Warning, "Export: voice slot is empty")
		return a
	}
	name := a.layout.VoiceName(bankIdx, areaIdx)
	stem := sanitizeFZVStemForExport(name)
	if stem == "" {
		stem = fmt.Sprintf("BANK%d-AREA%02d", bankIdx+1, areaIdx+1)
	}
	target := filepath.Join(a.workspace.Directory(), stem+".fzv")
	if err := os.WriteFile(target, fzv, 0o644); err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Export failed: %v", err))
		return a
	}
	a.workspace.Refresh()
	a.setStatus(status.Success, fmt.Sprintf("Exported %s", target))
	return a
}

// sanitizeFZVStemForExport mirrors pool.sanitizeFZVStem inside the
// App so we don't expose pool's helper. ASCII letters / digits / dash
// / underscore survive; spaces become underscores; everything else
// is dropped. Trailing underscores trimmed.
func sanitizeFZVStemForExport(name string) string {
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

// handleRefresh re-reads external state in whichever space is focused.
// Today only Workspace has external state worth refreshing (the file
// listing). Other spaces no-op with an info message so the keystroke
// isn't silently dropped.
func (a App) handleRefresh() (tea.Model, tea.Cmd) {
	if a.current == minimap.Workspace {
		a.workspace.Refresh()
		a.setStatus(status.Success, "Workspace re-scanned")
		return a, nil
	}
	a.setStatus(status.Info, "Refresh only applies to the Workspace")
	return a, nil
}

// growBanksTo materialises bank sectors up to (and including)
// targetCount-1, returning an updated container model and info. The
// bank list view always shows all 8 banks, but only the first
// BankCount sectors actually live in the on-disk buffer; this helper
// runs lazily when an assignment lands on a previously-unmaterialised
// bank. Returns (App, true) on success, (App, false) when growth is
// refused (WrappedVoice .img, multi-disk first half, past MaxBanks).
// The caller sets the appropriate status on refusal.
//
// Voice slot indices and audio sample pointers stay valid because
// vp[] entries are voice-area-relative and wave pointers are
// audio-area-relative; only file offsets shift.
func (a App) growBanksTo(targetCount int) (App, bool) {
	if targetCount <= a.containerInfo.BankCount {
		return a, true
	}
	if a.containerModel == nil {
		a.setStatus(status.Warning, "No container to grow")
		return a, false
	}
	if a.containerInfo.WrappedVoice {
		a.setStatus(status.Info,
			"This .img holds a single Voice file, not a Full Dump; banks don't apply")
		return a, false
	}
	if targetCount > disk.MaxBanks {
		a.setStatus(status.Info,
			fmt.Sprintf("Cannot exceed %d banks", disk.MaxBanks))
		return a, false
	}
	data := a.containerModel.Bytes()
	if fzutil.IsMultiDiskFirstHalf(data) {
		a.setStatus(status.Warning,
			"Refusing to grow: this looks like disk 1 of a 2-disk full dump; "+
				"the bank count is shared with disk 2")
		return a, false
	}

	insertAt := a.containerInfo.BankCount * disk.SectorSize
	if insertAt > len(data) {
		a.setStatus(status.Error, "Grow: bank insertion point past buffer end")
		return a, false
	}
	newData, growBytes := container.GrowBanks(data, a.containerInfo.BankCount, targetCount)
	a.containerModel.Replace(newData)

	a.containerInfo.BankCount = targetCount
	a.containerInfo.AudioAreaStart += growBytes
	a.containerInfo.TotalBytes = int64(len(newData))
	if a.containerInfo.Header != nil {
		a.containerInfo.Header.NBankSectors = a.containerInfo.BankCount
		a.containerInfo.Header.VoiceAreaStart += growBytes
	}
	return a, true
}

// handleNewDisk swaps the in-flight container for a fresh untitled
// FZF (8 empty banks, no voices, no audio) and drops the user into
// Layout. Dirty edits prompt for confirmation first; the pool's
// imports survive (we only flush bank-mirrored entries via
// MirrorContainerVoices(nil)) so a user can stage voices, hit `n`,
// and assign them into a clean canvas.
func (a App) handleNewDisk() (tea.Model, tea.Cmd) {
	if a.containerModel != nil && a.containerModel.Dirty() {
		a.openConfirm(
			confirm.Prompt{
				Title: unsavedChangesTitle,
				Body:  "Discard edits and start a new disk?",
				Options: []confirm.Option{
					{Label: cancelLabel, Result: 0},
					{Label: "Discard and start fresh", Result: 1},
				},
			},
			func(result int) (App, tea.Cmd) {
				if result == 1 {
					return a.installUntitledContainer(), nil
				}
				a.setStatus(status.Info, "New disk cancelled")
				return a, nil
			})
		return a, nil
	}
	return a.installUntitledContainer(), nil
}

// installUntitledContainer replaces the container with a fresh
// NewUntitled, resets dependent space state, and lands the user in
// Layout on bank A.
func (a App) installUntitledContainer() App {
	m, info := loader.NewUntitled()
	a.containerModel = m
	a.containerInfo = info
	a.layout.SetContainer(m, info)
	a.sound.Unbind()
	a.pool.MirrorContainerVoices(nil)
	a.current = minimap.Layout
	a.minimap.Current = a.current
	a.setStatus(status.Success, "New disk: untitled (Ctrl-S to name and save)")
	return a
}

func (a App) handleSave() (tea.Model, tea.Cmd) {
	if a.containerModel == nil {
		a.setStatus(status.Warning, "No container to save")
		return a, nil
	}
	if a.containerModel.Path() == "" {
		a.saveAsActive = true
		a.saveAsBuffer = ""
		a.setStatus(status.Info, "Save-as: type filename, Enter to commit, Esc to cancel")
		return a, nil
	}
	a = a.prepareForSave()
	if err := a.persistContainer(a.containerModel.Path()); err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Save failed: %v", err))
		return a, nil
	}
	// Save committed; remove the autosave snapshot so the next launch
	// doesn't offer recovery against bytes the user already wrote.
	a.clearAutoSaveBackup(a.containerModel.Path())
	a.setStatus(status.Success, fmt.Sprintf("Saved %s", a.containerModel.Path()))
	cmd := a.toast.Set("Saved!")
	return a, cmd
}

// persistContainer writes the in-memory container back to its source
// file, dispatching by Format. The model's bytes are the FZF payload
// only. For an .img container we have to splice that payload back
// into the .img's FULL-DATA-FZ slot via diskadd.ReplaceInMemory, not
// overwrite the .img with the FZF (which would shrink the file from
// 1.25 MB to ~1019 KB and corrupt the disk structure).
func (a App) persistContainer(path string) error {
	switch a.containerInfo.Format {
	case loader.FormatIMG:
		return a.saveContainerToImage(path)
	case loader.FormatFZF, loader.FormatUnknown:
		// FZF saves the model bytes verbatim; FormatUnknown (untitled
		// containers without a detected format) takes the same path
		// since the in-memory bytes are FZF-shaped.
		return a.containerModel.Save(path)
	default:
		return a.containerModel.Save(path)
	}
}

// prepareForSave chains the save-time passes that turn the live
// in-memory buffer into a real-hardware-compatible file:
//
//  1. preserveRenamedBanks: materialises a shared NoSound voice
//     slot so empty-but-renamed banks survive the round-trip
//     through CountBankSectors on reload.
//  2. compactVoiceArea: drops trailing orphan voice slots no
//     bank's vp[] references AND truncates the audio area past
//     the last live sample, for file-size hygiene.
//  3. compactEmptyBanks: drops trailing empty bank sectors so
//     the next reload sees the right bank count.
//
// Per-area deletion is handled at the point of the delete: vp[]
// collapses and bstep decrements in deleteArea, so the file never
// carries a sentinel that real firmware can't load.
func (a App) prepareForSave() App {
	if a.containerModel == nil {
		return a
	}
	a = a.preserveRenamedBanks()
	a = a.compactVoiceArea()
	a = a.compactEmptyBanks()
	return a
}

// preserveRenamedBanks finds banks with bstep=0 but a non-default
// (i.e. user-renamed) name field and gives them a synthetic
// presence: a shared NoSound voice slot is appended (or reused if
// one already exists at the very end of the voice area) and the
// bank's bstep is set to 1 with vp[0] pointing at the synthetic
// slot. The synthetic slot is silent on real hardware, so the bank
// has no audible effect, but the file's CountBankSectors walk now
// includes it on reload, preserving the rename.
func (a App) preserveRenamedBanks() App {
	data := a.containerModel.Bytes()
	bankCount := a.containerInfo.BankCount
	voiceAreaStart := bankCount * disk.SectorSize
	if voiceAreaStart > len(data) {
		return a
	}

	// Pass 1: identify renamed-but-empty banks.
	var renamed []int
	for b := 0; b < bankCount; b++ {
		base := b * disk.SectorSize
		bstepOff := base + disk.BankVoiceCountOffset
		if bstepOff+2 > len(data) {
			break
		}
		bstep := int(binary.LittleEndian.Uint16(data[bstepOff : bstepOff+2]))
		if bstep != 0 {
			continue
		}
		if !bankHasUserName(data, base) {
			continue
		}
		renamed = append(renamed, b)
	}
	if len(renamed) == 0 {
		return a
	}

	// Pass 2: find or append a NoSound slot in the voice area.
	audioStart := a.containerInfo.AudioAreaStart
	if audioStart <= voiceAreaStart || audioStart > len(data) {
		return a
	}
	voiceAreaBytes := audioStart - voiceAreaStart
	currentSlots := voiceAreaBytes / disk.VoicePackSize * disk.VoicesPerSector
	// Scan all current slots for an existing NoSound (placeholder)
	// we can reuse; that saves the 256-byte cost when one already exists.
	noSoundSlot := -1
	for slot := 0; slot < currentSlots; slot++ {
		off := disk.VoiceSlotOffset(voiceAreaStart, slot)
		if off+disk.VoiceHeaderUsed > len(data) {
			break
		}
		mode := binary.LittleEndian.Uint16(data[off+disk.VoiceLoopModeOffset:])
		if mode == disk.PlaybackModeNoSound {
			noSoundSlot = slot
			break
		}
	}

	if noSoundSlot < 0 {
		// Append a fresh voice sector hosting one NoSound slot at its
		// first position. We grow the buffer by a full SectorSize since
		// VoiceSlotOffset packs four slots per sector.
		newData := make([]byte, len(data)+disk.SectorSize)
		copy(newData[:audioStart], data[:audioStart])
		// Zero-filled new sector is already a NoSound slot at position 0
		// (mode=0 = PlaybackModeNoSound; wave/gen pointers zero; envelope
		// sustain offsets zero are valid). Audio area shifts forward.
		copy(newData[audioStart+disk.SectorSize:], data[audioStart:])
		a.containerModel.Replace(newData)
		a.containerInfo.AudioAreaStart = audioStart + disk.SectorSize
		a.containerInfo.TotalBytes = int64(len(newData))
		if a.containerInfo.Header != nil {
			a.containerInfo.Header.VoiceAreaStart = a.containerInfo.AudioAreaStart
		}
		noSoundSlot = currentSlots
		data = a.containerModel.Bytes()
	}

	// Pass 3: write bstep=1 and vp[0]=noSoundSlot into each renamed bank.
	patches := []model.Patch{}
	var stepBuf [2]byte
	binary.LittleEndian.PutUint16(stepBuf[:], 1)
	var slotBuf [2]byte
	binary.LittleEndian.PutUint16(slotBuf[:], uint16(noSoundSlot))
	for _, b := range renamed {
		base := b * disk.SectorSize
		bstepOff := base + disk.BankVoiceCountOffset
		vpOff := base + disk.BankVoiceNumOffset
		oldStep := make([]byte, 2)
		copy(oldStep, data[bstepOff:bstepOff+2])
		oldVP := make([]byte, 2)
		copy(oldVP, data[vpOff:vpOff+2])
		patches = append(patches,
			model.Patch{Offset: bstepOff, Old: oldStep, New: append([]byte(nil), stepBuf[:]...)},
			model.Patch{Offset: vpOff, Old: oldVP, New: append([]byte(nil), slotBuf[:]...)},
		)
	}
	if err := a.containerModel.ApplyBatch(patches); err != nil {
		a.setStatus(status.Warning, fmt.Sprintf("Rename preserve skipped: %v", err))
	}
	a.layout.RefreshContainer(a.containerModel, a.containerInfo)
	return a
}

// bankHasUserName reports whether the bank's name field carries any
// non-space, non-zero byte (i.e. the user assigned a name). Default
// FZ-1 bank names are 14 spaces; new-disk creation may leave them as
// 14 zeros; both count as "not renamed".
func bankHasUserName(data []byte, base int) bool {
	start := base + disk.BankNameOffset
	end := start + disk.VoiceNameFieldSize
	if end > len(data) {
		return false
	}
	for _, b := range data[start:end] {
		if b != ' ' && b != 0 {
			return true
		}
	}
	return false
}

// compactVoiceArea drops trailing orphan voice slots (slots not
// referenced by any bank's vp[]) and truncates the audio area to
// the last live sample address. Mid-array orphans aren't moved;
// shifting them would renumber vp[] entries across all banks, a
// more invasive operation we don't attempt here. Trailing-only
// compaction handles the most common "delete the last assignment"
// case, which is what bites file-size hygiene most.
func (a App) compactVoiceArea() App {
	newData, newAudioStart, changed := container.CompactVoiceArea(
		a.containerModel.Bytes(), a.containerInfo.BankCount, a.containerInfo.AudioAreaStart)
	if !changed {
		return a
	}
	a.containerModel.Replace(newData)
	a.containerInfo.AudioAreaStart = newAudioStart
	a.containerInfo.TotalBytes = int64(len(newData))
	if a.containerInfo.Header != nil {
		a.containerInfo.Header.VoiceAreaStart = newAudioStart
	}
	a.layout.RefreshContainer(a.containerModel, a.containerInfo)
	return a
}

// compactEmptyBanks removes every bank sector with bstep=0 from the
// in-memory buffer (both trailing and middle gaps). studio's "all 8
// banks visible" UX materialises empty bank sectors on rename or
// out-of-order assignment; without this compaction the saved file's
// CountBankSectors would stop at the first empty bank on reload, so
// later non-empty banks would either vanish (trailing case) or be
// silently re-interpreted as voice area (middle-gap case).
//
// Voice slot indices stay valid across the compaction: vp[] entries
// reference absolute voice-area positions, and the voice area's
// content is unchanged; only its file offset shifts. Wave/gen/loop
// pointers reference samples relative to the audio area's start, so
// they survive the audio-area shift too.
//
// Note: renames of unassigned banks are destructive ("rename Bank 5
// without assigning any voices" loses the rename on save). The
// rename was speculative; without content, the bank doesn't make
// it to disk. Reassigning compacts middle gaps in deterministic
// keep-in-order fashion: a (Bank 0, Bank 5 used / Banks 1-4 empty)
// in-memory state saves as a 2-bank file where the formerly-Bank-5
// becomes Bank 2.
func (a App) compactEmptyBanks() App {
	if a.containerModel == nil {
		return a
	}
	newData, newBankCount, newAudioStart, changed := container.CompactEmptyBanks(
		a.containerModel.Bytes(), a.containerInfo.BankCount, a.containerInfo.AudioAreaStart)
	if !changed {
		return a
	}
	droppedBytes := (a.containerInfo.BankCount - newBankCount) * disk.SectorSize
	a.containerModel.Replace(newData)
	a.containerInfo.BankCount = newBankCount
	a.containerInfo.AudioAreaStart = newAudioStart
	a.containerInfo.TotalBytes = int64(len(newData))
	if a.containerInfo.Header != nil {
		a.containerInfo.Header.NBankSectors = newBankCount
		a.containerInfo.Header.VoiceAreaStart -= droppedBytes
	}
	a.layout.RefreshContainer(a.containerModel, a.containerInfo)
	return a
}

func (a App) saveContainerToImage(path string) error {
	img, err := disk.OpenImage(path)
	if err != nil {
		return fmt.Errorf("open image: %w", err)
	}
	name := a.containerInfo.DiskEntryName
	if name == "" {
		name = disk.FullDumpName
	}
	payload := a.containerModel.Bytes()
	if a.containerInfo.WrappedVoice {
		// Synthetic single-voice FZF: drop the bank sector to recover
		// the FZV layout the .img expects. The voice header's wave /
		// gen pointers stayed 0-relative to the FZV's audio area
		// during editing, so the bytes after the leading bank sector
		// are already a valid FZV payload.
		if len(payload) < disk.SectorSize {
			return fmt.Errorf("save: wrapped voice too small (%d bytes)", len(payload))
		}
		payload = payload[disk.SectorSize:]
	}
	if err := diskadd.ReplaceInMemory(img, name, payload, 0); err != nil {
		return fmt.Errorf("replace %s: %w", name, err)
	}
	if err := fileutil.WriteAtomic(path, img.Bytes()); err != nil {
		return fmt.Errorf("write image: %w", err)
	}
	a.containerModel.ClearHistory()
	return nil
}

func (a App) handleAudition() (tea.Model, tea.Cmd) {
	if a.containerModel == nil {
		return a, nil
	}
	// When focused on Workspace and the cursor is on a .wav, audition
	// the file directly (no Area selection required). This short-
	// circuits the usual Layout / Sound code paths.
	if a.current == minimap.Workspace {
		if cmdOut, handled := a.handleWorkspaceWavAudition(); handled {
			return cmdOut, nil
		}
	}
	bankIdx, areaIdx, ok := a.layout.SelectedArea()
	if !ok {
		a.setStatus(status.Info, "Audition: select an Area in Layout first")
		return a, nil
	}
	data := a.containerModel.Bytes()
	if fzutil.IsMultiDiskFirstHalf(data) {
		a.setStatus(status.Warning,
			"Audition unavailable: this is part of a 2-disk full dump; "+
				"voice audio is split across both disks")
		return a, nil
	}
	voiceAreaStart := a.containerInfo.BankCount * disk.SectorSize
	// Refuse audition when the Area is outside the bank's bstep:
	// without this guard, vp[areaIdx] reads as 0, slot 0 holds Bank
	// 1's first voice (AMEN 01 on JUNGLE), and pressing audition on
	// an "(empty)" row resurrects that voice. Mirrors the same check
	// areaSummary uses for rendering.
	bankBase := bankIdx * disk.SectorSize
	if bankBase+disk.BankVoiceCountOffset+2 > len(data) {
		a.setStatus(status.Warning, "Audition: bank out of bounds")
		return a, nil
	}
	bstep := int(binary.LittleEndian.Uint16(
		data[bankBase+disk.BankVoiceCountOffset : bankBase+disk.BankVoiceCountOffset+2]))
	if areaIdx >= bstep {
		a.setStatus(status.Info, "Audition: this Area is empty")
		return a, nil
	}
	slotIdx, ok := disk.BankVPLookup(data, bankIdx, areaIdx)
	if !ok {
		a.setStatus(status.Warning, "Audition: vp[] lookup out of bounds")
		return a, nil
	}
	voiceOff := disk.VoiceSlotOffset(voiceAreaStart, slotIdx)
	if voiceOff+disk.VoicePackSize > len(data) {
		a.setStatus(status.Warning, "Audition: voice slot out of bounds")
		return a, nil
	}
	voiceBytes := data[voiceOff : voiceOff+disk.VoicePackSize]
	// Also refuse when the resolved slot is a NoSound placeholder
	// (mode==0 at offset 0x10). Catches the case where vp[] points
	// into an empty/zeroed voice slot.
	if binary.LittleEndian.Uint16(voiceBytes[disk.VoiceLoopModeOffset:]) == disk.PlaybackModeNoSound {
		a.setStatus(status.Info, "Audition: this Area is empty")
		return a, nil
	}
	voiceID := audio.VoiceID(fmt.Sprintf("%s:b%d:a%d", a.containerInfo.Path, bankIdx, areaIdx))
	pitch := int(voiceBytes[disk.VoiceKeyCentOffset])
	audioArea, err := a.containerAudioArea()
	if err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Audition failed: %v", err))
		return a, nil
	}
	if err := audio.Audition(voiceID, voiceBytes, audioArea, pitch); err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Audition failed: %v", err))
		return a, nil
	}
	if audio.CurrentVoiceID() == voiceID {
		a.setStatus(status.Success, fmt.Sprintf("Auditioning Bank %d / Area %d", bankIdx+1, areaIdx+1))
	} else {
		a.setStatus(status.Info, "Audition stopped")
	}
	return a, nil
}

// containerAudioArea returns the slice of containerModel.Bytes() that
// the in-focus voice header's wave / gen pointers index into. Reads
// the cached AudioAreaStart from ContainerInfo (set by the loader for
// FZF/IMG and by NewUntitled for the in-memory case) so this works
// uniformly across the three container origins.
func (a App) containerAudioArea() ([]byte, error) {
	if a.containerModel == nil {
		return nil, errors.New("no container loaded")
	}
	data := a.containerModel.Bytes()
	audioStart := a.containerInfo.AudioAreaStart
	if audioStart <= 0 || audioStart > len(data) {
		return nil, fmt.Errorf("container audio area out of range: start=%d size=%d",
			audioStart, len(data))
	}
	return data[audioStart:], nil
}

func (a App) handleUndo() (tea.Model, tea.Cmd) {
	if a.containerModel == nil {
		return a, nil
	}
	if err := a.containerModel.Undo(); err != nil {
		if errors.Is(err, model.ErrNothingToUndo) {
			a.setStatus(status.Info, "Nothing to undo")
		} else {
			a.setStatus(status.Error, fmt.Sprintf("Undo failed: %v", err))
		}
		return a, nil
	}
	a.setStatus(status.Info, "Undo")
	return a, nil
}

func (a App) handleRedo() (tea.Model, tea.Cmd) {
	if a.containerModel == nil {
		return a, nil
	}
	if err := a.containerModel.Redo(); err != nil {
		if errors.Is(err, model.ErrNothingToRedo) {
			a.setStatus(status.Info, "Nothing to redo")
		} else {
			a.setStatus(status.Error, fmt.Sprintf("Redo failed: %v", err))
		}
		return a, nil
	}
	a.setStatus(status.Info, "Redo")
	return a, nil
}

func (a App) navUp() (tea.Model, tea.Cmd) {
	if a.current > minimap.Workspace {
		a.current--
		a.minimap.Current = a.current
		if a.current != minimap.Sound {
			a.sound.Unbind()
		}
	}
	return a, nil
}

func (a App) navDown() (tea.Model, tea.Cmd) {
	switch a.current {
	case minimap.Workspace:
		a.current = minimap.Pool
		a.minimap.Current = a.current
	case minimap.Pool:
		a.current = minimap.Layout
		a.minimap.Current = a.current
	case minimap.Layout:
		bankIdx, areaIdx, ok := a.layout.SelectedArea()
		if !ok {
			a.setStatus(status.Info, "Select an Area in Layout first (Enter on a bank, then on an Area)")
			return a, nil
		}
		voiceArea := a.containerInfo.BankCount * 1024
		audioArea := a.audioAreaStart()
		a.sound.Bind(a.containerModel, a.containerInfo.BankCount, voiceArea, audioArea, bankIdx, areaIdx)
		a.current = minimap.Sound
		a.minimap.Current = a.current
	case minimap.Sound:
		// Sound is the deepest space; no further navDown.
	default:
		// Future spaces land here until they get an explicit case.
	}
	return a, nil
}

// handleWorkspaceWavAudition auditions the file currently highlighted
// in the Workspace's file browser when it is a .wav. Reads, wraps via
// voiceimport.Encode, and plays through the audio engine the same way
// the pool's audition path does. Returns (a, true) on success or a
// reported error; (a, false) when the highlighted entry isn't a .wav
// so the caller can fall back to the Layout-area audition path.
func (a App) handleWorkspaceWavAudition() (App, bool) {
	path := a.workspace.HighlightedPath()
	if path == "" || !strings.EqualFold(filepath.Ext(path), ".wav") {
		return a, false
	}
	f, err := fzutil.ReadWAV(path)
	if err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Audition WAV failed: %v", err))
		return a, true
	}
	if len(f.Samples) == 0 {
		a.setStatus(status.Warning, "Audition: WAV has no samples")
		return a, true
	}
	// Resample to a supported FZ rate so the audition plays at the
	// source's actual pitch. Without this, a 44.1 kHz WAV would be
	// labelled as 36 kHz and play ~3 semitones flat (the engine reads
	// it back at the FZ rate it was told).
	targetRate := pool.ChooseFZRate(f.SampleRate)
	samples, err := fzutil.Resample(f, targetRate)
	if err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Audition WAV resample: %v", err))
		return a, true
	}
	rateIdx, ok := disk.RateIndexFor(targetRate)
	if !ok {
		rateIdx = 0
	}
	name := fzutil.VoiceName(path)
	bytes := voiceimport.Encode(samples, rateIdx, name, 0, voiceimport.NoLoop())
	if len(bytes) < disk.SectorSize {
		a.setStatus(status.Warning, "Audition: encoded voice too small")
		return a, true
	}
	hdr := bytes[:disk.VoicePackSize]
	audioArea := bytes[disk.SectorSize:]
	pitch := int(hdr[disk.VoiceKeyCentOffset])
	voiceID := audio.VoiceID("wav:" + path)
	if err := audio.Audition(voiceID, hdr, audioArea, pitch); err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Audition failed: %v", err))
		return a, true
	}
	if audio.CurrentVoiceID() == voiceID {
		a.setStatus(status.Success, fmt.Sprintf("Auditioning %s", filepath.Base(path)))
	} else {
		a.setStatus(status.Info, "Audition stopped")
	}
	return a, true
}

// doOpenContainer loads + installs the container at path, then
// offers a recovery prompt if a newer .bak snapshot sits next to
// the file. Used by both the clean-state IntentOpenContainer
// branch and the post-confirm switch-while-dirty branch. Both
// paths share the recovery offer, so a user who switched away
// from unsaved edits still gets to recover the new file's prior
// crash snapshot if one exists.
func (a App) doOpenContainer(path string) App {
	a, ok := a.loadAndInstallContainer(path)
	if !ok {
		return a
	}
	return a.offerRecoveryIfNewer(path)
}

// loadAndInstallContainer reads the container at path via the
// production loader and swaps it into the App's state. Returns
// ok=false when the load fails (status message already set).
// Does not check for a recovery .bak; see offerRecoveryIfNewer.
func (a App) loadAndInstallContainer(path string) (App, bool) {
	m, info, err := loader.LoadContainer(path)
	if err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Open failed: %v", err))
		return a, false
	}
	a.containerModel = m
	a.containerInfo = info
	a.layout.SetContainer(m, info)
	a.sound.Unbind()
	// Pool is intentionally NOT touched on disk-open: it accumulates
	// across disks so users can stage voices in the pool, swap
	// disks, and drop the same voices into different containers.
	a.current = minimap.Layout
	a.minimap.Current = a.current
	a.setStatus(status.Success,
		fmt.Sprintf("Opened %s (%d banks, %d voices)",
			path, info.BankCount, info.VoiceCount))
	return a, true
}

// offerRecoveryIfNewer opens a confirm modal when an autosave .bak
// for path exists with a mod-time newer than path itself. The
// confirm offers Recover (load the snapshot, mark dirty) or
// Discard (delete the .bak so the prompt doesn't reappear).
// No-op when no candidate exists.
func (a App) offerRecoveryIfNewer(path string) App {
	bakPath := findRecoveryCandidate(a.backupDir, path)
	if bakPath == "" {
		return a
	}
	a.openConfirm(
		confirm.Prompt{
			Title: "Recover unsaved changes",
			Body: fmt.Sprintf(
				"A newer autosave snapshot exists at %s. Load it?",
				filepath.Base(bakPath)),
			Options: []confirm.Option{
				{Label: "Recover", Result: 1},
				{Label: "Discard", Result: 0},
			},
		},
		func(result int) (App, tea.Cmd) {
			if result != 1 {
				// User chose the on-disk version; remove the .bak so
				// the prompt doesn't reappear next launch.
				_ = os.Remove(bakPath)
				a.setStatus(status.Info, "Discarded autosave snapshot")
				return a, nil
			}
			bakBytes, err := os.ReadFile(bakPath)
			if err != nil {
				a.setStatus(status.Error,
					fmt.Sprintf("Recovery read failed: %v", err))
				return a, nil
			}
			// Replace sets dirty=true; the user sees unsaved changes,
			// prompting them to Ctrl-S to commit the snapshot back to
			// the on-disk file.
			a.containerModel.Replace(bakBytes)
			a.setStatus(status.Success,
				fmt.Sprintf("Recovered from %s", filepath.Base(bakPath)))
			return a, nil
		})
	return a
}

// audioAreaStart is a shim around containerInfo.AudioAreaStart kept
// so existing call sites compile while the refactor settles.
func (a App) audioAreaStart() int {
	return a.containerInfo.AudioAreaStart
}

func (a App) forwardToSpace(action nav.Action) (tea.Model, tea.Cmd) {
	switch a.current {
	case minimap.Workspace:
		msg, intent := a.workspace.Apply(action)
		if msg != "" {
			a.setStatus(status.Info, msg)
		}
		a = a.handleWorkspaceIntent(intent)
	case minimap.Pool:
		msg, intent := a.pool.Apply(action)
		if msg != "" {
			a.setStatus(status.Info, msg)
		}
		a = a.handlePoolIntent(intent)
	case minimap.Layout:
		msg, intent := a.layout.Apply(action)
		if msg != "" {
			a.setStatus(status.Info, msg)
		}
		a = a.handleLayoutIntent(intent)
	case minimap.Sound:
		if msg := a.sound.Apply(action); msg != "" {
			a.setStatus(status.Info, msg)
		}
	}
	return a, nil
}

func (a App) handleWorkspaceIntent(intent workspace.Intent) App {
	switch intent.Kind {
	case workspace.IntentOpenContainer:
		// Switch-while-dirty guard: if the current container has
		// unsaved edits, prompt before swapping it for the new one.
		// Cancel keeps the user where they were; Discard swaps;
		// Save persists then swaps.
		if a.containerModel != nil && a.containerModel.Dirty() {
			path := intent.Path
			a.openConfirm(
				confirm.Prompt{
					Title: unsavedChangesTitle,
					Body: fmt.Sprintf(
						"The current container has unsaved edits. Open %s anyway?",
						filepath.Base(path)),
					Options: []confirm.Option{
						{Label: "Save and switch", Result: 2},
						{Label: "Discard", Result: 1},
						{Label: cancelLabel, Result: 0},
					},
				},
				func(result int) (App, tea.Cmd) {
					switch result {
					case 2: // save then switch
						if a.containerModel.Path() != "" {
							if err := a.persistContainer(a.containerModel.Path()); err != nil {
								a.setStatus(status.Error,
									fmt.Sprintf("Save before switch failed: %v", err))
								return a, nil
							}
							a.clearAutoSaveBackup(a.containerModel.Path())
						}
						a = a.doOpenContainer(path)
						return a, nil
					case 1: // discard
						a = a.doOpenContainer(path)
						return a, nil
					default:
						a.setStatus(status.Info, "Open cancelled")
						return a, nil
					}
				})
			return a
		}
		a = a.doOpenContainer(intent.Path)
	case workspace.IntentAddVoiceToPool:
		if err := a.pool.AddFZV(intent.Path); err != nil {
			a.setStatus(status.Error, fmt.Sprintf("Add to pool failed: %v", err))
		} else {
			a.setStatus(status.Success,
				fmt.Sprintf("Added %s to pool", intent.Path))
		}
	case workspace.IntentAddSampleToPool:
		err := a.pool.AddWAV(intent.Path, -1)
		switch {
		case err == nil:
			a.setStatus(status.Success,
				fmt.Sprintf("Wrapped %s and added to pool", intent.Path))
		case errors.Is(err, pool.ErrStereoNeedsChoice):
			a = a.promptStereoChannelChoice(intent.Path)
		default:
			a.setStatus(status.Error, fmt.Sprintf("Wrap WAV failed: %v", err))
		}
	case workspace.IntentNone:
		// Zero-value intent: nothing to do.
	}
	return a
}

func (a App) handleLayoutIntent(intent layout.Intent) App {
	switch intent.Kind {
	case layout.IntentOpenSound:
		voiceArea := a.containerInfo.BankCount * 1024
		a.sound.Bind(a.containerModel, a.containerInfo.BankCount, voiceArea,
			a.audioAreaStart(), intent.BankIdx, intent.AreaIdx)
		a.current = minimap.Sound
		a.minimap.Current = a.current
	case layout.IntentExtractToPool:
		off, ok := a.layout.VoiceOffset(intent.BankIdx, intent.AreaIdx)
		if !ok {
			a.setStatus(status.Warning, "Copy to pool: voice slot out of bounds")
			return a
		}
		data := a.containerModel.Bytes()
		voiceBytes := make([]byte, disk.VoicePackSize)
		copy(voiceBytes, data[off:off+disk.VoicePackSize])
		name := a.layout.VoiceName(intent.BankIdx, intent.AreaIdx)
		source := fmt.Sprintf("bank %d / area %d", intent.BankIdx+1, intent.AreaIdx+1)
		a.pool.AddFromAreaVoice(name, source, voiceBytes)
		a.setStatus(status.Success,
			fmt.Sprintf("Copied %q to pool", name))
	case layout.IntentExportArea:
		a = a.exportAreaToWorkspace(intent.BankIdx, intent.AreaIdx)
	case layout.IntentSwapAreas:
		a = a.swapAreasInBank(intent.BankIdx, intent.SourceArea, intent.AreaIdx)
	case layout.IntentEditArea:
		a = a.openAreaEditor(intent.BankIdx, intent.AreaIdx)
	case layout.IntentEditEffects:
		a = a.openEffectsEditor(intent.BankIdx)
	case layout.IntentDeleteBank:
		a = a.confirmDeleteBank(intent.BankIdx)
	case layout.IntentDuplicateArea:
		a = a.duplicateArea(intent.BankIdx, intent.AreaIdx)
	case layout.IntentDeleteArea:
		bankIdx := intent.BankIdx
		areaIdx := intent.AreaIdx
		areaName := a.layout.VoiceName(bankIdx, areaIdx)
		a.openConfirm(
			confirm.Prompt{
				Title: "Delete Area?",
				Body: fmt.Sprintf(
					"Clear the voice at Bank %d / Area %d (%q).",
					bankIdx+1, areaIdx+1, areaName),
				Options: []confirm.Option{
					{Label: cancelLabel, Result: 0},
					{Label: "Delete", Result: 1},
				},
			},
			func(result int) (App, tea.Cmd) {
				if result != 1 {
					a.setStatus(status.Info, "Delete cancelled")
					return a, nil
				}
				return a.deleteArea(bankIdx, areaIdx, areaName), nil
			})
	case layout.IntentRenameVoice:
		off, ok := a.layout.VoiceOffset(intent.BankIdx, intent.AreaIdx)
		if !ok {
			a.setStatus(status.Warning, "Rename: voice slot out of bounds")
			return a
		}
		data := a.containerModel.Bytes()
		raw := data[off+disk.VoiceNameOffset : off+disk.VoiceNameOffset+disk.VoiceNameFieldSize]
		current := strings.TrimRight(strings.Trim(string(raw), "\x00"), " ")
		a.renameActive = true
		a.renameBank = false
		a.renameTarget = pickerTarget{BankIdx: intent.BankIdx, AreaIdx: intent.AreaIdx}
		a.renameBuffer = current
		a.renameFresh = true
		a.setStatus(status.Info,
			"Rename: type new name, Enter saves, Esc cancels")
	case layout.IntentRenameBank:
		a.renameActive = true
		a.renameBank = true
		a.renameTarget = pickerTarget{BankIdx: intent.BankIdx}
		a.renameBuffer = a.layout.BankName(intent.BankIdx)
		a.renameFresh = true
		a.setStatus(status.Info,
			"Rename bank: type new name, Enter saves, Esc cancels")
	case layout.IntentStartPicker:
		if len(a.pool.Entries()) == 0 {
			a.setStatus(status.Warning,
				"Pool is empty; import a voice via Workspace first")
			return a
		}
		target := pickerTarget{
			BankIdx:     intent.BankIdx,
			AreaIdx:     intent.AreaIdx,
			ReturnFocus: a.current,
		}
		a.pickingFor = &target
		a.pool.SetPickerTarget(
			fmt.Sprintf("Bank %d / Area %d", intent.BankIdx+1, intent.AreaIdx+1))
		a.current = minimap.Pool
		a.minimap.Current = a.current
		a.setStatus(status.Info,
			"Pick a voice, Enter to assign, Esc to cancel")
	case layout.IntentNone:
		// Zero-value intent: nothing to do.
	}
	return a
}

func (a App) handlePoolIntent(intent pool.Intent) App {
	switch intent.Kind {
	case pool.IntentAuditionPoolEntry:
		if intent.Entry == nil {
			return a
		}
		voiceID := audio.VoiceID(fmt.Sprintf("pool:%s", intent.Entry.Name))
		entryBytes := intent.Entry.Bytes
		if len(entryBytes) < disk.SectorSize {
			a.setStatus(status.Warning, "Audition: pool entry too small")
			return a
		}
		// Pool entries are FZV-shaped: header sector (with wave pointers
		// rewritten to be 0-relative by voiceunpack / voiceimport) plus
		// audio at SectorSize:.
		hdr := entryBytes[:disk.VoicePackSize]
		audioArea := entryBytes[disk.SectorSize:]
		pitch := int(hdr[disk.VoiceKeyCentOffset])
		if err := audio.Audition(voiceID, hdr, audioArea, pitch); err != nil {
			a.setStatus(status.Error, fmt.Sprintf("Audition failed: %v", err))
			return a
		}
		if audio.CurrentVoiceID() == voiceID {
			a.setStatus(status.Success,
				fmt.Sprintf("Auditioning pool entry %q", intent.Entry.Name))
		} else {
			a.setStatus(status.Info, "Audition stopped")
		}
	case pool.IntentAssignToArea:
		if a.pickingFor == nil {
			a.setStatus(status.Warning,
				"No assign target; press 'i' on an Area in Layout to start")
			return a
		}
		if intent.Entry == nil {
			a.setStatus(status.Warning, "Assign: no pool entry")
			return a
		}
		target := *a.pickingFor
		a = a.assignPoolEntryToArea(intent.Entry, target.BankIdx, target.AreaIdx)
		a = a.closePicker(target.ReturnFocus)
	case pool.IntentCancelPicker:
		if a.pickingFor != nil {
			ret := a.pickingFor.ReturnFocus
			a = a.closePicker(ret)
			a.setStatus(status.Info, "Import cancelled")
		}
	case pool.IntentNone:
		// Zero-value intent: nothing to do.
	}
	return a
}

// closePicker exits picker mode, clearing both the App-level pickingFor
// state and the Pool's banner. Restores focus to where the picker was
// opened from (Layout, by construction).
func (a App) closePicker(returnFocus minimap.Space) App {
	a.pickingFor = nil
	a.pool.SetPickerTarget("")
	a.current = returnFocus
	a.minimap.Current = a.current
	return a
}

// validateDuplicateInputs checks the input bounds for duplicateArea
// and reads the bank's current bstep. Returns the bstep and ok=true
// when the duplicate is allowed to proceed; sets a status warning
// and returns ok=false otherwise.
func (a *App) validateDuplicateInputs(bankIdx, areaIdx int) (int, bool) {
	data := a.containerModel.Bytes()
	base := bankIdx * disk.SectorSize
	bstepOff := base + disk.BankVoiceCountOffset
	if bstepOff+2 > len(data) {
		a.setStatus(status.Warning, "Duplicate: bank out of bounds")
		return 0, false
	}
	bstep := int(binary.LittleEndian.Uint16(data[bstepOff : bstepOff+2]))
	switch {
	case bstep == 0:
		a.setStatus(status.Warning, "Duplicate: bank is empty")
		return 0, false
	case bstep >= disk.MaxVoices:
		a.setStatus(status.Warning,
			fmt.Sprintf("Duplicate: bank full (%d Areas)", disk.MaxVoices))
		return 0, false
	case areaIdx < 0 || areaIdx >= bstep:
		a.setStatus(status.Warning, "Duplicate: area out of range")
		return 0, false
	}
	return bstep, true
}

// readVoiceHeaderForArea returns a copy of the 256-byte voice
// pack pointed at by the bank's vp[areaIdx]. Returns ok=false (and
// sets a status warning) when the lookup fails or the slot's bytes
// would run off the end of the container.
func (a *App) readVoiceHeaderForArea(bankIdx, areaIdx int) ([]byte, bool) {
	data := a.containerModel.Bytes()
	srcSlot, ok := disk.BankVPLookup(data, bankIdx, areaIdx)
	if !ok {
		a.setStatus(status.Warning, "Duplicate: source Area has no voice")
		return nil, false
	}
	voiceAreaStart := a.containerInfo.BankCount * disk.SectorSize
	srcOff := disk.VoiceSlotOffset(voiceAreaStart, srcSlot)
	if srcOff+disk.VoicePackSize > len(data) {
		a.setStatus(status.Error, "Duplicate: source voice out of bounds")
		return nil, false
	}
	header := make([]byte, disk.VoicePackSize)
	copy(header, data[srcOff:srcOff+disk.VoicePackSize])
	return header, true
}

// allocateVoiceSlot returns the index of a fresh voice slot at the
// end of the voice area, growing the voice area by one sector if
// the slot would land in the audio area. When growth occurs, the
// audio area shifts forward by SectorSize; existing wave / gen
// pointers (relative to AudioAreaStart) remain valid.
func (a App) allocateVoiceSlot() (App, int) {
	voiceAreaStart := a.containerInfo.BankCount * disk.SectorSize
	audioStart := a.containerInfo.AudioAreaStart
	currentVoiceSectors := (audioStart - voiceAreaStart) / disk.SectorSize
	currentSlots := currentVoiceSectors * disk.VoicesPerSector
	newSlot := currentSlots
	requiredVoiceSectors := disk.VoiceAreaSectors(newSlot + 1)
	growSectors := requiredVoiceSectors - currentVoiceSectors
	if growSectors > 0 {
		growBytes := growSectors * disk.SectorSize
		data := a.containerModel.Bytes()
		newData := make([]byte, len(data)+growBytes)
		copy(newData[:audioStart], data[:audioStart])
		// growBytes zero-bytes between the old voice tail and the
		// old audio area come from make()'s zero initialisation.
		copy(newData[audioStart+growBytes:], data[audioStart:])
		a.containerModel.Replace(newData)
		a.containerInfo.AudioAreaStart = audioStart + growBytes
		a.containerInfo.TotalBytes = int64(a.containerModel.Len())
		if a.containerInfo.Header != nil {
			a.containerInfo.Header.VoiceAreaStart = a.containerInfo.AudioAreaStart
		}
	}
	return a, newSlot
}

// duplicateArea appends a new Area at bstep that clones the source
// Area's voice and per-area metadata. The new voice header is
// copied into a fresh slot at the end of the voice area (growing
// the voice area if needed). Wave / gen pointers are unchanged:
// the duplicate shares audio with the source. Subsequent edits to
// either Area's voice header are independent.
//
// Rejects:
//   - bank empty (nothing to duplicate);
//   - bank full (bstep already at MaxVoices=64);
//   - source Area's vp[] doesn't resolve to a real voice slot.
func (a App) duplicateArea(bankIdx, areaIdx int) App {
	if a.containerModel == nil {
		return a
	}
	bstep, ok := a.validateDuplicateInputs(bankIdx, areaIdx)
	if !ok {
		return a
	}

	srcHeader, ok := a.readVoiceHeaderForArea(bankIdx, areaIdx)
	if !ok {
		return a
	}

	// Allocate the next voice slot (extending the voice area if
	// needed). After this point, a.containerModel may be larger
	// and a.containerInfo.AudioAreaStart may have shifted.
	a, newSlot := a.allocateVoiceSlot()

	// Build the patch batch: header copy + vp[bstep] + per-area
	// metadata + bstep bump. One ApplyBatch keeps the duplicate
	// atomic for Undo.
	data := a.containerModel.Bytes()
	base := bankIdx * disk.SectorSize
	voiceAreaStart := a.containerInfo.BankCount * disk.SectorSize
	newOff := disk.VoiceSlotOffset(voiceAreaStart, newSlot)

	patches := container.DuplicateAreaPatches(data, container.DuplicateAreaParams{
		Base: base, NewOff: newOff, SrcAreaIdx: areaIdx, Bstep: bstep, NewSlot: newSlot, SrcHeader: srcHeader,
	})

	if err := a.containerModel.ApplyBatch(patches); err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Duplicate failed: %v", err))
		return a
	}
	a.layout.RefreshContainer(a.containerModel, a.containerInfo)
	a.setStatus(status.Success,
		fmt.Sprintf("Duplicated Area %d as Area %d", areaIdx+1, bstep+1))
	return a
}

// deleteArea collapses the bank's per-area arrays so Area indices
// past areaIdx shift down by one, then decrements bstep. This is
// what real FZ-1 firmware expects: vp[] entries within bstep MUST
// be valid voice-slot indices. The voice slot the deleted Area
// pointed at is left in place as an orphan; the save-time
// compactVoiceArea pass reclaims trailing orphans (and audio).
func (a App) deleteArea(bankIdx, areaIdx int, name string) App {
	data := a.containerModel.Bytes()
	base := bankIdx * disk.SectorSize
	bstepOff := base + disk.BankVoiceCountOffset
	if bstepOff+2 > len(data) {
		a.setStatus(status.Warning, "Delete: bank out of bounds")
		return a
	}
	bstep := int(binary.LittleEndian.Uint16(data[bstepOff : bstepOff+2]))
	if areaIdx < 0 || areaIdx >= bstep {
		a.setStatus(status.Warning, "Delete: area out of range")
		return a
	}

	patches := container.DeleteAreaPatches(data, container.DeleteAreaParams{
		Base: base, AreaIdx: areaIdx, Bstep: bstep,
	})

	if err := a.containerModel.ApplyBatch(patches); err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Delete failed: %v", err))
		return a
	}
	a.setStatus(status.Success,
		fmt.Sprintf("Cleared Bank %d / Area %d (%q)", bankIdx+1, areaIdx+1, name))
	return a
}

// assignPoolEntryToArea writes the pool entry's voice into the target
// Area. The flow:
//
//  1. Append the pool entry's PCM to the container's audio area.
//  2. Rewrite the voice header's wave / gen / loop pointers to be
//     absolute sample addresses into the container's audio area
//     instead of the FZV-relative 0-based offsets the pool entry
//     carried.
//  3. Force the display name to the pool entry's Name (defends
//     against a blank name field in the encoded bytes).
//  4. Patch the rewritten header into the target slot.
//  5. Bump the bank's bstep so the Layout view treats the slot as
//     populated, and set default key/velocity ranges on the slot if
//     the bank still carries zeros there.
//
// Steps 1 and 4 cannot be a single atomic Patch because the container
// grows in size; the buffer is rebuilt and pushed through
// model.Replace, which clears undo/redo (the new buffer's offsets no
// longer line up with prior patches).
func (a App) assignPoolEntryToArea(entry *pool.Entry, bankIdx, areaIdx int) App {
	if len(entry.Bytes) < disk.SectorSize {
		a.setStatus(status.Warning, "Assign: pool entry too small")
		return a
	}

	// Auto-grow banks when assigning to an unmaterialised bank. The
	// bank-list view shows all 8 banks but only BankCount sectors
	// exist in the buffer; the first assignment to bank N>BankCount
	// inserts the missing sectors (with default-blank metadata).
	if bankIdx >= a.containerInfo.BankCount {
		var ok bool
		a, ok = a.growBanksTo(bankIdx + 1)
		if !ok {
			return a
		}
	}

	data := a.containerModel.Bytes()
	voiceAreaStart := a.containerInfo.BankCount * disk.SectorSize
	audioStart := a.containerInfo.AudioAreaStart
	if audioStart <= 0 || audioStart > len(data) {
		a.setStatus(status.Error, "Assign: container audio area not configured")
		return a
	}

	entryPCM := entry.Bytes[disk.SectorSize:]
	if len(entryPCM)%disk.BytesPerSample != 0 {
		a.setStatus(status.Warning, "Assign: pool entry's PCM is misaligned")
		return a
	}

	// Compute slotIdx purely from bank metadata so the math holds even
	// when the slot lies past the current voice area (we grow next).
	slotIdx := a.layout.VoiceSlotIndex(bankIdx, areaIdx)

	// Voice area can grow: NewUntitled ships with one voice sector
	// (=4 slots). Assigning beyond that (Area 5+ in any bank) used
	// to write into the audio area. We insert zero sectors at the
	// voice/audio boundary as needed so the new slot fits, shifting
	// the audio area later. Existing voices' wave pointers stay
	// valid because they're relative to the audio area's *start*,
	// not absolute file offsets.
	requiredVoiceSectors := disk.VoiceAreaSectors(slotIdx + 1)
	currentVoiceSectors := (audioStart - voiceAreaStart) / disk.SectorSize
	growSectors := requiredVoiceSectors - currentVoiceSectors
	if growSectors < 0 {
		growSectors = 0
	}
	growBytes := growSectors * disk.SectorSize
	newAudioStart := audioStart + growBytes

	// Where the new voice's PCM will land: at the current end of the
	// container, which is the end of the audio area.
	audioUsedBytes := len(data) - audioStart
	if audioUsedBytes < 0 {
		audioUsedBytes = 0
	}
	newWaveStartSamples := uint32(audioUsedBytes / disk.BytesPerSample)

	// Build the new voice header from the pool entry's, rewriting all
	// wave-area pointers to be absolute sample offsets into the
	// container's audio area.
	header := make([]byte, disk.VoicePackSize)
	copy(header, entry.Bytes[:disk.VoicePackSize])
	container.RewriteWavePointers(header, newWaveStartSamples)

	// Force the display name.
	paddedName := disk.PadLabel(entry.Name)
	copy(header[disk.VoiceNameOffset:disk.VoiceNameOffset+disk.LabelSize], paddedName[:])
	header[disk.VoiceNameOffset+disk.LabelSize] = 0
	header[disk.VoiceNameOffset+disk.LabelSize+1] = 0

	// Compose new buffer:
	//   [bank sectors][old voice area][grow zeros][old audio area][new PCM]
	newData := make([]byte, len(data)+growBytes+len(entryPCM))
	copy(newData[0:audioStart], data[0:audioStart])
	// growBytes worth of zero already from make()
	copy(newData[newAudioStart:newAudioStart+audioUsedBytes], data[audioStart:])
	copy(newData[newAudioStart+audioUsedBytes:], entryPCM)
	a.containerModel.Replace(newData)

	// New voice offset in the grown buffer.
	off := disk.VoiceSlotOffset(voiceAreaStart, slotIdx)

	// Patch the voice header + bank metadata in one batch on the new
	// buffer.
	patches := []model.Patch{}
	old := make([]byte, disk.VoicePackSize)
	copy(old, newData[off:off+disk.VoicePackSize])
	patches = append(patches, model.Patch{Offset: off, Old: old, New: header})
	// Point bank's vp[areaIdx] at the slot we just wrote so reads via
	// BankVPLookup resolve to this voice. Without this patch, vp[]
	// stays at its default (0) and reads land on slot 0, hiding the
	// assignment from Layout's area-list.
	vpOff := bankIdx*disk.SectorSize + disk.BankVoiceNumOffset + disk.VPEntrySize*areaIdx
	if vpOff+disk.VPEntrySize <= len(newData) {
		oldVP := make([]byte, disk.VPEntrySize)
		copy(oldVP, newData[vpOff:vpOff+disk.VPEntrySize])
		newVP := make([]byte, disk.VPEntrySize)
		binary.LittleEndian.PutUint16(newVP, uint16(slotIdx)) //nolint:gosec // G115: slotIdx is a voice-slot index bounded by the disk format's voice area capacity (well under uint16 max).
		patches = append(patches, model.Patch{Offset: vpOff, Old: oldVP, New: newVP})
	}
	if bumpPatch, ok := container.BankBstepBumpPatch(newData, bankIdx, areaIdx); ok {
		patches = append(patches, bumpPatch)
	}
	patches = append(patches, container.DefaultBankRangePatches(newData, bankIdx, areaIdx)...)
	if err := a.containerModel.ApplyBatch(patches); err != nil {
		a.setStatus(status.Error, fmt.Sprintf("Assign failed: %v", err))
		return a
	}

	// Update container info so audition + Layout viewers see the new
	// audio area extent and voice count. AudioAreaStart shifts by
	// growBytes when the voice area grew; the audition path reads it
	// to compute sample-pointer translations.
	a.containerInfo.TotalBytes = int64(a.containerModel.Len())
	a.containerInfo.PCMBytes += int64(len(entryPCM))
	a.containerInfo.AudioAreaStart = newAudioStart
	if a.containerInfo.Header != nil {
		// Bump the cached NVoice so SetContainer sees the new count if
		// the user re-binds via Layout.
		needed := areaIdx + 1
		for b := 0; b < bankIdx; b++ {
			base := b * disk.SectorSize
			if base+disk.BankVoiceCountOffset+2 <= len(a.containerModel.Bytes()) {
				needed += int(binary.LittleEndian.Uint16(
					a.containerModel.Bytes()[base+disk.BankVoiceCountOffset:]))
			}
		}
		if needed > a.containerInfo.Header.NVoice {
			a.containerInfo.Header.NVoice = needed
		}
		a.containerInfo.VoiceCount = a.containerInfo.Header.NVoice
	}
	// Keep Layout's view of the container in sync so the rebuilt voice
	// list reflects the assignment. Use Refresh, not SetContainer, so
	// the cursor stays on the Area the user just imported into.
	a.layout.RefreshContainer(a.containerModel, a.containerInfo)

	a.setStatus(status.Success,
		fmt.Sprintf("Assigned %q to Bank %d / Area %d",
			entry.Name, bankIdx+1, areaIdx+1))
	return a
}

// View implements tea.Model.
func (a App) View() tea.View {
	if a.tooSmall {
		msg := fmt.Sprintf(
			"Studio requires %d columns by %d rows.\n"+
				"Current: %d by %d.\n"+
				"Resize the terminal to continue.",
			minCols, minRows, a.width, a.height)
		styled := theme.SilverText.Render(msg)
		v := tea.NewView(centre(styled, a.width, a.height))
		v.AltScreen = true
		return v
	}

	var pane string
	switch a.current {
	case minimap.Workspace:
		pane = a.workspace.View(a.width, a.height)
	case minimap.Pool:
		pane = a.pool.View(a.width, a.height)
	case minimap.Layout:
		pane = a.layout.View(a.width, a.height)
	case minimap.Sound:
		pane = a.sound.View(a.width, a.height)
	}

	// The top bar consumes one row; the minimap column on the right
	// adds 12 cells of width; the bottom strip (dirty + status +
	// footer) takes 3 rows.
	paneBox := theme.BorderBox.
		Width(a.width-12).
		Height(a.height-7).
		Padding(0, 1).
		Render(pane)

	mini := a.minimap.View()

	rightCol := mini
	if a.containerInfo.TotalBytes > 0 {
		const fzDiskBytes int64 = 1310720 // FZ-1 floppy capacity: 1.25 MB
		free := fzDiskBytes - a.containerInfo.TotalBytes
		freeText := fmt.Sprintf("Free: %d KB", (free+512)/1024)
		freeStyle := theme.DimText
		if free <= 0 {
			freeText = "Free: 0 KB"
			freeStyle = theme.ErrorText
		} else if free < 64*1024 {
			// <64 KB headroom: highlight in warning amber so the user
			// sees the disk is nearly full while editing.
			freeStyle = theme.WarnText
		}
		rightCol = lipgloss.JoinVertical(lipgloss.Left, mini, "", freeStyle.Render(freeText))
	}

	top := lipgloss.JoinHorizontal(lipgloss.Top, paneBox, " ", rightCol)

	dirty := ""
	if a.containerModel != nil && a.containerModel.Dirty() {
		dirty = theme.WarnText.Render("● modified")
	}

	footer := theme.DimText.Render(
		"arrows cursor  •  SHIFT+up/down between spaces  •  Enter open  •  n new disk  •  Ctrl-S save  •  Ctrl-Z undo  •  ? help  •  Ctrl-Q quit")
	statusLine := a.status.View()
	toastLine := a.toast.View()

	bar := topbar.View(a.width, a.containerInfo.Path)
	rows := []string{bar, top, dirty}
	if toastLine != "" {
		rows = append(rows, toastLine)
	}
	rows = append(rows, statusLine, footer)
	body := lipgloss.JoinVertical(lipgloss.Left, rows...)

	// Modals overlay the body via lipgloss's compositor instead of
	// stacking below it. Compose with the body at z=0 and the modal at
	// z=1 positioned to centre over the body. Only one modal is ever
	// shown at a time (Update gates input through them in priority
	// order: saveAs > confirm > help).
	var overlay string
	switch {
	case a.saveAsActive:
		overlay = a.renderSaveAsModal()
	case a.renameActive:
		overlay = a.renderRenameModal()
	case a.areaEditor.IsOpen():
		overlay = a.areaEditor.View()
	case a.effectsEditor.IsOpen():
		overlay = a.effectsEditor.View()
	case a.confirm.IsOpen():
		overlay = a.confirm.View()
	case a.help.IsOpen():
		overlay = a.help.View()
	}
	if overlay != "" {
		body = overlayCentered(body, overlay, a.width, a.height)
	}

	v := tea.NewView(body)
	v.AltScreen = true
	return v
}

// overlayCentered composes a base layer and a modal layer so the modal
// sits centred over the base instead of appended below it.
func overlayCentered(base, modal string, width, height int) string {
	modalW := lipgloss.Width(modal)
	modalH := lipgloss.Height(modal)
	x := (width - modalW) / 2
	if x < 0 {
		x = 0
	}
	y := (height - modalH) / 2
	if y < 0 {
		y = 0
	}
	baseLayer := lipgloss.NewLayer(base).Z(0)
	modalLayer := lipgloss.NewLayer(modal).X(x).Y(y).Z(1)
	return lipgloss.NewCompositor(baseLayer, modalLayer).Render()
}

func (a App) renderRenameModal() string {
	var heading string
	if a.renameBank {
		heading = fmt.Sprintf("Rename Bank %d", a.renameTarget.BankIdx+1)
	} else {
		heading = fmt.Sprintf("Rename Bank %d / Area %d",
			a.renameTarget.BankIdx+1, a.renameTarget.AreaIdx+1)
	}
	title := theme.Heading.Render(heading)
	field := theme.PrimaryText.Render("Name: ")
	value := a.renameBuffer
	if a.renameFresh && value != "" {
		// "selected" affordance: underline the pre-loaded name so the
		// user sees that a single keystroke will overwrite.
		field += theme.AccentText.Underline(true).Render(value)
	} else {
		field += theme.AccentText.Render(value) + theme.DimText.Render("_")
	}
	hint := theme.DimText.Render("Enter saves  •  Esc cancels  •  max 12 chars")
	body := title + "\n\n" + field + "\n\n" + hint
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 3).
		Render(body)
}

func (a App) renderSaveAsModal() string {
	prompt := theme.Heading.Render("Save As") + "\n\n" +
		theme.PrimaryText.Render("Filename: ") +
		theme.AccentText.Render(a.saveAsBuffer) +
		theme.DimText.Render("_") + "\n\n" +
		theme.DimText.Render("Enter to save, Esc to cancel")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 3).
		Render(prompt)
}

// centre pads a single rendered block to width x height with empty
// space so it lands in the centre.
func centre(content string, width, height int) string {
	if width <= 0 || height <= 0 {
		return content
	}
	contentLines := strings.Split(content, "\n")
	contentHeight := len(contentLines)
	contentWidth := 0
	for _, l := range contentLines {
		if w := lipgloss.Width(l); w > contentWidth {
			contentWidth = w
		}
	}
	leftPad := (width - contentWidth) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	topPad := (height - contentHeight) / 2
	if topPad < 0 {
		topPad = 0
	}
	padded := make([]string, 0, height)
	for i := 0; i < topPad; i++ {
		padded = append(padded, "")
	}
	for _, l := range contentLines {
		padded = append(padded, strings.Repeat(" ", leftPad)+l)
	}
	return strings.Join(padded, "\n")
}
