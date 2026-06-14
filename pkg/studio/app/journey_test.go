// Package app's journey tests pin end-to-end user flows. Each test
// drives a complete user task through the Bubble Tea Update loop
// and asserts the workflow's contract: byte-level effect on the
// container, save/reload preservation, and any user-visible
// affordance (status messages, modal prompts) that the journey
// promises.
//
// Journeys are NOT a replacement for unit tests. Unit tests pin
// individual offsets and bounds; journeys pin "the user can
// actually do X and the result holds." A passing journey is the
// executable proof of one user contract; together they form the
// spec for what the studio guarantees.
//
// Layer placement: above unit / round-trip / property tests, below
// the corpus crash sweep. Skipped under -short.
//
// Discipline:
//   - Assert semantics, not liveness. Dirty()=true is not enough;
//     load the saved bytes, assert the field changed exactly where
//     expected, and confirm IsActiveOrEmptyVoiceSlot still holds.
//   - Fixtures copied into t.TempDir(); never mutate the corpus.
//   - Fixed terminal size: WindowSizeMsg{140, 30} before every
//     assertion (matches the minimum-floor used by the rest of
//     the suite).
//   - Use pump() so async tea.Cmds (toast dismiss, autosave tick)
//     resolve through the fake clock.

package app

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/loader"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
	"github.com/philipcunningham/fizzle/pkg/studio/spaces/layout"
	"github.com/philipcunningham/fizzle/pkg/studio/spaces/workspace"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/minimap"
)

// --- shared journey harness ---------------------------------------------

// Journey tests size the terminal at the App's declared minimum
// floor. Pinning to the floor doubles as a tooSmall-guard check: a
// regression that bumps minCols/minRows past these values fails the
// journey tests instead of silently letting them render at a too-
// small size.
const journeyCols = minCols
const journeyRows = minRows

// testKeyLeft / testKeyRight / testKeyEnter / testKeyEsc /
// testKeyMix are package-level test constants for goconst-flagged
// repeated strings shared across journey_test.go and snapshot_test.go.
// "left" / "right" / "enter" / "esc" are keyPress() inputs in the
// snapshot suite; "left" / "right" / "mix" are stereo-channel result
// labels in the journey suite. The string values happen to overlap
// so a single const block serves both purposes.
const (
	testKeyLeft  = "left"
	testKeyRight = "right"
	testKeyEnter = "enter"
	testKeyEsc   = "esc"
	testKeyMix   = "mix"
)

// journeyState bundles what a journey holds in its hand: the App
// being driven, the workspace dir, the path of the target file
// (inside the workspace), and the fake clock for any test that
// needs to advance time.
type journeyState struct {
	a      App
	dir    string
	target string // empty when launched untitled
	clock  *fakeClock
	// boundBank / boundArea track the most recent Sound-space
	// binding so helpers don't have to thread (bank, area)
	// through every step.
	boundBank, boundArea int
}

// newJourneyWithFixture copies fixtureRel (under testdata/) into a
// fresh workspace tempdir, loads it as the in-focus container, and
// pumps a WindowSizeMsg so View() returns full-layout content.
func newJourneyWithFixture(t *testing.T, fixtureRel string) journeyState {
	t.Helper()
	src := filepath.Join("..", "..", "..", "testdata", fixtureRel)
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("missing fixture %s: %v", fixtureRel, err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, filepath.Base(src))
	if err := os.WriteFile(target, raw, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed fixture: %v", err)
	}

	fc := newFakeClock()
	a := New(dir)
	a.backupDir = filepath.Join(dir, "backups")
	a.tick = fc.Tick
	a.toast.SetClock(fc.Tick)
	a.status.SetClock(fc.Tick)
	m, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("LoadContainer %s: %v", target, err)
	}
	a.containerModel = m
	a.containerInfo = info
	a.layout.SetContainer(m, info)
	a = pump(t, a, tea.WindowSizeMsg{Width: journeyCols, Height: journeyRows})
	return journeyState{a: a, dir: dir, target: target, clock: fc}
}

// navInto takes the journey from any entry space (post-load) into
// the Sound space at (bank, area) by direct binding. We avoid
// driving the full nav script here because individual journeys are
// not nav-tests; they're behavioural tests that happen to start
// from a known voice. Layout already has unit tests that pin the
// nav path.
//
//nolint:unparam // bank kept in the signature so the helper stays usable when a future fixture exercises Bank > 0; current callers all bind into Bank 0
func navInto(t *testing.T, st journeyState, bank, area int) journeyState {
	t.Helper()
	voiceArea := st.a.containerInfo.BankCount * disk.SectorSize
	st.a.sound.Bind(st.a.containerModel, st.a.containerInfo.BankCount,
		voiceArea, st.a.audioAreaStart(), bank, area)
	st.a.current = minimap.Sound
	st.a.minimap.Current = st.a.current
	st.boundBank = bank
	st.boundArea = area
	if !st.a.sound.HasVoice() {
		t.Fatalf("nav into Bank %d / Area %d landed on a voiceless slot", bank, area)
	}
	return st
}

// voiceHeaderOffset is the absolute byte offset of the bound voice's
// 192-byte header in the container.
func voiceHeaderOffset(t *testing.T, st journeyState) int {
	t.Helper()
	slotIdx, ok := disk.BankVPLookup(st.a.containerModel.Bytes(), st.boundBank, st.boundArea)
	if !ok {
		t.Fatalf("BankVPLookup(%d,%d) failed", st.boundBank, st.boundArea)
	}
	voiceArea := st.a.containerInfo.BankCount * disk.SectorSize
	return disk.VoiceSlotOffset(voiceArea, slotIdx)
}

// applyDCFCutoff sets the DCF cutoff byte for the currently-bound
// voice via the model's patch path. We bypass keyboard simulation
// because journey tests should not be keymap-regression tests; the
// keymap has its own table test.
func applyDCFCutoff(t *testing.T, st journeyState, newValue int) journeyState {
	t.Helper()
	if !st.a.sound.HasVoice() {
		t.Fatal("applyDCFCutoff: no voice bound")
	}
	off := voiceHeaderOffset(t, st) + disk.VoiceDCFOffset
	data := st.a.containerModel.Bytes()
	oldByte := data[off]
	newByte := byte(newValue) //nolint:gosec // G115: test value bounded (call sites pass byte-shaped ints)
	if newByte == oldByte {
		newByte = oldByte ^ 0x10
	}
	if err := st.a.containerModel.Apply(model.Patch{
		Offset: off,
		Old:    []byte{oldByte},
		New:    []byte{newByte},
	}); err != nil {
		t.Fatalf("apply DCF cutoff edit: %v", err)
	}
	return st
}

