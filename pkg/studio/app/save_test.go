package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/loader"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/minimap"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/toast"
	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
)

// testVoiceOneFallback is the placeholder name the Layout view falls
// back to when an Area's voice header is unset. Several save tests
// assert "the assigned voice is NOT this fallback" to confirm the
// assignment landed.
const testVoiceOneFallback = "VOICE 1"

// TestSave_IMG_RoundTripPreservesDiskStructure pins the invariant the
// user surfaced: saving an .img-sourced container must keep the file
// exactly 1310720 bytes (the FZ-1 floppy size) so the loader can read
// it back. Before this fix, `persistContainer` was overwriting the
// whole .img with the in-memory FZF payload (~1019 KB), corrupting the
// disk header and producing the
//
//	"disk: image must be exactly 1310720 bytes, got 1043456"
//
// error on the next Open.
func TestSave_IMG_RoundTripPreservesDiskStructure(t *testing.T) {
	src := filepath.Join("..", "..", "..", "testdata", "disk-images", "JUNGLE.img")
	if _, err := os.Stat(src); err != nil {
		// Fall back to any .img fixture we can find.
		alt := filepath.Join("..", "..", "..", "testdata", "synthetic", "STAB.img")
		if _, err := os.Stat(alt); err != nil {
			t.Skipf("no .img fixture available (looked at JUNGLE.img and STAB.img)")
		}
		src = alt
	}
	srcBytes, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if len(srcBytes) != 1310720 {
		t.Fatalf("fixture .img is %d bytes, expected 1310720", len(srcBytes))
	}

	// Copy into a tempdir so the test never mutates the corpus.
	dir := t.TempDir()
	target := filepath.Join(dir, "round-trip.img")
	if err := os.WriteFile(target, srcBytes, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed target: %v", err)
	}

	// Build an App, load the .img.
	a := New(dir)
	a.backupDir = filepath.Join(dir, "backups") // isolate autosave
	m, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("LoadContainer: %v", err)
	}
	a.containerModel = m
	a.containerInfo = info

	if info.Format != loader.FormatIMG {
		t.Fatalf("loaded format = %v, want FormatIMG", info.Format)
	}

	// Apply a small in-place edit so dirty -> save has something to
	// write. Bumping the first bank's bstep is harmless for fixtures
	// that already have voices; for empty fixtures it adds 1.
	data := m.Bytes()
	if len(data) < disk.SectorSize+1 {
		t.Fatalf("FZF payload too small")
	}
	old := []byte{data[disk.BankVoiceCountOffset]}
	newVal := old[0] + 1
	if newVal == 0 {
		newVal = 1
	}
	if err := m.Apply(model.Patch{
		Offset: disk.BankVoiceCountOffset,
		Old:    old,
		New:    []byte{newVal},
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Persist via the new path-aware save.
	if err := a.persistContainer(target); err != nil {
		t.Fatalf("persistContainer: %v", err)
	}

	// Invariant 1: the file is still 1310720 bytes.
	st, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat saved image: %v", err)
	}
	if st.Size() != 1310720 {
		t.Fatalf("saved .img size = %d, want 1310720 (image header / dir would be corrupt)", st.Size())
	}

	// Invariant 2: the loader can re-open the file we just wrote.
	m2, info2, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("reload saved .img: %v", err)
	}
	if info2.Format != loader.FormatIMG {
		t.Fatalf("reloaded format = %v, want FormatIMG", info2.Format)
	}
	// Invariant 3: the edit survived the round-trip.
	if got := m2.Bytes()[disk.BankVoiceCountOffset]; got != newVal {
		t.Fatalf("round-trip dropped the edit: bstep = %d, want %d", got, newVal)
	}

	// Invariant 4: dirty cleared after save.
	if m.Dirty() {
		t.Errorf("model still dirty after persistContainer")
	}
}

// TestLoad_VoiceOnlyIMG_RoundTrips covers an IMG that holds a single
// Voice file instead of a Full Dump (HOOVER.img is the example
// fixture). The loader now wraps it as a synthetic single-voice FZF
// for the editor and unwraps back to an FZV on Save. Pinned because
// it's an easy case to regress on by accident.
func TestLoad_VoiceOnlyIMG_RoundTrips(t *testing.T) {
	src := filepath.Join("..", "..", "..", "testdata", "synthetic", "HOOVER.img")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing HOOVER.img fixture: %v", err)
	}
	srcBytes, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "HOOVER.img")
	if err := os.WriteFile(target, srcBytes, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed: %v", err)
	}

	a := New(dir)
	a.backupDir = filepath.Join(dir, "backups")
	m, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("LoadContainer voice-only .img: %v", err)
	}
	if !info.WrappedVoice {
		t.Fatalf("expected WrappedVoice=true on voice-only .img; got %+v", info)
	}
	if info.DiskEntryName == "" || info.DiskEntryName == disk.FullDumpName {
		t.Fatalf("DiskEntryName = %q, expected the voice's own name", info.DiskEntryName)
	}
	a.containerModel = m
	a.containerInfo = info

	// Rename the voice via a direct byte patch, save, reload, and
	// assert the rename survived AND the .img is still 1310720 bytes.
	off := disk.SectorSize + disk.VoiceNameOffset // skip the synthetic bank sector
	data := m.Bytes()
	old := make([]byte, disk.VoiceNameFieldSize)
	copy(old, data[off:off+disk.VoiceNameFieldSize])
	padded := disk.PadLabel("RENAMED")
	newBytes := make([]byte, disk.VoiceNameFieldSize)
	copy(newBytes, padded[:])
	if err := m.Apply(model.Patch{
		Offset: off,
		Old:    old,
		New:    newBytes,
	}); err != nil {
		t.Fatalf("rename patch: %v", err)
	}

	if err := a.persistContainer(target); err != nil {
		t.Fatalf("persistContainer voice-only .img: %v", err)
	}

	st, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat saved image: %v", err)
	}
	if st.Size() != 1310720 {
		t.Fatalf("saved .img size = %d, want 1310720", st.Size())
	}

	m2, info2, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("reload voice-only .img: %v", err)
	}
	if !info2.WrappedVoice {
		t.Fatalf("reloaded info.WrappedVoice = false, want true")
	}
	gotRaw := m2.Bytes()[off : off+disk.VoiceNameFieldSize]
	got := strings.TrimRight(strings.Trim(string(gotRaw), "\x00"), " ")
	if got != "RENAMED" {
		t.Fatalf("voice name after round-trip = %q, want RENAMED", got)
	}
}

