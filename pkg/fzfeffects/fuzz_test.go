package fzfeffects

import (
	"bytes"
	"os"
	"testing"

	"github.com/philipcunningham/fizzle/pkg/fzfinfo"
	"github.com/philipcunningham/fizzle/pkg/internal/testutil/fzfbuilder"
)

func FuzzFZFEffectsChain(f *testing.F) {
	f.Add([]byte{24, 0, 64, 32})
	f.Add([]byte{0, 127, 127, 0, 100, 50, 25, 75})
	f.Add([]byte{127, 127, 127, 127, 0, 0, 0, 0, 63, 63, 63, 63})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 4 {
			return
		}
		_, path := fzfbuilder.MakeTestFZF(t, []string{"A"})

		iterations := len(data) / 4
		if iterations > 100 {
			iterations = 100
		}
		var lastBend, lastMod, lastFoot, lastAft int
		for i := range iterations {
			bend := int(data[i*4]) % 128
			mod := int(data[i*4+1]) % 128
			foot := int(data[i*4+2]) % 128
			aft := int(data[i*4+3]) % 128

			_, err := Set(path, SetParams{
				BendRange: bend,
				ModLFP:    mod,
				FotDCA:    foot,
				AftLFP:    aft,
			})
			if err != nil {
				t.Fatalf("Set(bend=%d, mod=%d, foot=%d, aft=%d): %v", bend, mod, foot, aft, err)
			}

			got, err := Parse(path)
			if err != nil {
				t.Fatalf("Parse after Set: %v", err)
			}
			if got.BendRange != bend {
				t.Fatalf("BendRange: got %d, want %d", got.BendRange, bend)
			}
			if got.ModLFP != mod {
				t.Fatalf("ModLFP: got %d, want %d", got.ModLFP, mod)
			}
			if got.FotDCA != foot {
				t.Fatalf("FotDCA: got %d, want %d", got.FotDCA, foot)
			}
			if got.AftLFP != aft {
				t.Fatalf("AftLFP: got %d, want %d", got.AftLFP, aft)
			}

			lastBend, lastMod, lastFoot, lastAft = bend, mod, foot, aft
		}

		if _, err := fzfinfo.Parse(path); err != nil {
			t.Fatalf("fzfinfo.Parse after mutations: %v", err)
		}

		if iterations > 0 {
			snap1, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile snap1: %v", err)
			}
			if _, err := Set(path, SetParams{
				BendRange: lastBend,
				ModLFP:    lastMod,
				FotDCA:    lastFoot,
				AftLFP:    lastAft,
			}); err != nil {
				t.Fatalf("idempotency Set(bend=%d, mod=%d, foot=%d, aft=%d): %v", lastBend, lastMod, lastFoot, lastAft, err)
			}
			snap2, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile snap2: %v", err)
			}
			if !bytes.Equal(snap1, snap2) {
				t.Fatalf("idempotency violation: re-applying Set(bend=%d, mod=%d, foot=%d, aft=%d) changed file bytes", lastBend, lastMod, lastFoot, lastAft)
			}
		}
	})
}