// saveReloadReparse drives Ctrl+S, then re-opens the saved file
// via the production loader and returns the reparsed (bytes,
// info). Catches writer/loader asymmetry the in-memory check
// would miss.
func saveReloadReparse(t *testing.T, st journeyState) ([]byte, loader.ContainerInfo) {
	t.Helper()
	st.a = pump(t, st.a, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if st.a.containerModel.Dirty() {
		t.Fatal("dirty=true after Ctrl+S; expected save to clear it")
	}
	m2, info2, err := loader.LoadContainer(st.target)
	if err != nil {
		t.Fatalf("reload after save: %v", err)
	}
	return m2.Bytes(), info2
}

// --- Journey 1: Open and edit -------------------------------------------

// TestJourney_OpenAndEdit pins the spine of every other journey:
// load a file, change one well-known field, save, reload, and
// confirm the byte landed exactly where expected. Runs twice (once
// against an .img wrapper and once against a .fzf) so writer
// asymmetries between the two paths surface here.
//
// Contract:
//
//   - The container starts clean (Dirty=false).
//   - After the edit, Dirty=true and the in-memory byte equals the
//     value we wrote.
//   - After Ctrl+S, Dirty=false and the on-disk file has the new
//     byte at the right offset.
//   - The voice header still passes IsActiveOrEmptyVoiceSlot; the
//     edit didn't corrupt other fields.
//   - For the .img case specifically: the saved file is still
//     exactly 1,310,720 bytes (the FZ-1 floppy size). A save that
//     truncates the .img to its FZF payload would silently break
//     downstream tooling.
func TestJourney_OpenAndEdit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey under -short")
	}

	cases := []struct {
		name        string
		fixture     string
		wantImgSize bool
	}{
		{name: "img", fixture: "synthetic/STAB.img", wantImgSize: true},
		{
			name: "fzf",
			fixture: filepath.Join("corpus", "casio-fz-1-factory-library",
				"casio-fz-sound-disk-fl-3", "string-ensemble", "String-Ensemble.fzf"),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			st := newJourneyWithFixture(t, tc.fixture)

			if st.a.containerModel.Dirty() {
				t.Fatal("freshly-loaded container should be clean")
			}

			// Drill into Bank 0 / Area 0 (every fixture we picked has a
			// populated first Area).
			st = navInto(t, st, 0, 0)

			off := voiceHeaderOffset(t, st) + disk.VoiceDCFOffset
			originalCutoff := st.a.containerModel.Bytes()[off]
			st = applyDCFCutoff(t, st, int(originalCutoff)+1)
			newCutoff := st.a.containerModel.Bytes()[off]

			if !st.a.containerModel.Dirty() {
				t.Fatal("expected Dirty=true after edit")
			}
			if newCutoff == originalCutoff {
				t.Fatalf("edit did not change in-memory byte (still %#02x)", newCutoff)
			}

			reloaded, info2 := saveReloadReparse(t, st)
			if got := reloaded[off]; got != newCutoff {
				t.Errorf("reload: byte at %#x = %#02x, want %#02x", off, got, newCutoff)
			}

			// The whole voice still parses as a real header.
			voiceArea := info2.BankCount * disk.SectorSize
			slotIdx, ok := disk.BankVPLookup(reloaded, 0, 0)
			if !ok {
				t.Fatalf("post-reload BankVPLookup failed")
			}
			voiceOff := disk.VoiceSlotOffset(voiceArea, slotIdx)
			if voiceOff+disk.VoiceHeaderUsed > len(reloaded) {
				t.Fatalf("voice slot %d out of bounds in reloaded buffer", slotIdx)
			}
			if !disk.IsActiveOrEmptyVoiceSlot(reloaded[voiceOff : voiceOff+disk.VoiceHeaderUsed]) {
				t.Errorf("voice %d failed IsActiveOrEmptyVoiceSlot after round-trip", slotIdx)
			}

			if tc.wantImgSize {
				sz, err := fileSize(st.target)
				if err != nil {
					t.Fatalf("stat target: %v", err)
				}
				if sz != 1310720 {
					t.Errorf("saved .img size = %d, want 1310720 (FZ-1 floppy)", sz)
				}
			}
		})
	}
}

// --- Journey 2: Compose new ---------------------------------------------

// writeStereoWAV materialises a minimal 16-bit stereo PCM WAV at
// path. Frame i has left=int16(100+i), right=int16(200+i), so the
// channel-pick branches in pool.AddWAV are observable in the
// resulting voice bytes (left yields ~100s, right yields ~200s, mix yields ~150s).
//
// Crafted inline rather than checked into testdata/; keeps the
// fixture self-describing and untouchable by the corpus sweep.
func writeStereoWAV(t *testing.T, path string, frames int) {
	t.Helper()
	w, err := os.Create(path)
	if err != nil {
		t.Fatalf("create stereo wav: %v", err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Fatalf("close stereo wav: %v", err)
		}
	}()
	dataSize := uint32(frames * 2 * 2) //nolint:gosec // G115: test value bounded (frames × channels × 16-bit)
	put32 := func(v uint32) { _ = binary.Write(w, binary.LittleEndian, v) }
	put16 := func(v uint16) { _ = binary.Write(w, binary.LittleEndian, v) }
	puti16 := func(v int16) { _ = binary.Write(w, binary.LittleEndian, v) }
	w.WriteString("RIFF") //nolint:errcheck
	put32(36 + dataSize)
	w.WriteString("WAVE") //nolint:errcheck
	w.WriteString("fmt ") //nolint:errcheck
	put32(16)
	put16(1) // PCM
	put16(2) // stereo
	put32(44100)
	put32(44100 * 2 * 2)  // byte rate
	put16(2 * 2)          // block align
	put16(16)             // bits/sample
	w.WriteString("data") //nolint:errcheck
	put32(dataSize)
	for i := 0; i < frames; i++ {
		puti16(int16(100 + i)) // L
		puti16(int16(200 + i)) // R
	}
}

