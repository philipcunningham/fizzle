package effectseditor

import (
	"testing"

	"pgregory.net/rapid"
)

// TestEffectsEditor_AllCellsInRangeAfterAnyKeyStream asserts the
// integrity invariant: every cell in the 22-cell effect block
// (bend + 3x7 modulation matrix) stays in [0, 127] no matter what
// sequence of HandleKey calls drives the modal.
//
// The FZ-1 effect bytes are 7-bit values; any byte > 127 would
// either be silently truncated by the firmware or cause undefined
// behaviour. studio1 writes 0..127; studio must match.
func TestEffectsEditor_AllCellsInRangeAfterAnyKeyStream(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		var seed SeedValues
		for i := 0; i < numCells; i++ {
			seed.Cells[i] = uint8(rapid.IntRange(0, 127).Draw(rt, "seedCell")) //nolint:gosec // G115: rapid draw bounded to 0..127
		}
		var m Model
		m.Open(0, seed)

		keys := []string{"tab", "shift+tab", "up", "down", "shift+up", "shift+down"}
		n := rapid.IntRange(0, 80).Draw(rt, "numKeys")
		for i := 0; i < n; i++ {
			k := rapid.SampledFrom(keys).Draw(rt, "key")
			m.HandleKey(k)
			cells := m.Cells()
			for ci, c := range cells {
				if c > 127 {
					rt.Fatalf("step %d after %q: cell %d = %d > 127", i, k, ci, c)
				}
			}
		}
	})
}

// TestEffectsEditor_OpenClampsOutOfRangeSeeds pins that bytes
// loaded from disk above 127 (corruption / future-format bits)
// don't leak through to commit unchanged. Without clamping, a
// stray 0xFF byte in the effect block would persist on save.
//
// Today the Open path does NOT clamp; it preserves the raw
// uint8s. This test documents the gap so the next iteration can
// decide between (a) clamping at Open, (b) clamping at commit,
// or (c) tolerating high bits if the firmware doesn't care.
func TestEffectsEditor_OpenPreservesSeedBytesAsIs(t *testing.T) {
	var seed SeedValues
	seed.Cells[0] = 200          // bend > 127
	seed.Cells[1] = 0xFF         // modulation cell > 127
	seed.Cells[numCells-1] = 130 // last matrix cell > 127

	var m Model
	m.Open(0, seed)

	cells := m.Cells()
	if cells[0] != 200 || cells[1] != 0xFF || cells[numCells-1] != 130 {
		t.Fatalf(
			"Open mutated seed bytes: cells[0]=%d cells[1]=%d cells[last]=%d",
			cells[0], cells[1], cells[numCells-1])
	}
}

// TestEffectsEditor_StepClampsAtBounds pins that a single Up step
// from 127 stays at 127 (no overflow into 128) and a single Down
// step from 0 stays at 0 (no wrap to 255).
func TestEffectsEditor_StepClampsAtBounds(t *testing.T) {
	var seed SeedValues
	seed.Cells[0] = 127
	seed.Cells[1] = 0

	var m Model
	m.Open(0, seed)

	// Focus cell 0 (bend); step up.
	m.HandleKey("up")
	if got := m.Cells()[0]; got != 127 {
		t.Fatalf("Up from 127 -> %d, want 127", got)
	}
	// Move to cell 1; step down.
	m.HandleKey("tab")
	m.HandleKey("down")
	if got := m.Cells()[1]; got != 0 {
		t.Fatalf("Down from 0 -> %d, want 0", got)
	}
}
