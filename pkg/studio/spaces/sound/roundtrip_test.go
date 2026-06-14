package sound

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/loader"
)

// TestEditorPatch_RoundTripPreservesVoicePlausibility is the
// stronger sibling of TestEditorPatch_PreservesVoicePlausibility:
// the in-memory invariant is checked AGAIN after a real Save +
// reload through the production loader. Catches writer bugs the
// in-memory test would miss (anything that strips, truncates, or
// reorders bytes during the file write).
//
// Coverage is intentionally narrower than the in-memory test (one
// representative value per field per voice, not min/max/mid) to
// keep the round-trip cost manageable; every iteration writes the
// fixture to disk and re-parses it.
func TestEditorPatch_RoundTripPreservesVoicePlausibility(t *testing.T) {
	src := filepath.Join("..", "..", "..", "..", "testdata", "corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-a-piano", "Piano.fzf")
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("missing Piano.fzf fixture: %v", err)
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "Piano.fzf")
	if err := os.WriteFile(target, raw, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed fixture: %v", err)
	}
	m, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("initial LoadContainer: %v", err)
	}
	voiceAreaStart := info.BankCount * disk.SectorSize

	// Probe one representative active voice slot. Piano.fzf has
	// five voices; the first plausible one is enough. The
	// in-memory property test already exhaustively covers slots,
	// so this just guards the writer path.
	var voiceOff int
	var voiceSlot int
	{
		init := m.Bytes()
		for slot := 0; slot < info.VoiceCount; slot++ {
			off := disk.VoiceSlotOffset(voiceAreaStart, slot)
			if off+disk.VoiceHeaderUsed > len(init) {
				break
			}
			if disk.IsActiveOrEmptyVoiceSlot(init[off : off+disk.VoiceHeaderUsed]) {
				voiceOff = off
				voiceSlot = slot
				break
			}
		}
	}

	type edit struct {
		row   row
		col   int
		fIdx  int
		value int
	}
	// One edit per row that exercises a non-trivial path; mid
	// values are picked so cross-field constraints don't kick in
	// (e.g. waveStartAt clamping against waved).
	edits := []edit{
		{row: rowDCA, col: 1, fIdx: 0, value: 7},  // level KF mid
		{row: rowDCF, col: 1, fIdx: 0, value: 50}, // cutoff mid
		{row: rowLFO, col: 1, fIdx: 1, value: 60}, // rate mid
		{row: rowSample, col: 1, fIdx: 0, value: 1},
		{row: rowLoops, col: 1, fIdx: 0, value: 0}, // sus loop 0
	}

	for _, e := range edits {
		fields := cellFields(e.row, e.col, voiceOff)
		if e.fIdx >= len(fields) {
			t.Fatalf("row %s col %d has %d fields, fIdx %d", e.row, e.col, len(fields), e.fIdx)
		}
		f := fields[e.fIdx]
		patches := f.patch(m.Bytes(), e.value)
		if len(patches) == 0 {
			// No-op: nothing to write. Skip to keep the test deterministic.
			continue
		}
		if err := m.ApplyBatch(patches); err != nil {
			t.Fatalf("edit %+v: ApplyBatch: %v", e, err)
		}
	}

	// Save (FZF path; no IMG splice involved).
	if err := m.Save(target); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reload from disk and re-validate the voice header.
	m2, info2, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("reload after Save: %v", err)
	}
	if info2.BankCount != info.BankCount {
		t.Errorf("BankCount drifted: got %d, want %d", info2.BankCount, info.BankCount)
	}
	if info2.VoiceCount != info.VoiceCount {
		t.Errorf("VoiceCount drifted: got %d, want %d", info2.VoiceCount, info.VoiceCount)
	}
	reloaded := m2.Bytes()
	if voiceOff+disk.VoiceHeaderUsed > len(reloaded) {
		t.Fatalf("reloaded buffer shrunk past the voice we edited (voice slot %d)", voiceSlot)
	}
	slot := reloaded[voiceOff : voiceOff+disk.VoiceHeaderUsed]
	if !disk.IsActiveOrEmptyVoiceSlot(slot) {
		t.Fatalf(
			"voice slot %d at %#x corrupted after Save+reload:\n"+
				"  SUS=%#x  END=%#x  DCFSus=%#x  DCFEnd=%#x  rate0=%#x  wavst=%#x  waved=%#x",
			voiceSlot, voiceOff,
			slot[disk.VoiceDCASusOffset], slot[disk.VoiceDCAEndOffset],
			slot[disk.VoiceDCFSusOffset], slot[disk.VoiceDCFEndOffset],
			slot[disk.VoiceDCARateOffset],
			slot[disk.VoiceWaveStartOffset], slot[disk.VoiceWaveEndOffset])
	}
}