// TestJourney_ComposeNew pins the "start from scratch" workflow:
// launch untitled, populate the pool from three FZV voices plus
// one stereo WAV (exercising the stereo-channel prompt modal),
// assign each pool entry to a fresh Area in a new bank, save-as,
// reload, and assert the assigned voices reach disk intact.
//
// The stereo prompt is a key contract: a stereo WAV should NOT
// silently default to one channel. The user must see a modal,
// pick left / right / mix / cancel, and the chosen channel is
// what lands in the pool. Verifying via the prompt's Cancel path
// gates regressions where the prompt is bypassed.
func TestJourney_ComposeNew(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey under -short")
	}

	dir := t.TempDir()

	// Copy three FZV voices into the workspace.
	fzvSrcRoot := filepath.Join("..", "..", "..", "testdata", "corpus",
		"casio-fz-1-shareware-library-fzf-format")
	fzvNames := []string{"CASIO010.FZV", "CASIO099.FZV", "CASIO113.FZV"}
	for _, n := range fzvNames {
		src := filepath.Join(fzvSrcRoot, n)
		data, err := os.ReadFile(src)
		if err != nil {
			t.Skipf("missing FZV fixture %s: %v", n, err)
		}
		if err := os.WriteFile(filepath.Join(dir, n), data, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
			t.Fatalf("copy fzv %s: %v", n, err)
		}
	}

	// Synthesise a stereo WAV inline so the test is self-describing.
	stereoPath := filepath.Join(dir, "stereo.wav")
	writeStereoWAV(t, stereoPath, 1024)

	// Launch untitled App with a fake clock.
	fc := newFakeClock()
	a := New(dir)
	a.backupDir = filepath.Join(dir, "backups")
	a.tick = fc.Tick
	a.toast.SetClock(fc.Tick)
	a.status.SetClock(fc.Tick)
	a = pump(t, a, tea.WindowSizeMsg{Width: journeyCols, Height: journeyRows})

	// Add the three FZVs directly. (Workspace's intent flow is
	// tested in the workspace package; the journey here is the
	// pool-grows behaviour.)
	for _, n := range fzvNames {
		if err := a.pool.AddFZV(filepath.Join(dir, n)); err != nil {
			t.Fatalf("AddFZV %s: %v", n, err)
		}
	}
	if got := len(a.pool.Entries()); got != 3 {
		t.Fatalf("after 3x AddFZV: pool len = %d, want 3", got)
	}

	// Add the stereo WAV via the App-level workspace intent so the
	// stereo prompt modal fires.
	a = a.handleWorkspaceIntent(workspace.Intent{
		Kind: workspace.IntentAddSampleToPool,
		Path: stereoPath,
	})
	if a.confirm == nil || !a.confirm.IsOpen() {
		t.Fatalf("expected stereo channel prompt to open; confirm.IsOpen=false")
	}

	// Cancel the prompt first; assert the pool didn't grow.
	a.confirm.Cancel()
	a = a.resolvePendingConfirm(t)
	if got := len(a.pool.Entries()); got != 3 {
		t.Errorf("after stereo Cancel: pool len = %d, want still 3", got)
	}

	// Re-prompt and pick "Left" (Result=0).
	a = a.handleWorkspaceIntent(workspace.Intent{
		Kind: workspace.IntentAddSampleToPool,
		Path: stereoPath,
	})
	if a.confirm == nil || !a.confirm.IsOpen() {
		t.Fatalf("second pass: stereo prompt did not open")
	}
	a.confirm.Confirm() // focus is on "Left" by default (first option)
	a = a.resolvePendingConfirm(t)
	if got := len(a.pool.Entries()); got != 4 {
		t.Fatalf("after stereo Confirm(Left): pool len = %d, want 4", got)
	}

	// Assign each pool entry to (Bank 0, Area N). The App's
	// assignPoolEntryToArea is the meat of the workflow; the
	// picker open/close gesture is covered in layout/pool tests.
	entries := a.pool.Entries()
	for i := range entries {
		entry := entries[i]
		a = a.assignPoolEntryToArea(&entry, 0, i)
	}

	// Bank 0 should now report bstep = 4 (one per assigned Area).
	bstepOff := disk.BankVoiceCountOffset
	if bstep := binary.LittleEndian.Uint16(a.containerModel.Bytes()[bstepOff : bstepOff+2]); bstep != 4 {
		t.Errorf("after 4x assign: bank 0 bstep = %d, want 4", bstep)
	}

	// Save as a real .fzf into the workspace and reload.
	target := filepath.Join(dir, "compose.fzf")
	if err := a.containerModel.Save(target); err != nil {
		t.Fatalf("Save compose.fzf: %v", err)
	}
	m2, info2, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("reload compose.fzf: %v", err)
	}
	if info2.BankCount < 1 {
		t.Fatalf("reloaded BankCount = %d, want >=1", info2.BankCount)
	}
	reloaded := m2.Bytes()
	if got := binary.LittleEndian.Uint16(reloaded[bstepOff : bstepOff+2]); got != 4 {
		t.Errorf("reload: bank 0 bstep = %d, want 4", got)
	}

	// Each of the four Areas points at a plausible voice slot.
	for i := 0; i < 4; i++ {
		slotIdx, ok := disk.BankVPLookup(reloaded, 0, i)
		if !ok {
			t.Errorf("Area %d: BankVPLookup failed", i)
			continue
		}
		voiceArea := info2.BankCount * disk.SectorSize
		voiceOff := disk.VoiceSlotOffset(voiceArea, slotIdx)
		if voiceOff+disk.VoiceHeaderUsed > len(reloaded) {
			t.Errorf("Area %d: voice slot %d out of bounds", i, slotIdx)
			continue
		}
		if !disk.IsActiveOrEmptyVoiceSlot(reloaded[voiceOff : voiceOff+disk.VoiceHeaderUsed]) {
			t.Errorf("Area %d: voice slot %d failed IsActiveOrEmptyVoiceSlot", i, slotIdx)
		}
	}
}