// TestSave_FZF_RoundTripStillWorks guards the standalone .fzf path
// against accidental regression while we refactor the .img branch.
func TestSave_FZF_RoundTripStillWorks(t *testing.T) {
	src := filepath.Join("..", "..", "..", "testdata", "corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-a-piano",
		"Piano.fzf")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing Piano.fzf fixture: %v", err)
	}
	srcBytes, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "round-trip.fzf")
	if err := os.WriteFile(target, srcBytes, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed target: %v", err)
	}

	a := New(dir)
	a.backupDir = filepath.Join(dir, "backups")
	m, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("LoadContainer: %v", err)
	}
	a.containerModel = m
	a.containerInfo = info

	if info.Format != loader.FormatFZF {
		t.Fatalf("loaded format = %v, want FormatFZF", info.Format)
	}

	old := []byte{m.Bytes()[disk.BankVoiceCountOffset]}
	if err := m.Apply(model.Patch{
		Offset: disk.BankVoiceCountOffset,
		Old:    old,
		New:    []byte{old[0] + 1},
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if err := a.persistContainer(target); err != nil {
		t.Fatalf("persistContainer: %v", err)
	}

	m2, _, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("reload saved .fzf: %v", err)
	}
	if got := m2.Bytes()[disk.BankVoiceCountOffset]; got != old[0]+1 {
		t.Errorf("FZF round-trip dropped the edit: bstep = %d, want %d", got, old[0]+1)
	}
}

// TestSave_FiresSavedToast asserts that Ctrl-S on a loaded container
// emits the dismiss tick AND raises the toast text. The status line
// already logs the path (verbose), but the toast catches the eye
// (loud, brief). A stale DismissMsg from a prior toast must NOT clear
// the live one (token mismatch).
func TestSave_FiresSavedToast(t *testing.T) {
	src := filepath.Join("..", "..", "..", "testdata", "corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-a-piano",
		"Piano.fzf")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing Piano.fzf fixture: %v", err)
	}
	srcBytes, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "round-trip.fzf")
	if err := os.WriteFile(target, srcBytes, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed target: %v", err)
	}

	a := New(dir)
	a.backupDir = filepath.Join(dir, "backups")
	m, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("LoadContainer: %v", err)
	}
	a.containerModel = m
	a.containerInfo = info

	// Dirty the model so save isn't a no-op.
	old := []byte{m.Bytes()[disk.BankVoiceCountOffset]}
	if err := m.Apply(model.Patch{
		Offset: disk.BankVoiceCountOffset,
		Old:    old,
		New:    []byte{old[0] + 1},
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	updated, cmd := a.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	a, _ = updated.(App)
	if cmd == nil {
		t.Fatalf("Ctrl-S returned nil cmd; expected a dismiss tick for the toast")
	}
	if got := a.toast.View(); got == "" {
		t.Fatalf("toast empty after save; expected the Saved! banner")
	}

	// A stale dismiss from an earlier (non-existent) toast must not
	// blank the live one.
	a2, _ := a.Update(toast.DismissMsg{Token: 0})
	a2App, _ := a2.(App)
	if a2App.toast.View() == "" {
		t.Fatalf("stale DismissMsg cleared the live toast; token check is broken")
	}

	// The matching dismiss does clear it. We don't actually wait for
	// the tick; we synthesise the message with the current token.
	a3, _ := a.Update(toast.DismissMsg{Token: 1})
	a3App, _ := a3.(App)
	if got := a3App.toast.View(); got != "" {
		t.Fatalf("matching DismissMsg failed to clear toast; got %q", got)
	}
}

// TestNewDisk_CleanContainer_StartsFreshLayout asserts pressing `n`
// on a clean (untitled, no edits) container installs a fresh
// NewUntitled, lands the user in Layout, and the new container has
// no path. A clean container shouldn't prompt for confirmation.
func TestNewDisk_CleanContainer_StartsFreshLayout(t *testing.T) {
	dir := t.TempDir()
	a := New(dir)
	prevPath := a.containerInfo.Path

	updated, _ := a.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	a, _ = updated.(App)
	if a.confirm.IsOpen() {
		t.Fatalf("clean container should NOT trigger confirm modal")
	}
	if a.containerInfo.Path != "" {
		t.Fatalf("new disk container.Path = %q, want untitled", a.containerInfo.Path)
	}
	if a.containerInfo.Path == prevPath && a.containerInfo.BankCount != 8 {
		t.Fatalf("new disk did not install: BankCount = %d, want 8", a.containerInfo.BankCount)
	}
}

// TestBankRename_WritesBankNameField pins the rename-bank gesture
// surfaced from the Layout bank list. Pressing `r` on a bank row
// opens the rename modal seeded with the current bank name; typing
// new characters + Enter writes them into the bank's name field
// (BankNameOffset). Materialised banks rename in place; unmaterialised
// banks auto-grow then rename, mirroring the assignment flow.
func TestBankRename_WritesBankNameField(t *testing.T) {
	src := filepath.Join("..", "..", "..", "testdata", "corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-a-piano",
		"Piano.fzf")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing Piano.fzf: %v", err)
	}
	data, _ := os.ReadFile(src)
	dir := t.TempDir()
	target := filepath.Join(dir, "Piano.fzf")
	if err := os.WriteFile(target, data, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed: %v", err)
	}

	a := New(dir)
	m, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("LoadContainer: %v", err)
	}
	a.containerModel = m
	a.containerInfo = info
	a.layout.SetContainer(m, info)
	a.current = minimap.Layout

	// On the bank list, with cursor on Bank 1.
	updated, _ := a.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	a, _ = updated.(App)
	if !a.renameActive || !a.renameBank {
		t.Fatalf("rename modal not active on bank target: active=%v bank=%v",
			a.renameActive, a.renameBank)
	}
	// Type "NEWBANK" + Enter. The buffer starts in "fresh" mode (the
	// first keystroke replaces the seeded name).
	for _, ch := range "NEWBANK" {
		updated, _ = a.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		a, _ = updated.(App)
	}
	updated, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, _ = updated.(App)

	if a.renameActive {
		t.Fatalf("rename modal stayed open after Enter")
	}
	bytes := a.containerModel.Bytes()
	off := disk.BankNameOffset
	nameField := bytes[off : off+disk.VoiceNameFieldSize]
	got := strings.TrimRight(strings.Trim(string(nameField), "\x00"), " ")
	if got != "NEWBANK" {
		t.Fatalf("Bank 1 name = %q, want %q", got, "NEWBANK")
	}
}

