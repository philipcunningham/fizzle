package areaeditor

import (
	"testing"

	"pgregory.net/rapid"
)

// TestAreaEditor_InvariantsHoldAcrossKeyStream is the state-machine
// integrity check: drive HandleKey with a random stream of valid
// inputs and assert every cross-field invariant after each step.
// If a sequence breaks an invariant, rapid shrinks to a minimal
// failing case.
//
// Invariants:
//
//   - KeyLow / KeyHigh / KeyOrig in [0, 127].
//   - VelLow / VelHigh / Volume   in [0, 127].
//   - MIDIChan in [0, 15].
//   - AudioOut in [0, 255].
//   - KeyLow <= KeyHigh           (cross-field; HandleKey drags the
//     other endpoint when one would overshoot it, by design).
//   - VelLow <= VelHigh.
//
// These cover the "key-low <= high / vel reject-and-revert" rule
// the test plan called out, with the actual implemented behaviour
// (drag-the-other-endpoint, not reject-and-revert).
func TestAreaEditor_InvariantsHoldAcrossKeyStream(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		seed := SeedValues{
			KeyLow:   rapid.IntRange(0, 127).Draw(rt, "seedKeyLow"),
			KeyHigh:  rapid.IntRange(0, 127).Draw(rt, "seedKeyHigh"),
			KeyOrig:  rapid.IntRange(0, 127).Draw(rt, "seedKeyOrig"),
			VelLow:   rapid.IntRange(0, 127).Draw(rt, "seedVelLow"),
			VelHigh:  rapid.IntRange(0, 127).Draw(rt, "seedVelHigh"),
			Volume:   rapid.IntRange(0, 127).Draw(rt, "seedVolume"),
			AudioOut: rapid.IntRange(0, 255).Draw(rt, "seedAudioOut"),
			MIDIChan: rapid.IntRange(0, 15).Draw(rt, "seedMIDIChan"),
		}
		// Pin the seed's cross-field invariants the way Open will:
		// if seedKeyLow > seedKeyHigh, both clamp to the same value
		// downstream (HandleKey enforces the relationship), so we
		// expect any unrelated initial seed to be "fixed up" by the
		// first key press. We assert invariants after each step
		// regardless of seed shape.
		var m Model
		m.Open(0, 0, seed)

		keys := []string{"tab", "shift+tab", "up", "down", "shift+up", "shift+down"}
		n := rapid.IntRange(0, 60).Draw(rt, "numKeys")
		for i := 0; i < n; i++ {
			k := rapid.SampledFrom(keys).Draw(rt, "key")
			m.HandleKey(k)
			assertInvariants(rt, m, i, k)
		}
	})
}

func assertInvariants(rt *rapid.T, m Model, step int, key string) {
	rt.Helper()
	if m.keyLow < 0 || m.keyLow > 127 {
		rt.Fatalf("step %d after %q: keyLow=%d out of [0,127]", step, key, m.keyLow)
	}
	if m.keyHigh < 0 || m.keyHigh > 127 {
		rt.Fatalf("step %d after %q: keyHigh=%d out of [0,127]", step, key, m.keyHigh)
	}
	if m.keyOrig < 0 || m.keyOrig > 127 {
		rt.Fatalf("step %d after %q: keyOrig=%d out of [0,127]", step, key, m.keyOrig)
	}
	if m.velLow < 0 || m.velLow > 127 {
		rt.Fatalf("step %d after %q: velLow=%d out of [0,127]", step, key, m.velLow)
	}
	if m.velHigh < 0 || m.velHigh > 127 {
		rt.Fatalf("step %d after %q: velHigh=%d out of [0,127]", step, key, m.velHigh)
	}
	if m.volume < 0 || m.volume > 127 {
		rt.Fatalf("step %d after %q: volume=%d out of [0,127]", step, key, m.volume)
	}
	if m.midiChan < 0 || m.midiChan > 15 {
		rt.Fatalf("step %d after %q: midiChan=%d out of [0,15]", step, key, m.midiChan)
	}
	if m.audioOut < 0 || m.audioOut > 255 {
		rt.Fatalf("step %d after %q: audioOut=%d out of [0,255]", step, key, m.audioOut)
	}
	if m.keyLow > m.keyHigh {
		rt.Fatalf("step %d after %q: keyLow=%d > keyHigh=%d", step, key, m.keyLow, m.keyHigh)
	}
	if m.velLow > m.velHigh {
		rt.Fatalf("step %d after %q: velLow=%d > velHigh=%d", step, key, m.velLow, m.velHigh)
	}
}

