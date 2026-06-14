package sound

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/loader"
)

// TestEditorPatch_PreservesVoicePlausibility is the headline data-
// integrity invariant for studio: every patch returned by every
// field constructor in editor.go, applied to any voice slot at any
// in-range value, must leave the voice header still satisfying
// disk.IsActiveOrEmptyVoiceSlot (i.e. a plausible active voice OR
// the explicit NoSound placeholder, both of which the FZ-1 firmware
// accepts).
//
// A failing case here is almost always a real-hardware corruption
// bug: the kind that turns a saved FZF into garbage the FZ-1
// firmware can't load. The test that catches the editor.go writing
// 0xFF into VoiceDCASusOffset / VoiceDCAEndOffset (and similar for
// DCF) lives here.
//
// Coverage shape:
//
//   - Fixture: Piano.fzf from the factory library (5 voices).
//   - For each voice slot satisfying IsPlausibleVoiceSlot in the
//     untouched fixture, walk every (row, col) cell in the Sound
//     space and every field returned by cellFields.
//   - For numeric fields (fieldUnsigned / fieldSigned), test min,
//     max, and midpoint values.
//   - For enum fields, test every option index.
//   - Text fields (voice name) are skipped: their plausibility
//     check is name-printability which the cell editor enforces at
//     input time, not via the patch path.
func TestEditorPatch_PreservesVoicePlausibility(t *testing.T) {
	src := filepath.Join("..", "..", "..", "..", "testdata", "corpus",
		"casio-fz-1-factory-library", "casio-fz-sound-disk-fl-a-piano", "Piano.fzf")
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("missing Piano.fzf fixture: %v", err)
	}

	// Load via the production loader so the container shape (banks,
	// voice area, audio area) matches what the App sees at runtime.
	dir := t.TempDir()
	target := filepath.Join(dir, "Piano.fzf")
	if err := os.WriteFile(target, raw, 0o644); err != nil { //nolint:gosec // G703: testdata fixture under repo root
		t.Fatalf("seed fixture: %v", err)
	}
	m, info, err := loader.LoadContainer(target)
	if err != nil {
		t.Fatalf("LoadContainer: %v", err)
	}

	voiceAreaStart := info.BankCount * disk.SectorSize
	maxSlots := info.VoiceCount
	if maxSlots <= 0 {
		t.Fatalf("fixture reports VoiceCount=%d; expected >0", maxSlots)
	}

	// Pre-collect the live voice offsets so we can assert
	// plausibility before AND after each patch, and report which
	// voice failed.
	type voiceRef struct {
		slot int
		off  int
	}
	var voices []voiceRef
	dataInit := m.Bytes()
	for slot := 0; slot < maxSlots; slot++ {
		off := disk.VoiceSlotOffset(voiceAreaStart, slot)
		if off+disk.VoiceHeaderUsed > len(dataInit) {
			break
		}
		if !disk.IsActiveOrEmptyVoiceSlot(dataInit[off : off+disk.VoiceHeaderUsed]) {
			continue
		}
		voices = append(voices, voiceRef{slot: slot, off: off})
	}
	if len(voices) == 0 {
		t.Fatalf("no plausible voice slots in fixture")
	}

	for _, v := range voices {
		v := v
		for r := row(0); r < numRows; r++ {
			for col := 0; col < cellCount(r); col++ {
				fields := cellFields(r, col, v.off)
				for fIdx, f := range fields {
					testCases := candidateValues(f)
					for _, val := range testCases {
						// Snapshot original bytes for restore.
						original := append([]byte(nil), m.Bytes()...)

						patches := f.patch(m.Bytes(), val)
						if len(patches) == 0 {
							// No-op patch is allowed; nothing to assert
							// beyond "voice still plausible" (already
							// true since we didn't change anything).
							continue
						}
						if err := m.ApplyBatch(patches); err != nil {
							t.Fatalf(
								"voice slot %d cell (%s,%d) field %d (%q) value %d: ApplyBatch: %v",
								v.slot, r, col, fIdx, f.label, val, err)
						}

						data := m.Bytes()
						slot := data[v.off : v.off+disk.VoiceHeaderUsed]
						if !disk.IsActiveOrEmptyVoiceSlot(slot) {
							t.Errorf(
								"voice slot %d corrupted by cell (%s,%d) field %d (%q) value %d:\n"+
									"  SUS=%#x  END=%#x  DCFSus=%#x  DCFEnd=%#x  rate0=%#x",
								v.slot, r, col, fIdx, f.label, val,
								data[v.off+disk.VoiceDCASusOffset],
								data[v.off+disk.VoiceDCAEndOffset],
								data[v.off+disk.VoiceDCFSusOffset],
								data[v.off+disk.VoiceDCFEndOffset],
								data[v.off+disk.VoiceDCARateOffset])
						}

						// Restore for the next iteration via Replace
						// rather than ApplyBatch: a full-buffer patch
						// every iteration would push ~2 MB onto the
						// unbounded undo stack and OOM the test under
						// -race on tens of thousands of iterations.
						m.Replace(original)
					}
				}
			}
		}
	}
}

// candidateValues returns the in-range values to probe for one
// field. Numeric fields get (min, max, mid). Enums get every
// option index. Text fields return nil (skipped by the caller).
func candidateValues(f field) []int {
	switch f.kind {
	case fieldUnsigned, fieldSigned:
		mid := (f.min + f.max) / 2
		if mid == f.min || mid == f.max {
			return []int{f.min, f.max}
		}
		return []int{f.min, mid, f.max}
	case fieldEnum:
		n := len(f.options)
		if n == 0 {
			return nil
		}
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	case fieldText:
		return nil
	}
	return nil
}