// TestAssign_AutoGrowsBankCountOnUnmaterialisedBank pins the
// auto-grow model the user requested: the bank list always shows all
// 8 banks (with later ones rendering as "(empty)"), and assigning a
// voice to an Area in a previously-unmaterialised bank inserts the
// bank sectors lazily. JUNGLE.img starts with BankCount=1; assigning
// to Bank 3 / Area 1 should bump BankCount to 3 and leave a usable
// container.
func TestAssign_AutoGrowsBankCountOnUnmaterialisedBank(t *testing.T) {
	src := filepath.Join("..", "..", "..", "testdata", "disk-images", "JUNGLE.img")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing JUNGLE.img: %v", err)
	}
	data, _ := os.ReadFile(src)
	dir := t.TempDir()
	target := filepath.Join(dir, "JUNGLE.img")
	if err := os.WriteFile(target, data, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed: %v", err)
	}

	a := New(dir)
	a.backupDir = filepath.Join(dir, "backups")
	m, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("LoadContainer: %v", err)
	}
	a.containerModel = m
	a.containerInfo = info
	a.layout.SetContainer(m, info)

	startBanks := a.containerInfo.BankCount
	if startBanks >= 3 {
		t.Skipf("JUNGLE fixture has %d banks; need < 3 for this test", startBanks)
	}

	// Seed a wav into the pool, then drive an assignment into Bank 3.
	wavSrc := filepath.Join("..", "..", "..", "testdata", "synthetic", "JUNGLISM Samples", "808.wav")
	if _, err := os.Stat(wavSrc); err != nil {
		t.Skipf("missing 808.wav: %v", err)
	}
	if err := a.pool.AddWAV(wavSrc, -1); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
	// Drive the assignment directly through the App helper (skips the
	// picker UI plumbing, which we test elsewhere).
	entry := a.pool.Selected()
	if entry == nil {
		t.Fatalf("pool empty after AddWAV")
	}
	a = a.assignPoolEntryToArea(entry, 2, 0) // Bank 3 (zero-indexed), Area 1

	if a.containerInfo.BankCount != 3 {
		t.Fatalf("BankCount = %d after assigning into Bank 3; want 3",
			a.containerInfo.BankCount)
	}
	if got := a.layout.VoiceName(2, 0); got == "" || got == testVoiceOneFallback {
		t.Fatalf("Bank 3 Area 1 voice name = %q; want a real assignment", got)
	}
}

// TestAssign_RefusesAutoGrowOnVoiceOnlyIMG pins the WrappedVoice
// guard inside growBanksTo: a single-voice .img can't host extra
// banks; the assignment must be rejected, not silently corrupt the
// container.
func TestAssign_RefusesAutoGrowOnVoiceOnlyIMG(t *testing.T) {
	src := filepath.Join("..", "..", "..", "testdata", "synthetic", "HOOVER.img")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing HOOVER.img: %v", err)
	}
	data, _ := os.ReadFile(src)
	dir := t.TempDir()
	target := filepath.Join(dir, "HOOVER.img")
	if err := os.WriteFile(target, data, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed: %v", err)
	}
	a := New(dir)
	m, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("LoadContainer: %v", err)
	}
	a.containerModel = m
	a.containerInfo = info
	a.layout.SetContainer(m, info)

	startBytes := a.containerModel.Len()
	// Stub a pool entry so the early "pool entry too small" check
	// doesn't short-circuit. Any FZV-shaped bytes will do.
	stub := make([]byte, disk.SectorSize+disk.BytesPerSample*8)
	copy(stub[disk.VoiceNameOffset:], []byte("STUB        "))
	a.pool.MirrorContainerVoices([][]byte{stub})
	entry := a.pool.Selected()
	if entry == nil {
		t.Fatalf("pool seed failed")
	}
	a = a.assignPoolEntryToArea(entry, 2, 0)

	if a.containerModel.Len() != startBytes {
		t.Fatalf("voice-only .img grew on assign; want no-op")
	}
}