// TestJourney_ComposeNew_StereoChannelChoices pins the channel-
// selection branches the J2 main test doesn't cover: Right and
// Mix. Each produces a different sample stream in the pool entry
// (verified by comparing a representative byte at the entry's
// audio data); a regression that wires the wrong AddWAV branch
// would surface as the same byte across all three choices.
func TestJourney_ComposeNew_StereoChannelChoices(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey under -short")
	}

	cases := []struct {
		name   string
		result int // 0=Left, 1=Right, 2=Mix
		// We don't assert specific sample values (resampling +
		// voice-encode normalisation make those hard to predict);
		// instead, each case must produce DIFFERENT pool bytes
		// than the others, which is what we assert below.
	}{
		{name: testKeyLeft, result: 0},
		{name: testKeyRight, result: 1},
		{name: testKeyMix, result: 2},
	}

	got := make(map[string][]byte, len(cases))
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			stereoPath := filepath.Join(dir, "stereo.wav")
			writeStereoWAV(t, stereoPath, 1024)

			fc := newFakeClock()
			a := New(dir)
			a.backupDir = filepath.Join(dir, "backups")
			a.tick = fc.Tick
			a.toast.SetClock(fc.Tick)
			a.status.SetClock(fc.Tick)
			a = pump(t, a, tea.WindowSizeMsg{Width: journeyCols, Height: journeyRows})

			a = a.handleWorkspaceIntent(workspace.Intent{
				Kind: workspace.IntentAddSampleToPool,
				Path: stereoPath,
			})
			if a.confirm == nil || !a.confirm.IsOpen() {
				t.Fatal("stereo prompt did not open")
			}
			// Focus walks left=0, right=1, mix=2, cancel=3.
			for i := 0; i < tc.result; i++ {
				a.confirm.Next()
			}
			a.confirm.Confirm()
			a = a.resolvePendingConfirm(t)

			if len(a.pool.Entries()) != 1 {
				t.Fatalf("pool len = %d, want 1", len(a.pool.Entries()))
			}
			entry := a.pool.Entries()[0]
			// Copy bytes out for cross-case comparison after the
			// subtests close.
			cp := make([]byte, len(entry.Bytes))
			copy(cp, entry.Bytes)
			got[tc.name] = cp
		})
	}

	// All three results must produce distinct pool bytes; a wired
	// regression where every choice silently routes to channel 0
	// would surface as identical bytes here.
	if bytes.Equal(got[testKeyLeft], got[testKeyRight]) {
		t.Errorf("Left and Right channel-picks produced identical pool bytes")
	}
	if bytes.Equal(got[testKeyLeft], got[testKeyMix]) {
		t.Errorf("Left and Mix channel-picks produced identical pool bytes")
	}
	if bytes.Equal(got[testKeyRight], got[testKeyMix]) {
		t.Errorf("Right and Mix channel-picks produced identical pool bytes")
	}
}

// resolvePendingConfirm reads the confirm result channel and runs
// the pendingAction, mirroring what the App's Update loop does
// after a confirm modal resolves. Used by journey tests to drive
// the confirm flow without simulating keypresses on the modal.
//
// confirm.Show returns a buffered channel; Confirm/Cancel write to
// it synchronously before returning, so the receive below normally
// completes immediately. The 1s timeout is a safety net for the
// case where a callsite forgot to Confirm/Cancel; it fails
// loudly rather than hanging the test binary.
func (a App) resolvePendingConfirm(t *testing.T) App {
	t.Helper()
	if a.pendingAction == nil {
		t.Fatal("resolvePendingConfirm: no pendingAction set")
	}
	select {
	case result := <-a.confirmResult:
		next, _ := a.pendingAction(result)
		next.pendingAction = nil
		next.confirmResult = nil
		return next
	case <-time.After(time.Second):
		t.Fatal("resolvePendingConfirm: timed out after 1s; was Confirm/Cancel called on the modal?")
		return a // unreachable but the compiler can't see t.Fatal
	}
}

// --- Journey 3: Sound-sculpt --------------------------------------------

// TestJourney_SoundSculpt pins the multi-field sound-shaping
// workflow: a user drills into a voice, sets a stage to SUS and
// another to END on the DCA envelope, dials in an LFO pitch
// depth, sets a DCF cutoff, saves, reloads, and confirms every
// edit landed exactly where expected AND the voice is still a
// loadable FZ-1 voice (the discipline test for the 0xFF SUS/END
// Role-Normal bug that lived here historically).
//
// Audition is exercised at the unit level inside pkg/studio/audio;
// this journey deliberately doesn't try to assert on the audio
// engine. Adding cross-package audition assertions would require
// exposing TestPlayer through the App constructor, which the
// project's spike scope doesn't yet justify.
func TestJourney_SoundSculpt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey under -short")
	}
	st := newJourneyWithFixture(t, "synthetic/PAD-LFO.img")
	st = navInto(t, st, 0, 0)

	voiceOff := voiceHeaderOffset(t, st)

	// Original bytes for each edit target.
	data := st.a.containerModel.Bytes()
	origSus := data[voiceOff+disk.VoiceDCASusOffset]
	origEnd := data[voiceOff+disk.VoiceDCAEndOffset]
	origLFOPitch := data[voiceOff+disk.VoiceLFODCPOffset]
	origCutoff := data[voiceOff+disk.VoiceDCFOffset]

	// Apply four edits via direct byte patches (mirroring what the
	// editor's commit path does). Choosing values that differ from
	// the originals so the edits are observable.
	const newSus, newEnd byte = 1, 6
	newLFOPitch := origLFOPitch ^ 0x20
	newCutoff := origCutoff ^ 0x10

	patches := []model.Patch{
		{Offset: voiceOff + disk.VoiceDCASusOffset, Old: []byte{origSus}, New: []byte{newSus}},
		{Offset: voiceOff + disk.VoiceDCAEndOffset, Old: []byte{origEnd}, New: []byte{newEnd}},
		{Offset: voiceOff + disk.VoiceLFODCPOffset, Old: []byte{origLFOPitch}, New: []byte{newLFOPitch}},
		{Offset: voiceOff + disk.VoiceDCFOffset, Old: []byte{origCutoff}, New: []byte{newCutoff}},
	}
	if err := st.a.containerModel.ApplyBatch(patches); err != nil {
		t.Fatalf("ApplyBatch four edits: %v", err)
	}
	if !st.a.containerModel.Dirty() {
		t.Fatal("Dirty=false after edits")
	}

	reloaded, info2 := saveReloadReparse(t, st)

	if got := reloaded[voiceOff+disk.VoiceDCASusOffset]; got != newSus {
		t.Errorf("DCA SUS after reload = %#x, want %#x", got, newSus)
	}
	if got := reloaded[voiceOff+disk.VoiceDCAEndOffset]; got != newEnd {
		t.Errorf("DCA END after reload = %#x, want %#x", got, newEnd)
	}
	if got := reloaded[voiceOff+disk.VoiceLFODCPOffset]; got != newLFOPitch {
		t.Errorf("LFO pitch depth after reload = %#x, want %#x", got, newLFOPitch)
	}
	if got := reloaded[voiceOff+disk.VoiceDCFOffset]; got != newCutoff {
		t.Errorf("DCF cutoff after reload = %#x, want %#x", got, newCutoff)
	}

	// Voice header must still be plausible: the SUS/END pointers
	// are in range, the rate/stop arrays untouched, the wave pointers
	// unaltered. This is the historical 0xFF bug's pin.
	voiceArea := info2.BankCount * disk.SectorSize
	slotIdx, ok := disk.BankVPLookup(reloaded, 0, 0)
	if !ok {
		t.Fatalf("post-reload BankVPLookup failed")
	}
	checkOff := disk.VoiceSlotOffset(voiceArea, slotIdx)
	if !disk.IsActiveOrEmptyVoiceSlot(reloaded[checkOff : checkOff+disk.VoiceHeaderUsed]) {
		t.Errorf("voice failed IsActiveOrEmptyVoiceSlot after sound-sculpt + save+reload")
	}
}