// TestAreaEditor_AudioOutExtraSurvivesCycle pins the "multi-bit
// gchn isn't lost on first nudge" fix. Loading 0x05 (outputs 1+3)
// should not auto-snap to a canonical state when the user steps
// past it; it should sit between 0x04 and 0x08 in the cycle.
func TestAreaEditor_AudioOutExtraSurvivesCycle(t *testing.T) {
	var m Model
	m.Open(0, 0, SeedValues{AudioOut: 0x05})
	if m.AudioOut() != 0x05 {
		t.Fatalf("Open dropped multi-bit AudioOut: got %#x, want 0x05", m.AudioOut())
	}
	// Focus AudioOut.
	for m.field != FieldAudioOut {
		m.HandleKey("tab")
	}
	// One step up from 0x05 should hit 0x08 (the next canonical entry).
	m.HandleKey("up")
	if got := m.AudioOut(); got != 0x08 {
		t.Fatalf("Up from 0x05 -> %#x, want 0x08", got)
	}
	// One step down from 0x08 should return to 0x05 (the extra
	// stays in the cycle for this modal session).
	m.HandleKey("down")
	if got := m.AudioOut(); got != 0x05 {
		t.Fatalf("Down from 0x08 -> %#x, want 0x05 (extra dropped)", got)
	}
}

// TestAreaEditor_OpenClampsOutOfRangeSeeds pins that bogus seeds
// (e.g. from a corrupt file) get clamped at Open. The widget must
// never carry an out-of-range value through to commit, since the
// App's patcher would then write it back to disk.
func TestAreaEditor_OpenClampsOutOfRangeSeeds(t *testing.T) {
	cases := []struct {
		name string
		in   SeedValues
		want SeedValues
	}{
		{
			name: "negative-everywhere",
			in: SeedValues{
				KeyLow: -5, KeyHigh: -5, KeyOrig: -5,
				VelLow: -5, VelHigh: -5,
				Volume: -5, AudioOut: -5, MIDIChan: -5,
			},
			want: SeedValues{
				KeyLow: 0, KeyHigh: 0, KeyOrig: 0,
				VelLow: 0, VelHigh: 0,
				Volume: 0, AudioOut: 0, MIDIChan: 0,
			},
		},
		{
			name: "overflow-everywhere",
			in: SeedValues{
				KeyLow: 999, KeyHigh: 999, KeyOrig: 999,
				VelLow: 999, VelHigh: 999,
				Volume: 999, AudioOut: 999, MIDIChan: 999,
			},
			want: SeedValues{
				KeyLow: 127, KeyHigh: 127, KeyOrig: 127,
				VelLow: 127, VelHigh: 127,
				Volume: 127, AudioOut: 255, MIDIChan: 15,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m Model
			m.Open(0, 0, tc.in)
			if m.KeyLow() != tc.want.KeyLow {
				t.Errorf("KeyLow=%d, want %d", m.KeyLow(), tc.want.KeyLow)
			}
			if m.KeyHigh() != tc.want.KeyHigh {
				t.Errorf("KeyHigh=%d, want %d", m.KeyHigh(), tc.want.KeyHigh)
			}
			if m.KeyOrig() != tc.want.KeyOrig {
				t.Errorf("KeyOrig=%d, want %d", m.KeyOrig(), tc.want.KeyOrig)
			}
			if m.VelLow() != tc.want.VelLow {
				t.Errorf("VelLow=%d, want %d", m.VelLow(), tc.want.VelLow)
			}
			if m.VelHigh() != tc.want.VelHigh {
				t.Errorf("VelHigh=%d, want %d", m.VelHigh(), tc.want.VelHigh)
			}
			if m.Volume() != tc.want.Volume {
				t.Errorf("Volume=%d, want %d", m.Volume(), tc.want.Volume)
			}
			if m.AudioOut() != tc.want.AudioOut {
				t.Errorf("AudioOut=%d, want %d", m.AudioOut(), tc.want.AudioOut)
			}
			if m.MIDIChan() != tc.want.MIDIChan {
				t.Errorf("MIDIChan=%d, want %d", m.MIDIChan(), tc.want.MIDIChan)
			}
		})
	}
}
