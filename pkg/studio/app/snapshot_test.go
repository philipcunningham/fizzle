package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/audio"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/minimap"
)

// fixtureWorkspace materialises a workspace directory holding the
// Piano.fzf file from the corpus. We copy rather than pointing at the
// corpus directly so snapshots stay deterministic if other fixtures
// land alongside Piano.fzf later.
func fixtureWorkspace(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join("..", "..", "..", "testdata", "corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-a-piano", "Piano.fzf")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Piano.fzf"), data, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("copy fixture: %v", err)
	}
	return dir
}

// step feeds messages through Update and returns the resulting App.
// Mirrors the helper in lattice's tui tests so studio snapshots can
// drive the model from outside a running Bubble Tea program.
func step(t testing.TB, a App, msgs ...tea.Msg) App {
	t.Helper()
	var m tea.Model = a
	for _, msg := range msgs {
		m, _ = m.Update(msg)
	}
	a, _ = m.(App)
	return a
}

// renderView returns the rendered string the View produces, with ANSI
// styling stripped so the snapshot survives palette changes. Path
// fields are rewritten to deterministic placeholders so the snapshot
// survives the random tempdir t.TempDir hands out. On Windows the
// native path separator is rewritten to '/' so the committed snapshot
// stays cross-platform: real users still see native separators in
// the running TUI, this normalisation only applies to the test view.
func renderView(a App) string {
	origDir := a.directory
	a = stabilize(a)
	out := stripANSI(a.View().Content)
	// Status messages cached before stabilize() still hold the raw path;
	// substitute them so the snapshot is stable.
	if runtime.GOOS == "windows" {
		// Normalise path separators in the rendered view so the
		// committed snapshot stays cross-platform. The running TUI
		// still shows native '\' to real Windows users; only this
		// test view normalises before substitution and comparison.
		out = strings.ReplaceAll(out, `\`, "/")
		origDir = filepath.ToSlash(origDir)
	}
	if origDir != "" {
		out = strings.ReplaceAll(out, origDir, "<workspace>")
	}
	// Trim trailing whitespace per line. The status line picks up
	// padding tied to the original tempdir name's length; trimming
	// keeps snapshots stable across tempdir name shuffles without
	// touching the box-border content.
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

// stabilize replaces every path-shaped field with a deterministic
// placeholder. The snapshot_test.go file is package app and has
// access to unexported fields on App and Workspace.
func stabilize(a App) App {
	if a.directory != "" {
		a.workspace.SubstituteDirectoryPrefix(a.directory, "<workspace>")
		a.directory = "<workspace>"
	}
	if a.containerInfo.Path != "" {
		stable := "<workspace>/" + filepath.Base(a.containerInfo.Path)
		a.containerInfo.Path = stable
		a.layout.SetPathLabel(stable)
		if a.containerModel != nil && a.containerModel.Path() != "" {
			a.containerModel.SetPath(stable)
		}
	}
	return a
}

// stripANSI removes CSI/OSC escape sequences so snapshots stay
// readable and stable across palette tweaks. It is the same shape as
// the helper in lattice.
func stripANSI(s string) string {
	var out strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 0x40 && r <= 0x7e) && r != '[' {
				inEsc = false
			}
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func keyPress(s string) tea.KeyPressMsg {
	switch s {
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case testKeyLeft:
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case testKeyRight:
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "shift+up":
		return tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift}
	case "shift+down":
		return tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}
	case testKeyEnter:
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case testKeyEsc:
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	}
	// Treat anything else as a single text rune.
	if len(s) == 1 {
		return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
	}
	return tea.KeyPressMsg{Text: s}
}

func windowSize(w, h int) tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: w, Height: h}
}

func TestSnapshot_WorkspaceInitial(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a, windowSize(140, 40))
	snaps.MatchSnapshot(t, renderView(a))
}

func TestSnapshot_LayoutAfterOpeningPiano(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a, windowSize(140, 40), keyPress(testKeyEnter))
	snaps.MatchSnapshot(t, renderView(a))
}

func TestSnapshot_LayoutBank2AreaList(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a,
		windowSize(140, 40),
		keyPress(testKeyEnter), // open Piano.fzf, lands in Layout
		keyPress("down"),       // move bank cursor to Bank 2
		keyPress(testKeyEnter), // drill into Bank 2
	)
	snaps.MatchSnapshot(t, renderView(a))
}

func TestSnapshot_WorkspaceCursorMovesDown(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.fzf", "b.fzf"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("placeholder"), 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
			t.Fatalf("seed: %v", err)
		}
	}
	a := New(dir)
	before := renderView(step(t, a, windowSize(140, 40)))
	after := renderView(step(t, a, windowSize(140, 40), keyPress("down")))
	if before == after {
		t.Fatalf("pressing down did not change the rendered view:\n%s", after)
	}
	snaps.MatchSnapshot(t, after)
}

// TestSnapshot_WorkspaceAscendsFromSubdirectory pins the ascend
// contract: after descending into a subdirectory the user can come
// back up with Left / Esc. Regression for the "Cannot ascend past the
// workspace root" bug that fired when the workspace root was passed
// as a relative path and filepath.HasPrefix returned false on the
// subpath.
func TestSnapshot_WorkspaceAscendsFromSubdirectory(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "samples")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "top.fzf"), []byte("x"), 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed top: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "nested.fzv"), []byte("y"), 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed nested: %v", err)
	}
	a := New(dir)
	// Drill into samples/, then ascend back to dir/.
	a = step(t, a, windowSize(140, 40),
		keyPress(testKeyEnter), // descend (samples/ sorts first as a directory)
		keyPress(testKeyLeft),  // ascend
	)
	if got := a.workspace.CurrentDirectory(); got != dir {
		t.Fatalf("expected cwd back at %q after ascend; got %q", dir, got)
	}
	view := renderView(a)
	if strings.Contains(view, "Cannot ascend") {
		t.Fatalf("ascend reported error but path was inside workspace root:\n%s", view)
	}
	if !strings.Contains(view, "top.fzf") {
		t.Fatalf("expected top.fzf back on screen after ascending; got:\n%s", view)
	}
}

// TestSnapshot_WorkspaceTraversesIntoSubdirectory pins the
// directory-traversal contract: Enter on a subdirectory descends into
// it, and the rendered view then shows the subdirectory's contents,
// not the parent's.
func TestSnapshot_WorkspaceTraversesIntoSubdirectory(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "samples")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "top-level.fzf"), []byte("x"), 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed top: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "nested.fzv"), []byte("y"), 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed nested: %v", err)
	}
	a := New(dir)
	// The filepicker sorts directories first, so "samples/" is row 0.
	a = step(t, a, windowSize(140, 40), keyPress(testKeyEnter))

	view := renderView(a)
	if !strings.Contains(view, "nested.fzv") {
		t.Fatalf("expected nested.fzv after traversing into 'samples/', got:\n%s", view)
	}
	if strings.Contains(view, "top-level.fzf") {
		t.Fatalf("expected top-level.fzf hidden after descending; got:\n%s", view)
	}
	snaps.MatchSnapshot(t, view)
}

func TestSnapshot_SmallTerminalNotice(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a, windowSize(30, 8))
	snaps.MatchSnapshot(t, renderView(a))
}

func TestSnapshot_HelpModalOpen(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a, windowSize(140, 40), keyPress("?"))
	snaps.MatchSnapshot(t, renderView(a))
}

func TestSnapshot_PoolEmpty(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a, windowSize(140, 40),
		keyPress("shift+down"), // Workspace -> Pool
	)
	snaps.MatchSnapshot(t, renderView(a))
}

func TestSnapshot_SoundAfterDrillingIntoArea(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a,
		windowSize(140, 40),
		keyPress(testKeyEnter), // open Piano.fzf, land in Layout
		keyPress(testKeyEnter), // open Bank 1
		keyPress(testKeyEnter), // open Area 1, emit IntentOpenSound
	)
	snaps.MatchSnapshot(t, renderView(a))
}

func TestSnapshot_SoundNavigateRight(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a,
		windowSize(140, 40),
		keyPress(testKeyEnter), // open Piano.fzf
		keyPress(testKeyEnter), // open Bank 1
		keyPress(testKeyEnter), // open Area 1
		keyPress(testKeyRight), // move sound cursor right one column
	)
	snaps.MatchSnapshot(t, renderView(a))
}

func TestSnapshot_SaveAsModalOpen(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	// Container is untitled at launch. Ctrl-S should open save-as.
	a = step(t, a,
		windowSize(140, 40),
		tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl},
	)
	if !a.saveAsActive {
		t.Fatalf("save-as did not activate after Ctrl-S on untitled container")
	}
	snaps.MatchSnapshot(t, renderView(a))
}

func TestSnapshot_SaveAsModalWithFilename(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a,
		windowSize(140, 40),
		tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl},
		keyPress("M"), keyPress("y"), keyPress("D"), keyPress("i"), keyPress("s"), keyPress("k"),
	)
	if a.saveAsBuffer != "MyDisk" {
		t.Fatalf("saveAsBuffer = %q, want %q", a.saveAsBuffer, "MyDisk")
	}
	snaps.MatchSnapshot(t, renderView(a))
}

func TestSnapshot_UndoOnUntitled(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a,
		windowSize(140, 40),
		tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl},
	)
	snaps.MatchSnapshot(t, renderView(a))
}

// TestSnapshot_SoundEnvelopeStageRoleLabel pins the FZ-1 LCD
// convention that a "normal" (non-SUS, non-END) envelope stage shows
// "***" rather than "Normal". Lands on DCA stage cell s0 (column 3),
// where the Role enum is the first field. A regression would re-add
// the word "Normal" to the Role line.
func TestSnapshot_SoundEnvelopeStageRoleLabel(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a,
		windowSize(140, 40),
		keyPress(testKeyEnter), // open Piano.fzf
		keyPress(testKeyEnter), // open Bank 1
		keyPress(testKeyEnter), // open Area 1, lands in Sound (col=1 [lvlKF/VF])
		keyPress(testKeyRight), // -> col=2 [rateKF/VF]
		keyPress(testKeyRight), // -> col=3 [s0]
	)
	view := renderView(a)
	if strings.Contains(view, "Normal") {
		t.Fatalf("Sound envelope cell shows %q; FZ-1 LCD uses *** for non-SUS/END stages", "Normal")
	}
	snaps.MatchSnapshot(t, view)
}

// TestSnapshot_SoundLoopsVisual pins the Loops row's visual cell:
// drilling into Loops should show the sample waveform with loop
// markers overlaid, not the old "(visual deferred)" placeholder.
func TestSnapshot_SoundLoopsVisual(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	// Walk: open Piano, open Bank 1, open Area 1, switch to Loops row.
	// Sound binds at DCA (row 0); four downs lands on Loops (row 4).
	a = step(t, a,
		windowSize(140, 40),
		keyPress(testKeyEnter),
		keyPress(testKeyEnter),
		keyPress(testKeyEnter),
		keyPress("down"),      // DCA -> DCF
		keyPress("down"),      // DCF -> LFO
		keyPress("down"),      // LFO -> Sample
		keyPress("down"),      // Sample -> Loops
		keyPress(testKeyLeft), // [L0] -> [vis]
	)
	view := renderView(a)
	if strings.Contains(view, "visual deferred") {
		t.Fatalf("Loops row still shows the deferred placeholder")
	}
	if !strings.Contains(view, "Loops") {
		t.Fatalf("Loops cell heading missing from view")
	}
	snaps.MatchSnapshot(t, view)
}

// TestSnapshot_LayoutRenameVoiceInline pins the inline-rename gesture
// from the Layout Area list. Drilling into Sound just to rename was
// tedious; pressing `r` on an Area opens a rename modal and Enter
// patches the voice header's name field directly.
func TestSnapshot_LayoutRenameVoiceInline(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a,
		windowSize(140, 40),
		keyPress(testKeyEnter), // open Piano.fzf
		keyPress(testKeyEnter), // open Bank 1 (Area list, cursor on A01)
		keyPress("r"),          // open rename modal
	)
	if !a.renameActive {
		t.Fatalf("rename modal did not open after `r`")
	}
	if a.renameTarget.BankIdx != 0 || a.renameTarget.AreaIdx != 0 {
		t.Fatalf("renameTarget = %+v, want (0,0)", a.renameTarget)
	}
	// Type a new name and commit. First keystroke clears the pre-loaded
	// existing name (draftFresh).
	a = step(t, a,
		keyPress("F"), keyPress("O"), keyPress("O"),
		keyPress(testKeyEnter),
	)
	if a.renameActive {
		t.Fatalf("rename modal should close after Enter")
	}

	off, _ := a.layout.VoiceOffset(0, 0)
	data := a.containerModel.Bytes()
	raw := data[off+disk.VoiceNameOffset : off+disk.VoiceNameOffset+disk.VoiceNameFieldSize]
	got := strings.TrimRight(strings.Trim(string(raw), "\x00"), " ")
	if got != "FOO" {
		t.Fatalf("voice name = %q, want FOO; raw = %v", got, raw)
	}
}

// TestSnapshot_LayoutRenameVoiceSpaceAndCase pins two FZ-1 voice-name
// constraints: spaces are accepted, and lowercase auto-uppercases.
// Before the fix, the space key arrived as msg.String() == "space"
// and was swallowed by the len(r)==1 guard.
func TestSnapshot_LayoutRenameVoiceSpaceAndCase(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a, windowSize(140, 40), keyPress(testKeyEnter), keyPress(testKeyEnter))
	a = step(t, a,
		keyPress("r"),
		// Type "ab cd"; should land as "AB CD" (auto-upper + space).
		keyPress("a"), keyPress("b"), keyPress(" "),
		keyPress("c"), keyPress("d"),
		keyPress(testKeyEnter),
	)

	off, _ := a.layout.VoiceOffset(0, 0)
	data := a.containerModel.Bytes()
	raw := data[off+disk.VoiceNameOffset : off+disk.VoiceNameOffset+disk.VoiceNameFieldSize]
	got := strings.TrimRight(strings.Trim(string(raw), "\x00"), " ")
	if got != "AB CD" {
		t.Fatalf("voice name = %q, want %q", got, "AB CD")
	}
}

// TestSnapshot_LayoutRenameVoiceCancel pins Esc-to-cancel: opening
// the rename modal and pressing Esc must leave the voice header
// untouched.
func TestSnapshot_LayoutRenameVoiceCancel(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a, windowSize(140, 40), keyPress(testKeyEnter), keyPress(testKeyEnter))
	off, _ := a.layout.VoiceOffset(0, 0)
	before := append([]byte(nil),
		a.containerModel.Bytes()[off+disk.VoiceNameOffset:off+disk.VoiceNameOffset+disk.VoiceNameFieldSize]...)

	a = step(t, a,
		keyPress("r"),
		keyPress("X"), keyPress("Y"), keyPress("Z"),
		keyPress(testKeyEsc),
	)
	if a.renameActive {
		t.Fatalf("rename modal should close on Esc")
	}
	after := a.containerModel.Bytes()[off+disk.VoiceNameOffset : off+disk.VoiceNameOffset+disk.VoiceNameFieldSize]
	if string(before) != string(after) {
		t.Fatalf("voice name field changed after Esc: %v -> %v", before, after)
	}
}

// Renaming voices used to live in Sound to Sample to Name, with two
// tests pinning that flow. The inline rename in Layout's Area list
// supersedes it (see TestSnapshot_LayoutRenameVoiceInline above), so
// those tests have been retired.

func TestSnapshot_SoundEditMode(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a,
		windowSize(140, 40),
		keyPress(testKeyEnter), // open Piano.fzf
		keyPress(testKeyEnter), // open Bank 1
		keyPress(testKeyEnter), // open Area 1
		keyPress(testKeyEnter), // enter edit mode on focused cell
	)
	snaps.MatchSnapshot(t, renderView(a))
}

func TestSnapshot_SoundEditModeAdjustValue(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a,
		windowSize(140, 40),
		keyPress(testKeyEnter), // open Piano.fzf
		keyPress(testKeyEnter), // open Bank 1
		keyPress(testKeyEnter), // open Area 1
		keyPress(testKeyEnter), // enter edit mode
		keyPress("up"),         // adjust value up by one
	)
	// Edit-mode adjust commits via ApplyBatch, so the container should
	// be dirty after this.
	if !a.containerModel.Dirty() {
		t.Fatalf("container should be dirty after edit-mode up adjust")
	}
	snaps.MatchSnapshot(t, renderView(a))
}

func TestSnapshot_DirtyMarkerVisible(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a,
		windowSize(140, 40),
		keyPress(testKeyEnter), keyPress(testKeyEnter), keyPress(testKeyEnter),
		keyPress(testKeyEnter), keyPress("up"),
		keyPress(testKeyEsc), // exit edit mode so the body returns to Sound view
	)
	snaps.MatchSnapshot(t, renderView(a))
}

func TestSnapshot_AuditionWithSelection(t *testing.T) {
	audio.InstallNoopForTest(t)
	dir := fixtureWorkspace(t)
	a := New(dir)
	a = step(t, a,
		windowSize(140, 40),
		keyPress(testKeyEnter), // open Piano
		keyPress(testKeyEnter), // open Bank 1 (selects Bank 1)
		// We are now in the Area list; Bank 1 / Area 1 is selected.
		keyPress(" "), // audition
	)
	snaps.MatchSnapshot(t, renderView(a))
}

// TestSnapshot_WorkspaceHidesUnsupportedFiles pins the rule that only
// supported files (.img / .fzf / .fzv / .wav) plus directories appear
// in the Workspace listing. Anything else is filtered at the source:
// no greyed-out rows, no clutter.
func TestSnapshot_WorkspaceHidesUnsupportedFiles(t *testing.T) {
	dir := t.TempDir()
	supported := []string{"a-voice.fzv", "b-sample.wav", "c-disk.img", "d-dump.fzf"}
	unsupported := []string{"README.md", "notes.txt", "script.sh", "image.png"}
	for _, name := range append(supported, unsupported...) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
			t.Fatalf("seed %q: %v", name, err)
		}
	}
	// A subdirectory must always survive the filter (traversal target).
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	a := New(dir)
	a = step(t, a, windowSize(140, 40))

	view := renderView(a)
	for _, name := range supported {
		if !strings.Contains(view, name) {
			t.Fatalf("Workspace dropped supported file %q:\n%s", name, view)
		}
	}
	if !strings.Contains(view, "subdir/") {
		t.Fatalf("Workspace dropped subdirectory; should always appear:\n%s", view)
	}
	for _, name := range unsupported {
		if strings.Contains(view, name) {
			t.Fatalf("Workspace listed unsupported file %q; should be filtered:\n%s", name, view)
		}
	}
	snaps.MatchSnapshot(t, view)
}

// TestSnapshot_WorkspaceAuditionsWAV pins the rule that Space on a
// .wav row in Workspace plays it directly (no Layout Area selection
// required). We can't actually verify audio output in a test, but we
// can verify the audio engine ends up with the expected VoiceID
// after the Audition call.
func TestSnapshot_WorkspaceAuditionsWAV(t *testing.T) {
	audio.InstallNoopForTest(t)
	dir := t.TempDir()
	src := filepath.Join("..", "..", "..", "testdata", "synthetic", "JUNGLISM Samples", "808.wav")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing wav fixture: %v", err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	target := filepath.Join(dir, "808.wav")
	if err := os.WriteFile(target, data, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("copy fixture: %v", err)
	}
	a := New(dir)
	a = step(t, a, windowSize(140, 40), keyPress(" "))

	view := renderView(a)
	if !strings.Contains(view, "auditioning 808.wav") &&
		!strings.Contains(view, "Audition WAV failed") &&
		!strings.Contains(view, "Audition failed") {
		t.Fatalf("expected an audition status for 808.wav, got:\n%s", view)
	}
	snaps.MatchSnapshot(t, view)
}

// TestSnapshot_LayoutImportOpensPickerOnPool pins the new assign
// flow: in Layout's Area list, pressing `i` opens the Pool in picker
// mode (banner shows the target Area), and the focused space becomes
// Pool. The picker is only available when the pool has entries.
func TestSnapshot_LayoutImportOpensPickerOnPool(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join("..", "..", "..", "testdata", "synthetic", "JUNGLISM Samples", "808.wav")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing wav fixture: %v", err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// Stage a workspace with one wav so we have a pool entry.
	if err := os.WriteFile(filepath.Join(dir, "808.wav"), data, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed wav: %v", err)
	}

	a := New(dir)
	a = step(t, a,
		windowSize(140, 40),
		keyPress(testKeyEnter), // open 808.wav (adds to pool)
		keyPress("shift+down"), // -> Pool
		keyPress("shift+down"), // -> Layout
		keyPress(testKeyEnter), // open Bank 1
		keyPress("i"),          // open picker for Bank 1 / Area 1
	)
	if a.pickingFor == nil {
		t.Fatalf("expected picker to be open after `i`")
	}
	view := renderView(a)
	if !strings.Contains(view, "Picking voice for") {
		t.Fatalf("expected picker banner; got:\n%s", view)
	}
	if !strings.Contains(view, "Bank 1 / Area 1") {
		t.Fatalf("expected target 'Bank 1 / Area 1' in picker; got:\n%s", view)
	}
	snaps.MatchSnapshot(t, view)
}

// TestSnapshot_AssignKeepsLayoutCursorOnArea pins the rule that after
// an import completes, Layout stays on the Area page where the user
// invoked `i` (not bumped back to the Bank list). Tests that the
// cursor remains on the same Area index too.
func TestSnapshot_AssignKeepsLayoutCursorOnArea(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join("..", "..", "..", "testdata", "synthetic", "JUNGLISM Samples", "808.wav")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing wav fixture: %v", err)
	}
	data, _ := os.ReadFile(src)
	if err := os.WriteFile(filepath.Join(dir, "808.wav"), data, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed: %v", err)
	}

	a := New(dir)
	a = step(t, a,
		windowSize(140, 40),
		keyPress(testKeyEnter), // open 808.wav (into pool)
		keyPress("shift+down"), // -> Pool
		keyPress("shift+down"), // -> Layout
		keyPress(testKeyEnter), // open Bank 1
		keyPress("down"),       // cursor at A02
		keyPress("down"),       // cursor at A03
		keyPress("i"),          // open picker for Bank 1 / Area 3
		keyPress(testKeyEnter), // assign
	)

	if a.pickingFor != nil {
		t.Fatalf("expected picker closed after assign")
	}
	if a.current != minimap.Layout {
		t.Fatalf("expected focus on Layout after assign; got %v", a.current)
	}
	bank, area, ok := a.layout.SelectedArea()
	if !ok {
		t.Fatalf("expected Layout to be in Area view (inBank=true) after assign")
	}
	if bank != 0 || area != 2 {
		t.Fatalf("expected cursor on Bank 1 / Area 3 (0,2); got (%d,%d)", bank, area)
	}
	snaps.MatchSnapshot(t, renderView(a))
}

// TestSnapshot_PickerCancelReturnsToLayout pins the Esc behaviour
// inside the picker: it closes the picker without assigning, and
// focus returns to Layout.
func TestSnapshot_PickerCancelReturnsToLayout(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join("..", "..", "..", "testdata", "synthetic", "JUNGLISM Samples", "808.wav")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing wav fixture: %v", err)
	}
	data, _ := os.ReadFile(src)
	if err := os.WriteFile(filepath.Join(dir, "808.wav"), data, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed: %v", err)
	}
	a := New(dir)
	a = step(t, a,
		windowSize(140, 40),
		keyPress(testKeyEnter),
		keyPress("shift+down"),
		keyPress("shift+down"),
		keyPress(testKeyEnter),
		keyPress("i"),
		keyPress(testKeyEsc), // cancel
	)
	if a.pickingFor != nil {
		t.Fatalf("expected picker closed after Esc; pickingFor = %+v", a.pickingFor)
	}
	if a.current != minimap.Layout {
		t.Fatalf("expected focus on Layout after cancel; got %v", a.current)
	}
	view := renderView(a)
	if strings.Contains(view, "Picking voice for") {
		t.Fatalf("picker banner should be gone after cancel:\n%s", view)
	}
}

func TestSnapshot_AuditionWithoutSelection(t *testing.T) {
	dir := fixtureWorkspace(t)
	a := New(dir)
	// Pressing Space (Audition) without an Area selected should hand
	// back a hint, not crash.
	a = step(t, a,
		windowSize(140, 40),
		keyPress(" "),
	)
	snaps.MatchSnapshot(t, renderView(a))
}