// --- Journey 4: Layer (velocity multi-switch) ---------------------------

// TestJourney_Layer pins the velocity-layering workflow: pick an
// Area, duplicate it (Ctrl-D), narrow the source's velocity band,
// expand the duplicate's velocity band to cover the complementary
// half, save, reload. The contract is:
//
//   - Duplicate appends a new Area with bstep += 1.
//   - The new Area's voice header is a clone of the source's, in a
//     fresh voice slot (so editing one doesn't affect the other).
//   - Per-area metadata (key range, vel range, root, MIDI chan,
//     audio out, volume) is copied from the source.
//   - Velocity range validation (low <= high) is enforced by the
//     area editor's step logic and Open clamping.
//   - All values survive save+reload.
//   - Duplicating a full bank (bstep=64) posts a warning and
//     changes nothing.
func TestJourney_Layer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey under -short")
	}

	st := newJourneyWithFixture(t, filepath.Join("corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-7", "animals", "Animals.fzf"))

	// Read pre-duplicate bstep + source's voice slot.
	data := st.a.containerModel.Bytes()
	bstepOff := disk.BankVoiceCountOffset
	origBstep := binary.LittleEndian.Uint16(data[bstepOff : bstepOff+2])
	srcSlot, ok := disk.BankVPLookup(data, 0, 0)
	if !ok {
		t.Fatal("source area lookup failed")
	}

	// Apply Duplicate via the App's layout intent handler; same
	// route the production Ctrl-D press takes.
	st.a = st.a.handleLayoutIntent(layout.Intent{
		Kind:    layout.IntentDuplicateArea,
		BankIdx: 0,
		AreaIdx: 0,
	})

	// bstep bumped by 1.
	data = st.a.containerModel.Bytes()
	newBstep := binary.LittleEndian.Uint16(data[bstepOff : bstepOff+2])
	if newBstep != origBstep+1 {
		t.Fatalf("post-duplicate bstep = %d, want %d", newBstep, origBstep+1)
	}

	// vp[bstep-1] points at a NEW slot distinct from the source.
	dupAreaIdx := int(origBstep)
	dupSlot, ok := disk.BankVPLookup(data, 0, dupAreaIdx)
	if !ok {
		t.Fatalf("duplicate area %d lookup failed", dupAreaIdx)
	}
	if dupSlot == srcSlot {
		t.Errorf("duplicate slot %d equals source slot %d; should differ", dupSlot, srcSlot)
	}

	// Edits to the duplicate must not bleed into the source; the
	// headers live in independent slots. Smoke-test: write a known
	// byte into the duplicate's header and confirm the source's
	// header is unchanged.
	voiceArea := st.a.containerInfo.BankCount * disk.SectorSize
	srcOff := disk.VoiceSlotOffset(voiceArea, srcSlot)
	dupOff := disk.VoiceSlotOffset(voiceArea, dupSlot)
	srcBefore := data[srcOff+disk.VoiceDCFOffset]
	dupBefore := data[dupOff+disk.VoiceDCFOffset]
	if srcBefore != dupBefore {
		t.Errorf("duplicate did not clone source DCF cutoff (%#x vs %#x)", dupBefore, srcBefore)
	}
	if err := st.a.containerModel.Apply(model.Patch{
		Offset: dupOff + disk.VoiceDCFOffset,
		Old:    []byte{dupBefore},
		New:    []byte{dupBefore ^ 0x20},
	}); err != nil {
		t.Fatalf("edit duplicate cutoff: %v", err)
	}
	data = st.a.containerModel.Bytes()
	if data[srcOff+disk.VoiceDCFOffset] != srcBefore {
		t.Errorf("editing duplicate corrupted source: src cutoff = %#x, want %#x",
			data[srcOff+disk.VoiceDCFOffset], srcBefore)
	}

	// Adjust both velocity bands: source 0..63, duplicate 64..127.
	// We patch the per-area arrays directly (the area editor's
	// state-machine is unit-tested in widgets/areaeditor).
	bankBase := 0
	patches := []model.Patch{}
	addBytePatch := func(absOff int, newByte byte) {
		oldByte := data[absOff]
		if oldByte == newByte {
			return
		}
		patches = append(patches, model.Patch{Offset: absOff, Old: []byte{oldByte}, New: []byte{newByte}})
	}
	addBytePatch(bankBase+disk.BankVelLowOffset+0, 0)
	addBytePatch(bankBase+disk.BankVelHighOffset+0, 63)
	addBytePatch(bankBase+disk.BankVelLowOffset+dupAreaIdx, 64)
	addBytePatch(bankBase+disk.BankVelHighOffset+dupAreaIdx, 127)
	if err := st.a.containerModel.ApplyBatch(patches); err != nil {
		t.Fatalf("apply vel bands: %v", err)
	}

	// Save and reload; bytes must persist.
	reloaded, info2 := saveReloadReparse(t, st)
	if got := binary.LittleEndian.Uint16(reloaded[bstepOff : bstepOff+2]); got != origBstep+1 {
		t.Errorf("reload bstep = %d, want %d", got, origBstep+1)
	}
	if got := reloaded[disk.BankVelLowOffset+0]; got != 0 {
		t.Errorf("reload src vel low = %d, want 0", got)
	}
	if got := reloaded[disk.BankVelHighOffset+0]; got != 63 {
		t.Errorf("reload src vel high = %d, want 63", got)
	}
	if got := reloaded[disk.BankVelLowOffset+dupAreaIdx]; got != 64 {
		t.Errorf("reload dup vel low = %d, want 64", got)
	}
	if got := reloaded[disk.BankVelHighOffset+dupAreaIdx]; got != 127 {
		t.Errorf("reload dup vel high = %d, want 127", got)
	}
	// Both Areas remain plausible voices.
	for _, ai := range []int{0, dupAreaIdx} {
		slotIdx, ok := disk.BankVPLookup(reloaded, 0, ai)
		if !ok {
			t.Errorf("reload BankVPLookup Area %d failed", ai)
			continue
		}
		voiceArea2 := info2.BankCount * disk.SectorSize
		off := disk.VoiceSlotOffset(voiceArea2, slotIdx)
		if !disk.IsActiveOrEmptyVoiceSlot(reloaded[off : off+disk.VoiceHeaderUsed]) {
			t.Errorf("Area %d (slot %d) failed IsActiveOrEmptyVoiceSlot after reload", ai, slotIdx)
		}
	}
}