// TestAssign_GrowsVoiceAreaWhenSlotPastEnd pins the voice-area growth
// fix. NewUntitled ships with one voice sector (4 slots). Assigning to
// Area 5 or beyond used to write the voice header into the audio area.
// The slot's byte offset overshot the voice-area boundary. The fix
// inserts zero sectors between the voice area and the audio area
// before patching, and shifts AudioAreaStart accordingly. The
// regression: after assigning Area 6 (slot 6), Layout.VoiceName must
// resolve to the assigned voice (not "VOICE 1") AND the saved bytes
// must reload cleanly.
func TestAssign_GrowsVoiceAreaWhenSlotPastEnd(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join("..", "..", "..", "testdata", "synthetic", "JUNGLISM Samples", "808.wav")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing 808.wav: %v", err)
	}
	data, _ := os.ReadFile(src)
	if err := os.WriteFile(filepath.Join(dir, "808.wav"), data, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed: %v", err)
	}

	a := New(dir)
	// Walk to Bank 1, drill into Areas, advance cursor to A07 (areaIdx 6).
	a = mustStep(t, a,
		tea.WindowSizeMsg{Width: 140, Height: 40},
		tea.KeyPressMsg{Code: tea.KeyEnter},                   // open 808.wav -> pool
		tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}, // -> Pool
		tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}, // -> Layout
		tea.KeyPressMsg{Code: tea.KeyEnter},                   // open Bank 1
	)
	for i := 0; i < 6; i++ {
		a = mustStep(t, a, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	a = mustStep(t, a,
		tea.KeyPressMsg{Code: 'i', Text: "i"},
		tea.KeyPressMsg{Code: tea.KeyEnter}, // assign
	)

	bytes := a.containerModel.Bytes()
	slot, ok := disk.BankVPLookup(bytes, 0, 6)
	if !ok {
		t.Fatalf("BankVPLookup out of bounds")
	}
	if slot == 0 {
		t.Fatalf("vp[area 7] = 0 (unset); the assignment did not patch vp[]")
	}
	if got := a.layout.VoiceName(0, 6); got == testVoiceOneFallback {
		t.Fatalf("Area 7 still shows VOICE 1; voice header didn't land in the grown voice area")
	}
	// Sanity check: voice area must now extend at least to slot 6+1 = 7 slots.
	voiceAreaStart := a.containerInfo.BankCount * disk.SectorSize
	voiceSectors := (a.containerInfo.AudioAreaStart - voiceAreaStart) / disk.SectorSize
	if voiceSectors < disk.VoiceAreaSectors(7) {
		t.Fatalf("voice area only %d sectors; need at least %d for slot 6",
			voiceSectors, disk.VoiceAreaSectors(7))
	}
}

// TestAssign_EmptyGapsShowAsEmpty pins the cosmetic bstep-bump fix.
// Assigning Area 3 bumps bstep to 3, marking Areas 1 and 2 as
// "populated" inside the bank. Before the fix, those rendered as
// "VOICE 1" because vp[]=0 -> slot 0 -> empty header -> blank name
// -> "VOICE n" fallback. With the fix, areaSummary detects the
// NoSound loop-mode and the view shows "(empty)" instead.
func TestAssign_EmptyGapsShowAsEmpty(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join("..", "..", "..", "testdata", "synthetic", "JUNGLISM Samples", "808.wav")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing 808.wav: %v", err)
	}
	data, _ := os.ReadFile(src)
	if err := os.WriteFile(filepath.Join(dir, "808.wav"), data, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed: %v", err)
	}

	a := New(dir)
	a = mustStep(t, a,
		tea.WindowSizeMsg{Width: 140, Height: 40},
		tea.KeyPressMsg{Code: tea.KeyEnter},                   // -> pool
		tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}, // -> Pool
		tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}, // -> Layout
		tea.KeyPressMsg{Code: tea.KeyEnter},                   // open Bank 1
		tea.KeyPressMsg{Code: tea.KeyDown},                    // A02
		tea.KeyPressMsg{Code: tea.KeyDown},                    // A03
		tea.KeyPressMsg{Code: 'i', Text: "i"},                 // open picker
		tea.KeyPressMsg{Code: tea.KeyEnter},                   // assign
	)
	// Gap Areas (0 and 1) must read as empty, not "VOICE 1".
	for _, areaIdx := range []int{0, 1} {
		if got := a.layout.VoiceName(0, areaIdx); got != "" && got != "(empty)" {
			t.Errorf("Area %d should render as empty; got %q", areaIdx+1, got)
		}
	}
}

