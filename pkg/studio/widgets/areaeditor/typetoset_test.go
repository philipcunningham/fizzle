package areaeditor

import "testing"

// TestTypeToSet pins UXE: numeric fields in the Area editor accept typed
// digits, matching the Sound editor, instead of only stepping ±1/±12.
func TestTypeToSet(t *testing.T) {
	m := New()
	m.Open(0, 0, SeedValues{KeyLow: 0, KeyHigh: 127, VelLow: 1, VelHigh: 127, Volume: 0})

	// Focus starts on Key Low; typing "36" sets it directly.
	m.HandleKey("3")
	m.HandleKey("6")
	if m.KeyLow() != 36 {
		t.Errorf("KeyLow after typing 36 = %d, want 36", m.KeyLow())
	}

	// Backspace edits the typed value.
	m.HandleKey("backspace")
	if m.KeyLow() != 3 {
		t.Errorf("KeyLow after backspace = %d, want 3", m.KeyLow())
	}

	// Moving to another field clears the typed buffer; typing on Volume
	// sets it fresh (not appended to Key Low's digits).
	m.HandleKey("tab") // -> Key High
	m.HandleKey("tab") // -> Key Orig
	m.HandleKey("tab") // -> Vel Low
	m.HandleKey("tab") // -> Vel High
	m.HandleKey("tab") // -> Volume
	m.HandleKey("9")
	m.HandleKey("9")
	if m.Volume() != 99 {
		t.Errorf("Volume after typing 99 = %d, want 99", m.Volume())
	}

	// Audio Out is a bitmask cycle, not type-settable. Typing a digit there
	// must be ignored and must not bleed into the previously set numeric
	// field (Volume stays 99).
	m.HandleKey("tab") // -> Audio Out (non-numeric)
	m.HandleKey("5")
	if m.Volume() != 99 {
		t.Errorf("typing on non-numeric Audio Out mutated Volume = %d, want 99 unchanged", m.Volume())
	}

	// Out-of-range typing on a numeric field clamps (Key fields are 0..127).
	m2 := New()
	m2.Open(0, 0, SeedValues{KeyLow: 0, KeyHigh: 127})
	m2.HandleKey("9")
	m2.HandleKey("9")
	m2.HandleKey("9") // 999 -> clamp 127
	if m2.KeyLow() != 127 {
		t.Errorf("KeyLow after typing 999 = %d, want clamp 127", m2.KeyLow())
	}
}