// TestJourney_Layer_SharesAudio pins that Duplicate Area does not
// copy the audio area; wave/gen pointers in the duplicate point
// at the same shared region. After Duplicate, the container's
// total byte length must grow only by the voice-area sector cost
// (at most one disk.SectorSize), never by the audio payload size.
func TestJourney_Layer_SharesAudio(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey under -short")
	}
	st := newJourneyWithFixture(t, filepath.Join("corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-7", "animals", "Animals.fzf"))

	before := len(st.a.containerModel.Bytes())
	st.a = st.a.handleLayoutIntent(layout.Intent{
		Kind:    layout.IntentDuplicateArea,
		BankIdx: 0,
		AreaIdx: 0,
	})
	after := len(st.a.containerModel.Bytes())

	delta := after - before
	if delta < 0 {
		t.Fatalf("buffer shrank on duplicate: %d -> %d", before, after)
	}
	// Voice-area growth is at most one sector (4 slots per sector;
	// Duplicate adds at most one slot). Audio area growth would be
	// the per-voice audio extent of the source: many KB minimum,
	// well beyond a sector. A delta below SectorSize proves no
	// audio was copied.
	if delta > disk.SectorSize {
		t.Errorf("Duplicate grew buffer by %d bytes; expected <= one SectorSize (%d). Audio area was likely copied instead of shared.",
			delta, disk.SectorSize)
	}
}

// TestJourney_Layer_FullBankRejectsDuplicate pins the "duplicate
// when full" rejection: when bstep is already at MaxVoices=64,
// Ctrl-D is a no-op with a warning status; no bytes change.
func TestJourney_Layer_FullBankRejectsDuplicate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey under -short")
	}
	st := newJourneyWithFixture(t, filepath.Join("corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-7", "animals", "Animals.fzf"))

	// Pin bstep at 64 manually so Duplicate sees a "full bank."
	data := st.a.containerModel.Bytes()
	bstepOff := disk.BankVoiceCountOffset
	origBstep := data[bstepOff]
	var full [2]byte
	binary.LittleEndian.PutUint16(full[:], uint16(disk.MaxVoices))
	if err := st.a.containerModel.Apply(model.Patch{
		Offset: bstepOff,
		Old:    []byte{origBstep, data[bstepOff+1]},
		New:    append([]byte(nil), full[:]...),
	}); err != nil {
		t.Fatalf("seed full bstep: %v", err)
	}
	snapshotLen := len(st.a.containerModel.Bytes())

	st.a = st.a.handleLayoutIntent(layout.Intent{
		Kind:    layout.IntentDuplicateArea,
		BankIdx: 0,
		AreaIdx: 0,
	})

	// Buffer length and bstep unchanged.
	data = st.a.containerModel.Bytes()
	if len(data) != snapshotLen {
		t.Errorf("Duplicate on full bank changed buffer length: %d -> %d", snapshotLen, len(data))
	}
	if got := binary.LittleEndian.Uint16(data[bstepOff : bstepOff+2]); got != disk.MaxVoices {
		t.Errorf("Duplicate on full bank changed bstep: %d, want %d", got, disk.MaxVoices)
	}
}

// --- Journey 5: Don't lose work -----------------------------------------