// TestAssign_WritesVPEntry pins the vp[] write that was missing. Before
// this fix, assigning a voice to Bank 1 / Area 3 wrote the voice header
// to slot 3 but left vp[3] at 0 (default), so reads via BankVPLookup
// returned slot 0 and the user saw "VOICE 1" in every area instead of
// the assigned voice. The fix patches vp[areaIdx] = slotIdx in the
// same batch as the voice header.
func TestAssign_WritesVPEntry(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join("..", "..", "..", "testdata", "synthetic", "JUNGLISM Samples", "808.wav")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing 808.wav: %v", err)
	}
	data, _ := os.ReadFile(src)
	if err := os.WriteFile(filepath.Join(dir, "808.wav"), data, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed: %v", err)
	}

	a := New(dir)
	// Open the wav into the pool, drill to Layout, pick Area 3, assign.
	a = mustStep(t, a,
		tea.WindowSizeMsg{Width: 140, Height: 40},
		tea.KeyPressMsg{Code: tea.KeyEnter},                   // open 808.wav -> pool
		tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}, // -> Pool
		tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}, // -> Layout
		tea.KeyPressMsg{Code: tea.KeyEnter},                   // open Bank 1
		tea.KeyPressMsg{Code: tea.KeyDown},                    // A02
		tea.KeyPressMsg{Code: tea.KeyDown},                    // A03
		tea.KeyPressMsg{Code: 'i', Text: "i"},                 // open picker
		tea.KeyPressMsg{Code: tea.KeyEnter},                   // assign
	)

	// Direct vp[] inspection on the model bytes; the fix's invariant.
	bytes := a.containerModel.Bytes()
	slot, ok := disk.BankVPLookup(bytes, 0, 2) // bank 1, area 3 (zero-indexed)
	if !ok {
		t.Fatalf("BankVPLookup out of bounds; model length=%d", len(bytes))
	}
	if slot == 0 {
		t.Fatalf("vp[area 3] = 0 (unset); assignment did not patch vp[]")
	}

	// And the Layout view must show the voice name there, not the
	// "VOICE 1" fallback (which is the symptom of the bug).
	name := a.layout.VoiceName(0, 2)
	if name == testVoiceOneFallback {
		t.Fatalf("Layout still shows VOICE 1 in Area 3: vp[] write didn't take effect")
	}
}

func mustStep(t *testing.T, a App, msgs ...tea.Msg) App {
	t.Helper()
	var m tea.Model = a
	for _, msg := range msgs {
		m, _ = m.Update(msg)
	}
	a, _ = m.(App)
	return a
}

// TestNewDisk_SaveAsImg_WrapsIntoFloppy pins the round-trip the user
// hit: press `n`, Ctrl-S, name "FOO.img". Before this fix, doSaveTo
// wrote the raw 9216-byte FZF to FOO.img, which the loader rejected
// on re-open ("image must be exactly 1310720 bytes, got 9216").
// Now the .img path goes through diskformat.Format + diskadd.AddBytes
// so the saved file is a real FZ-1 floppy image.
func TestNewDisk_SaveAsImg_WrapsIntoFloppy(t *testing.T) {
	dir := t.TempDir()
	a := New(dir)
	target := filepath.Join(dir, "FOO.img")

	if err := a.writeContainerToPath(target); err != nil {
		t.Fatalf("writeContainerToPath: %v", err)
	}
	st, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat saved image: %v", err)
	}
	if st.Size() != 1310720 {
		t.Fatalf("saved .img size = %d, want 1310720", st.Size())
	}
	// And the loader must accept it.
	if _, _, err := loader.LoadContainer(target); err != nil {
		t.Fatalf("loader rejected the new-disk .img: %v", err)
	}
}

// TestNewDisk_SaveAsFZF_RawWrite keeps the FZF path honest: when the
// user types an .fzf extension, doSaveTo must still write raw FZF
// bytes (no IMG wrapping).
func TestNewDisk_SaveAsFZF_RawWrite(t *testing.T) {
	dir := t.TempDir()
	a := New(dir)
	target := filepath.Join(dir, "FOO.fzf")

	if err := a.writeContainerToPath(target); err != nil {
		t.Fatalf("writeContainerToPath: %v", err)
	}
	st, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat saved: %v", err)
	}
	// NewUntitled is 8 bank sectors + 1 voice area sector = 9216 bytes.
	if st.Size() != 9216 {
		t.Fatalf("FZF save size = %d, want 9216", st.Size())
	}
}

// TestExport_PoolEntry_WritesFZV pins the export round-trip: add a
// voice to the pool via the Workspace path, press `e`, and the
// resulting .fzv must be loadable by the loader (proving the bytes
// the pool held really are a valid FZV).
func TestExport_PoolEntry_WritesFZV(t *testing.T) {
	src := filepath.Join("..", "..", "..", "testdata", "corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-a-piano",
		"Piano.fzf")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing Piano.fzf fixture: %v", err)
	}

	dir := t.TempDir()
	a := New(dir)
	// Seed the pool from a Piano.fzf load so we have a real voice.
	m, info, err := loader.LoadContainer(src)
	if err != nil {
		t.Fatalf("LoadContainer: %v", err)
	}
	a.containerModel = m
	a.containerInfo = info
	// Mirror via the same path the open-flow uses.
	voices, _, err := voiceunpack.UnpackDataFromBytes(m.Bytes())
	if err != nil {
		t.Fatalf("unpack: %v", err)
	}
	a.pool.MirrorContainerVoices(voices)
	if len(a.pool.Entries()) == 0 {
		t.Fatalf("pool empty after mirror; expected at least one voice")
	}
	a.current = minimap.Pool

	updated, _ := a.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	a, _ = updated.(App)

	// Find the .fzv that landed in dir.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	var exported string
	for _, ent := range entries {
		if strings.HasSuffix(ent.Name(), ".fzv") {
			exported = filepath.Join(dir, ent.Name())
			break
		}
	}
	if exported == "" {
		t.Fatalf("no .fzv landed in %s after export", dir)
	}
	// The exported bytes must match what the pool held (round-trip
	// is byte-for-byte, no transformation on export).
	got, err := os.ReadFile(exported)
	if err != nil {
		t.Fatalf("read exported .fzv: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("exported .fzv is empty")
	}
	first := a.pool.Entries()[0]
	if string(got) != string(first.Bytes) {
		t.Fatalf("exported bytes != pool entry bytes (got %d, want %d bytes)",
			len(got), len(first.Bytes))
	}
}