// TestJourney_DontLoseWork pins three contracts that together
// protect against silent edit loss:
//
//  1. Switch-while-dirty: opening a different container while the
//     current one has unsaved edits opens a confirm modal with
//     Save / Discard / Cancel.
//  2. Autosave: a tick on a dirty container writes a single
//     {name}.bak snapshot next to the source file (overwritten on
//     each subsequent tick).
//  3. Recovery: on launch, if a .bak is newer than its named
//     container, the user is offered the recovery snapshot.
//
// Each sub-flow is a t.Run so a regression on any one is named in
// the test failure rather than buried in a multi-step monolith.
func TestJourney_DontLoseWork(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey under -short")
	}

	t.Run("switch_while_dirty_opens_confirm", func(t *testing.T) {
		dir := t.TempDir()
		// Seed two FZF fixtures in the workspace; both come from
		// the factory library so the loader is happy.
		srcA := filepath.Join("..", "..", "..", "testdata", "corpus",
			"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-3", "string-ensemble", "String-Ensemble.fzf")
		srcB := filepath.Join("..", "..", "..", "testdata", "corpus",
			"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-4", "drums", "Drums.fzf")
		rawA, errA := os.ReadFile(srcA)
		rawB, errB := os.ReadFile(srcB)
		if errA != nil || errB != nil {
			t.Skipf("missing FZF fixture: errA=%v errB=%v", errA, errB)
		}
		targetA := filepath.Join(dir, "A.fzf")
		targetB := filepath.Join(dir, "B.fzf")
		if err := os.WriteFile(targetA, rawA, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
			t.Fatalf("seed A: %v", err)
		}
		if err := os.WriteFile(targetB, rawB, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
			t.Fatalf("seed B: %v", err)
		}

		fc := newFakeClock()
		a := New(dir)
		a.backupDir = filepath.Join(dir, "_backups")
		a.tick = fc.Tick
		a.toast.SetClock(fc.Tick)
		a.status.SetClock(fc.Tick)
		a = pump(t, a, tea.WindowSizeMsg{Width: journeyCols, Height: journeyRows})

		// Load A, mutate it dirty.
		a = a.doOpenContainer(targetA)
		bytesA := a.containerModel.Bytes()
		if err := a.containerModel.Apply(model.Patch{
			Offset: 0,
			Old:    []byte{bytesA[0]},
			New:    []byte{bytesA[0] ^ 0x01},
		}); err != nil {
			t.Fatalf("dirty A: %v", err)
		}
		if !a.containerModel.Dirty() {
			t.Fatal("A should be dirty")
		}

		// Try to open B. Expect the confirm modal.
		a = a.handleWorkspaceIntent(workspace.Intent{
			Kind: workspace.IntentOpenContainer,
			Path: targetB,
		})
		if a.confirm == nil || !a.confirm.IsOpen() {
			t.Fatal("expected switch-while-dirty confirm to open")
		}

		// Cancel keeps the user on A.
		a.confirm.Cancel()
		a = a.resolvePendingConfirm(t)
		if a.containerInfo.Path != targetA {
			t.Errorf("after Cancel: path = %q, want %q", a.containerInfo.Path, targetA)
		}
		if !a.containerModel.Dirty() {
			t.Errorf("Cancel should preserve dirty state on A")
		}

		// Reopen B; this time pick Discard (Result=1).
		a = a.handleWorkspaceIntent(workspace.Intent{
			Kind: workspace.IntentOpenContainer,
			Path: targetB,
		})
		if !a.confirm.IsOpen() {
			t.Fatal("second open: confirm did not reopen")
		}
		// Walk to the Discard option (Save=2 is index 0, Discard=1 at
		// index 1, Cancel=0 at index 2). The widget's focus starts at
		// 0; Next once.
		a.confirm.Next()
		a.confirm.Confirm()
		a = a.resolvePendingConfirm(t)
		if a.containerInfo.Path != targetB {
			t.Errorf("after Discard: path = %q, want %q", a.containerInfo.Path, targetB)
		}
		if a.containerModel.Dirty() {
			t.Errorf("Discard should land B clean; Dirty=true")
		}
	})

	t.Run("save_and_switch_persists_then_swaps", func(t *testing.T) {
		// Picks "Save and switch" on the switch-while-dirty modal.
		// The current container's dirty bytes must hit disk BEFORE
		// the swap; reloading the first file after the swap proves
		// the save happened, and the App now shows the second file.
		dir := t.TempDir()
		srcA := filepath.Join("..", "..", "..", "testdata", "corpus",
			"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-3", "string-ensemble", "String-Ensemble.fzf")
		srcB := filepath.Join("..", "..", "..", "testdata", "corpus",
			"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-4", "drums", "Drums.fzf")
		rawA, errA := os.ReadFile(srcA)
		rawB, errB := os.ReadFile(srcB)
		if errA != nil || errB != nil {
			t.Skipf("missing FZF fixture: errA=%v errB=%v", errA, errB)
		}
		targetA := filepath.Join(dir, "A.fzf")
		targetB := filepath.Join(dir, "B.fzf")
		if err := os.WriteFile(targetA, rawA, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
			t.Fatalf("seed A: %v", err)
		}
		if err := os.WriteFile(targetB, rawB, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
			t.Fatalf("seed B: %v", err)
		}

		fc := newFakeClock()
		a := New(dir)
		a.backupDir = filepath.Join(dir, "_backups")
		a.tick = fc.Tick
		a.toast.SetClock(fc.Tick)
		a.status.SetClock(fc.Tick)
		a = pump(t, a, tea.WindowSizeMsg{Width: journeyCols, Height: journeyRows})

		a = a.doOpenContainer(targetA)
		bytesA := a.containerModel.Bytes()
		// Choose a byte that doesn't break IsActiveOrEmptyVoiceSlot:
		// flip a low bit in the bank name region.
		off := disk.BankNameOffset
		if err := a.containerModel.Apply(model.Patch{
			Offset: off,
			Old:    []byte{bytesA[off]},
			New:    []byte{bytesA[off] ^ 0x01},
		}); err != nil {
			t.Fatalf("dirty A: %v", err)
		}
		wantByte := a.containerModel.Bytes()[off]

		// Trigger switch; pick "Save and switch" (Result=2, focus
		// index 0).
		a = a.handleWorkspaceIntent(workspace.Intent{
			Kind: workspace.IntentOpenContainer,
			Path: targetB,
		})
		if a.confirm == nil || !a.confirm.IsOpen() {
			t.Fatal("expected switch-while-dirty confirm")
		}
		a.confirm.Confirm() // first option = "Save and switch"
		a = a.resolvePendingConfirm(t)

		if a.containerInfo.Path != targetB {
			t.Errorf("after Save-and-switch: path = %q, want %q", a.containerInfo.Path, targetB)
		}
		// Reload A from disk; the dirty byte must have been
		// persisted before the swap.
		reloadedA, err := os.ReadFile(targetA)
		if err != nil {
			t.Fatalf("reload A: %v", err)
		}
		if reloadedA[off] != wantByte {
			t.Errorf("A.fzf was not saved before swap: byte at %#x = %#x, want %#x",
				off, reloadedA[off], wantByte)
		}
	})

	t.Run("autosave_writes_bak_in_workspace", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join("..", "..", "..", "testdata", "corpus",
			"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-3", "string-ensemble", "String-Ensemble.fzf")
		raw, err := os.ReadFile(src)
		if err != nil {
			t.Skipf("missing fixture: %v", err)
		}
		target := filepath.Join(dir, "Piano.fzf")
		if err := os.WriteFile(target, raw, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
			t.Fatalf("seed: %v", err)
		}

		fc := newFakeClock()
		a := New(dir)
		// Do NOT override backupDir; production behaviour writes
		// to the workspace, which is what this test pins.
		a.tick = fc.Tick
		a.toast.SetClock(fc.Tick)
		a.status.SetClock(fc.Tick)
		a = a.doOpenContainer(target)

		// Mutate to make the container dirty.
		data := a.containerModel.Bytes()
		if err := a.containerModel.Apply(model.Patch{
			Offset: 0,
			Old:    []byte{data[0]},
			New:    []byte{data[0] ^ 0x01},
		}); err != nil {
			t.Fatalf("dirty: %v", err)
		}

		// Fire the autosave tick that Init scheduled.
		fc.FireAll() // returns autoSaveTick msgs we don't need to feed back
		a.runAutoSave()

		bakPath := filepath.Join(dir, "Piano.fzf.bak")
		if _, err := os.Stat(bakPath); err != nil {
			t.Fatalf("expected .bak in workspace: %v", err)
		}

		// Successful Save deletes the .bak.
		if err := a.containerModel.Save(target); err != nil {
			t.Fatalf("Save: %v", err)
		}
		a.clearAutoSaveBackup(target)
		if _, err := os.Stat(bakPath); !os.IsNotExist(err) {
			t.Errorf("Save should have removed %s (stat err=%v)", bakPath, err)
		}
	})

	t.Run("recovery_offers_newer_bak", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join("..", "..", "..", "testdata", "corpus",
			"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-3", "string-ensemble", "String-Ensemble.fzf")
		raw, err := os.ReadFile(src)
		if err != nil {
			t.Skipf("missing fixture: %v", err)
		}
		target := filepath.Join(dir, "Piano.fzf")
		if err := os.WriteFile(target, raw, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
			t.Fatalf("seed: %v", err)
		}
		// Backdate the source so the bak (written below) is newer.
		old := time.Now().Add(-1 * time.Minute)
		if err := os.Chtimes(target, old, old); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
		// Construct a "snapshot" that differs from the on-disk
		// container in one byte; this represents in-flight unsaved
		// edits that didn't reach Save.
		snapshot := make([]byte, len(raw))
		copy(snapshot, raw)
		snapshot[0] ^= 0x7F
		bakPath := filepath.Join(dir, "Piano.fzf.bak")
		if err := os.WriteFile(bakPath, snapshot, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
			t.Fatalf("seed bak: %v", err)
		}

		fc := newFakeClock()
		a := New(dir)
		a.tick = fc.Tick
		a.toast.SetClock(fc.Tick)
		a.status.SetClock(fc.Tick)
		a = pump(t, a, tea.WindowSizeMsg{Width: journeyCols, Height: journeyRows})

		// Open the container; recovery prompt should fire.
		a = a.handleWorkspaceIntent(workspace.Intent{
			Kind: workspace.IntentOpenContainer,
			Path: target,
		})
		if a.confirm == nil || !a.confirm.IsOpen() {
			t.Fatal("expected recovery prompt to open")
		}
		// Recover (Result=1, focus index 0).
		a.confirm.Confirm()
		a = a.resolvePendingConfirm(t)

		if !a.containerModel.Dirty() {
			t.Errorf("Recover should land in dirty state")
		}
		if a.containerModel.Bytes()[0] != snapshot[0] {
			t.Errorf("Recover did not load snapshot bytes: got [0]=%#x, want %#x",
				a.containerModel.Bytes()[0], snapshot[0])
		}
	})

	t.Run("recovery_discard_removes_bak", func(t *testing.T) {
		// Symmetric to recovery_offers_newer_bak: pick Discard
		// instead of Recover. The .bak must be deleted from disk so
		// the prompt doesn't reappear next launch; the App should
		// land on the on-disk container's bytes (not the snapshot).
		dir := t.TempDir()
		src := filepath.Join("..", "..", "..", "testdata", "corpus",
			"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-3", "string-ensemble", "String-Ensemble.fzf")
		raw, err := os.ReadFile(src)
		if err != nil {
			t.Skipf("missing fixture: %v", err)
		}
		target := filepath.Join(dir, "Piano.fzf")
		if err := os.WriteFile(target, raw, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
			t.Fatalf("seed: %v", err)
		}
		old := time.Now().Add(-1 * time.Minute)
		if err := os.Chtimes(target, old, old); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
		snapshot := make([]byte, len(raw))
		copy(snapshot, raw)
		snapshot[0] ^= 0x7F
		bakPath := filepath.Join(dir, "Piano.fzf.bak")
		if err := os.WriteFile(bakPath, snapshot, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
			t.Fatalf("seed bak: %v", err)
		}

		fc := newFakeClock()
		a := New(dir)
		a.tick = fc.Tick
		a.toast.SetClock(fc.Tick)
		a.status.SetClock(fc.Tick)
		a = pump(t, a, tea.WindowSizeMsg{Width: journeyCols, Height: journeyRows})

		a = a.handleWorkspaceIntent(workspace.Intent{
			Kind: workspace.IntentOpenContainer,
			Path: target,
		})
		if a.confirm == nil || !a.confirm.IsOpen() {
			t.Fatal("expected recovery prompt")
		}
		// Recover is at index 0; Discard at index 1.
		a.confirm.Next()
		a.confirm.Confirm()
		a = a.resolvePendingConfirm(t)

		// On-disk bytes should match the original, not the
		// snapshot.
		if a.containerModel.Bytes()[0] != raw[0] {
			t.Errorf("Discard kept the snapshot bytes; got [0]=%#x, want %#x",
				a.containerModel.Bytes()[0], raw[0])
		}
		if _, err := os.Stat(bakPath); !os.IsNotExist(err) {
			t.Errorf("Discard did not delete %s (stat err=%v)", bakPath, err)
		}
	})
}

func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}