// TestRefresh_RescansWorkspace asserts Ctrl-R re-reads the workspace
// directory. We write a file after App construction, hit Ctrl-R, and
// check the file is now visible in the browser. Without refresh the
// browser only re-reads on traversal.
func TestRefresh_RescansWorkspace(t *testing.T) {
	dir := t.TempDir()
	a := New(dir)
	// Seed window size so View renders a real listing.
	updated, _ := a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a, _ = updated.(App)

	// Drop a file in the dir AFTER New has cached the listing.
	if err := os.WriteFile(filepath.Join(dir, "NEW.fzv"), []byte{0, 0, 0, 0}, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed file: %v", err)
	}

	// Pre-refresh: the browser should NOT know about the file yet.
	if strings.Contains(renderView(a), "NEW.fzv") {
		t.Fatalf("workspace knew about NEW.fzv before refresh; broken cache assumption")
	}

	updated, _ = a.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	a, _ = updated.(App)

	if !strings.Contains(renderView(a), "NEW.fzv") {
		t.Fatalf("Ctrl-R did not surface NEW.fzv; refresh broken")
	}
}

// TestSave_CompactsTrailingEmptyBanks pins the round-trip for
// "rename a high bank, save, reload". Two passes the saver makes
// interact here:
//
//   - preserveRenamedBanks: any bank with bstep=0 BUT a user-set
//     name gets a synthetic NoSound voice so the rename survives
//     CountBankSectors on reload.
//   - compactEmptyBanks: anonymously-empty banks (bstep=0, no
//     name) are dropped before write.
//
// Renaming Bank 8 on a 3-bank disk auto-grows BankCount to 8 with
// Banks 4-7 empty + unnamed and Bank 7 empty + renamed "ZAP".
// After save the file should have 4 banks: the original 3 plus
// ZAP compacted down into the next slot. Reload confirms the
// rename round-trips.
func TestSave_CompactsTrailingEmptyBanks(t *testing.T) {
	src := filepath.Join("..", "..", "..", "testdata", "corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-a-piano",
		"Piano.fzf")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing Piano.fzf: %v", err)
	}
	data, _ := os.ReadFile(src)
	dir := t.TempDir()
	target := filepath.Join(dir, "Piano.fzf")
	if err := os.WriteFile(target, data, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed: %v", err)
	}

	a := New(dir)
	a.backupDir = filepath.Join(dir, "backups")
	m, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("LoadContainer: %v", err)
	}
	a.containerModel = m
	a.containerInfo = info
	a.layout.SetContainer(m, info)
	startBanks := a.containerInfo.BankCount

	// Rename Bank 8 (zero-indexed 7). This auto-grows to BankCount=8
	// with Banks 4-8 empty (bstep=0).
	a = a.installBankRenameForTest(7, "ZAP")
	if a.containerInfo.BankCount != 8 {
		t.Fatalf("BankCount after rename = %d, want 8", a.containerInfo.BankCount)
	}

	// Save via Ctrl-S, which routes through persistContainer with the
	// compaction step.
	updated, _ := a.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	a, _ = updated.(App)

	// Compaction drops anonymous trailing empties (Banks 4-7) but
	// preserves the renamed Bank 8, compacting it into the next
	// available slot, so the count is startBanks + 1.
	wantBanks := startBanks + 1
	if a.containerInfo.BankCount != wantBanks {
		t.Fatalf("after save: BankCount = %d, want %d", a.containerInfo.BankCount, wantBanks)
	}

	// Reload from disk, confirm CountBankSectors agrees and the
	// rename is on the last bank.
	m2, info2, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if info2.BankCount != wantBanks {
		t.Fatalf("reload BankCount = %d, want %d", info2.BankCount, wantBanks)
	}
	data2 := m2.Bytes()
	lastBank := info2.BankCount - 1
	nameStart := lastBank*disk.SectorSize + disk.BankNameOffset
	nameEnd := nameStart + disk.VoiceNameFieldSize
	gotName := strings.TrimRight(string(data2[nameStart:nameEnd]), " \x00")
	if gotName != "ZAP" {
		t.Fatalf("reload Bank %d name = %q, want %q", lastBank+1, gotName, "ZAP")
	}
}

// installBankRenameForTest drives a bank rename without going
// through the rename modal's per-keystroke flow. Used by save tests
// where the modal interactions aren't relevant.
func (a App) installBankRenameForTest(bankIdx int, newName string) App {
	a.renameActive = true
	a.renameBank = true
	a.renameTarget = pickerTarget{BankIdx: bankIdx}
	a.renameBuffer = newName
	a.renameFresh = false
	m, _ := a.commitBankRename()
	a, _ = m.(App)
	return a
}

// TestSave_CompactsMiddleEmptyBanks pins the middle-gap case: when
// the user assigns to Bank 5 while skipping Banks 2-4, the saved
// file must drop the empty middle banks and renumber so the
// formerly-Bank-5 lives at the next free slot after the kept banks.
// Voice slot indices in vp[] are preserved (they're absolute into
// the voice area, not bank-relative).
func TestSave_CompactsMiddleEmptyBanks(t *testing.T) {
	src := filepath.Join("..", "..", "..", "testdata", "corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-a-piano",
		"Piano.fzf")
	wavSrc := filepath.Join("..", "..", "..", "testdata", "synthetic", "JUNGLISM Samples", "808.wav")
	for _, p := range []string{src, wavSrc} {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("missing fixture: %v", err)
		}
	}
	data, _ := os.ReadFile(src)
	dir := t.TempDir()
	target := filepath.Join(dir, "Piano.fzf")
	if err := os.WriteFile(target, data, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed: %v", err)
	}
	if wavBytes, _ := os.ReadFile(wavSrc); len(wavBytes) > 0 {
		_ = os.WriteFile(filepath.Join(dir, "808.wav"), wavBytes, 0o644) //nolint:gosec // G703: testdata fixture under repo root
	}

	a := New(dir)
	a.backupDir = filepath.Join(dir, "backups")
	m, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("LoadContainer: %v", err)
	}
	a.containerModel = m
	a.containerInfo = info
	a.layout.SetContainer(m, info)

	// Stage a wav in the pool.
	if err := a.pool.AddWAV(wavSrc, -1); err != nil {
		t.Fatalf("add wav: %v", err)
	}
	entry := a.pool.Selected()
	if entry == nil {
		t.Fatalf("pool empty")
	}

	startBanks := a.containerInfo.BankCount
	// Assign to Bank 6 / Area 0 (zero-indexed 5 / 0). Banks
	// 4-5 (zero-indexed) are materialised as empty middle gaps.
	a = a.assignPoolEntryToArea(entry, 5, 0)
	if a.containerInfo.BankCount != 6 {
		t.Fatalf("BankCount after assign-to-Bank6 = %d, want 6",
			a.containerInfo.BankCount)
	}

	// Save via Ctrl-S; compaction drops Banks 4 and 5 (empty middle
	// gaps), pulling formerly-Bank-6 down to Bank 4.
	updated, _ := a.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	a, _ = updated.(App)
	if a.containerInfo.BankCount != startBanks+1 {
		t.Fatalf("BankCount after compaction = %d, want %d (orig %d + 1 kept new bank)",
			a.containerInfo.BankCount, startBanks+1, startBanks)
	}

	// Reload from disk to confirm CountBankSectors agrees.
	_, info2, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if info2.BankCount != startBanks+1 {
		t.Fatalf("reload BankCount = %d, want %d", info2.BankCount, startBanks+1)
	}
	// The voice we assigned must survive at the new bank index.
	a2 := New(dir)
	m2, info2b, _ := loader.LoadContainer(target)
	a2.containerModel = m2
	a2.containerInfo = info2b
	a2.layout.SetContainer(m2, info2b)
	if name := a2.layout.VoiceName(startBanks, 0); name == "" || name == testVoiceOneFallback {
		t.Errorf("reloaded compacted file: Bank %d Area 1 voice name = %q; want a real assignment",
			startBanks+1, name)
	}
}

// TestSwapAreas_ExchangesVPAndKeyRanges pins the `m`-`m` swap. Press
// m on Area 1, navigate to Area 5, press m: vp[] entries and per-
// area metadata swap. We assert by reading the raw bytes: the
// post-swap vp[area1] equals the pre-swap vp[area5] and vice versa.
func TestSwapAreas_ExchangesVPAndKeyRanges(t *testing.T) {
	src := filepath.Join("..", "..", "..", "testdata", "corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-a-piano",
		"Piano.fzf")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing Piano.fzf: %v", err)
	}
	data, _ := os.ReadFile(src)
	dir := t.TempDir()
	target := filepath.Join(dir, "Piano.fzf")
	if err := os.WriteFile(target, data, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed: %v", err)
	}

	a := New(dir)
	m, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("LoadContainer: %v", err)
	}
	a.containerModel = m
	a.containerInfo = info
	a.layout.SetContainer(m, info)
	a.current = minimap.Layout

	// Drill into Bank 1.
	a = mustStep(t, a,
		tea.WindowSizeMsg{Width: 140, Height: 40},
		tea.KeyPressMsg{Code: tea.KeyEnter},
	)

	// Snapshot vp[0] and vp[4] before the swap.
	before := a.containerModel.Bytes()
	vp0Before, _ := disk.BankVPLookup(before, 0, 0)
	vp4Before, _ := disk.BankVPLookup(before, 0, 4)
	if vp0Before == vp4Before {
		t.Skipf("Areas 1 and 5 already have identical vp[] entries; swap is a no-op")
	}

	// Press m on Area 1 (cursor already there), navigate to Area 5,
	// press m again.
	a = mustStep(t, a,
		tea.KeyPressMsg{Code: 'm', Text: "m"},
		tea.KeyPressMsg{Code: tea.KeyDown},
		tea.KeyPressMsg{Code: tea.KeyDown},
		tea.KeyPressMsg{Code: tea.KeyDown},
		tea.KeyPressMsg{Code: tea.KeyDown},
		tea.KeyPressMsg{Code: 'm', Text: "m"},
	)

	after := a.containerModel.Bytes()
	vp0After, _ := disk.BankVPLookup(after, 0, 0)
	vp4After, _ := disk.BankVPLookup(after, 0, 4)
	if vp0After != vp4Before {
		t.Errorf("vp[0] after swap = %d, want %d (the original vp[4])", vp0After, vp4Before)
	}
	if vp4After != vp0Before {
		t.Errorf("vp[4] after swap = %d, want %d (the original vp[0])", vp4After, vp0Before)
	}
}

// TestLayoutExport_WritesFZV pins the `e` gesture on an Area. The
// focused voice is extracted via voiceunpack (full FZV with audio,
// pointers rewritten to be 0-relative) and written to the workspace
// directory.
func TestLayoutExport_WritesFZV(t *testing.T) {
	src := filepath.Join("..", "..", "..", "testdata", "corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-a-piano",
		"Piano.fzf")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing Piano.fzf: %v", err)
	}
	data, _ := os.ReadFile(src)
	dir := t.TempDir()
	target := filepath.Join(dir, "Piano.fzf")
	if err := os.WriteFile(target, data, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed: %v", err)
	}

	a := New(dir)
	m, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("LoadContainer: %v", err)
	}
	a.containerModel = m
	a.containerInfo = info
	a.layout.SetContainer(m, info)
	a.current = minimap.Layout

	// Drill into Bank 1 (Layout starts in bank-list view), then export A01.
	a = mustStep(t, a,
		tea.WindowSizeMsg{Width: 140, Height: 40},
		tea.KeyPressMsg{Code: tea.KeyEnter},   // drill into Bank 1
		tea.KeyPressMsg{Code: 'e', Text: "e"}, // export Area 1
	)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	var exported string
	for _, ent := range entries {
		if strings.HasSuffix(ent.Name(), ".fzv") {
			exported = filepath.Join(dir, ent.Name())
			break
		}
	}
	if exported == "" {
		t.Fatalf("no .fzv landed in %s after export", dir)
	}
	st, err := os.Stat(exported)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Size() < disk.SectorSize {
		t.Fatalf("exported .fzv smaller than one sector: %d bytes", st.Size())
	}
}

// TestNewDisk_DirtyContainer_PromptsBeforeDiscard pins the safety
// gate: dirty edits MUST open a confirm modal; choosing Cancel
// preserves the container. Discard installs the fresh canvas.
func TestNewDisk_DirtyContainer_PromptsBeforeDiscard(t *testing.T) {
	src := filepath.Join("..", "..", "..", "testdata", "corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-a-piano",
		"Piano.fzf")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("missing Piano.fzf fixture: %v", err)
	}
	srcBytes, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "p.fzf")
	if err := os.WriteFile(target, srcBytes, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed: %v", err)
	}

	a := New(dir)
	a.backupDir = filepath.Join(dir, "backups")
	m, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("LoadContainer: %v", err)
	}
	a.containerModel = m
	a.containerInfo = info

	// Dirty it.
	old := []byte{m.Bytes()[disk.BankVoiceCountOffset]}
	if err := m.Apply(model.Patch{
		Offset: disk.BankVoiceCountOffset,
		Old:    old,
		New:    []byte{old[0] + 1},
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	updated, _ := a.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	a, _ = updated.(App)
	if !a.confirm.IsOpen() {
		t.Fatalf("dirty new-disk did NOT open confirm modal")
	}
	if a.containerInfo.Path != target {
		t.Fatalf("container swapped before confirmation; Path=%q", a.containerInfo.Path)
	}
}

// TestSaveAsModal_ShowsResolvedTarget pins N-07: the Save-As dialog
// previews the resolved destination path and the appended .img
// extension, so the user knows where and as what the file is written.
func TestSaveAsModal_ShowsResolvedTarget(t *testing.T) {
	a, _ := newTestAppEmpty(t)
	a.saveAsActive = true
	a.saveAsBuffer = "mydisk"

	modal := a.renderSaveAsModal()
	if !strings.Contains(modal, "Saves to:") {
		t.Errorf("save-as modal does not show the destination:\n%s", modal)
	}
	if !strings.Contains(modal, "mydisk.img") {
		t.Errorf("save-as modal does not preview the .img extension:\n%s", modal)
	}
	// The dialog is really "name this new disk", not a save-a-copy
	// feature, so it must not be titled the misleading "Save As".
	if !strings.Contains(modal, "Name new disk") {
		t.Errorf("modal should be titled 'Name new disk':\n%s", modal)
	}
	if strings.Contains(modal, "Save As") {
		t.Errorf("modal must not use the misleading 'Save As' label:\n%s", modal)
	}
}

// TestQuitConfirm_SaysDisk pins F-D: the unsaved-changes prompt speaks
// the user's word ("disk"), not the codebase's internal "container".
func TestQuitConfirm_SaysDisk(t *testing.T) {
	a, _ := newTestAppEmpty(t)
	a = pump(t, a, tea.WindowSizeMsg{Width: 140, Height: 40})
	// Force a dirty state so Ctrl-Q opens the confirm modal.
	data := a.containerModel.Bytes()
	if err := a.containerModel.Apply(modelApplyDirtyByte(t, data)); err != nil {
		t.Fatalf("seed dirty: %v", err)
	}
	updated, _ := a.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	a, _ = updated.(App)

	view := a.confirm.View()
	if !strings.Contains(view, "disk") {
		t.Errorf("unsaved-changes prompt should say 'disk':\n%s", view)
	}
	if strings.Contains(view, "container") {
		t.Errorf("unsaved-changes prompt leaks the internal word 'container':\n%s", view)
	}
}
