// The loop chain demo fixture, read through the facade the browser
// reads it through. Its five voices carry one sample and one loop
// table and differ only in which loops they name, so a designation
// that drifts stops demonstrating anything and nothing else would say
// so.
package webcore

import (
	"os"
	"path/filepath"
	"testing"
)

// The three windows every voice carries, in voice frames: a low sine,
// a mid sawtooth, and a high pulsing tone, one second each at 18 kHz.
var loopDemoWindows = [3][2]int{
	{3600, 21600},
	{21600, 39600},
	{39600, 57600},
}

const (
	loopDemoFrames = 61200
	loopDemoRate   = 18000
	loopNone       = 8 // the format's "none" for both designations
)

func loopDemo(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "synthetic", "LOOPDEMO.img"))
	if err != nil {
		t.Fatalf("read LOOPDEMO fixture: %v", err)
	}
	return data
}

func TestLoopDemoVoicesNameTheChainTheyDemonstrate(t *testing.T) {
	cases := []struct {
		name    string
		sustain int
		release int
		// The loop a held key repeats, min(loop_sus, loop_end)
		// (F000:122B), and the one the key coming up moves to
		// (F000:1515).
		held  int
		freed int
	}{
		{name: "1 LOW HIGH", sustain: 0, release: 2, held: 0, freed: 2},
		{name: "2 MID BOTH", sustain: 1, release: 1, held: 1, freed: 1},
		// The cap is the end loop from note on, which a voice naming
		// only a sustain loop cannot show.
		{name: "3 HIGH ONLY", sustain: loopNone, release: 2, held: 2, freed: 2},
		{name: "4 LOW ONLY", sustain: 0, release: loopNone, held: 0, freed: loopNone},
		{name: "5 NO LOOP", sustain: loopNone, release: loopNone, held: loopNone, freed: loopNone},
	}

	s := NewSession()
	if _, cerr := s.OpenImage(loopDemo(t)); cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	inst := instrument(t, s)
	if len(inst.Voices) != len(cases) {
		t.Fatalf("voices = %d, want %d", len(inst.Voices), len(cases))
	}

	for i, want := range cases {
		got := inst.Voices[i]
		t.Run(want.name, func(t *testing.T) {
			if got.Name != want.name {
				t.Fatalf("slot %d is %q, want %q", i, got.Name, want.name)
			}
			d := got.Voice
			if d == nil {
				t.Fatal("no voice detail")
			}
			if d.Frames != loopDemoFrames || d.SampleRate != loopDemoRate {
				t.Errorf("%d frames at %d Hz, want %d at %d",
					d.Frames, d.SampleRate, loopDemoFrames, loopDemoRate)
			}
			if d.LoopSustain != want.sustain || d.LoopRelease != want.release {
				t.Errorf("loop_sus=%d loop_end=%d, want %d and %d",
					d.LoopSustain, d.LoopRelease, want.sustain, want.release)
			}

			for j, window := range loopDemoWindows {
				loop := d.Loops[j]
				if loop.Start != window[0] || loop.End != window[1] {
					t.Errorf("loop %d is %d to %d, want %d to %d",
						j+1, loop.Start, loop.End, window[0], window[1])
				}
			}
			// The rest read as no loop, or a rule that walks the table
			// finds a window where the voice names none.
			for j := len(loopDemoWindows); j < len(d.Loops); j++ {
				if loop := d.Loops[j]; loop.End > loop.Start {
					t.Errorf("loop %d spans %d to %d, want an empty range",
						j+1, loop.Start, loop.End)
				}
			}

			if held := min(d.LoopSustain, d.LoopRelease); held != want.held {
				t.Errorf("a held key repeats loop %d, want %d", held, want.held)
			}
			if d.LoopRelease != want.freed {
				t.Errorf("the key coming up moves to loop %d, want %d", d.LoopRelease, want.freed)
			}
		})
	}
}

// The end loop is audible only while the voice still sounds, so a
// fixture that lost its release would name its loops and demonstrate
// nothing.
func TestLoopDemoEnvelopesOutlastTheKey(t *testing.T) {
	s := NewSession()
	if _, cerr := s.OpenImage(loopDemo(t)); cerr != nil {
		t.Fatalf("OpenImage: %v", cerr)
	}
	inst := instrument(t, s)

	for _, v := range inst.Voices {
		dca := v.Voice.Dca
		oneShot := v.Name == "5 NO LOOP"
		if oneShot {
			// Sustain at or past end spells a voice that plays through.
			if dca.Sustain < dca.End {
				t.Errorf("%s: DCA sustain %d below end %d, so it holds rather than playing through",
					v.Name, dca.Sustain, dca.End)
			}
			continue
		}
		if dca.Sustain >= dca.End {
			t.Errorf("%s: DCA sustain %d at or past end %d, so the key coming up runs no stages",
				v.Name, dca.Sustain, dca.End)
		}
		// The end stage carries the fall, so it is the one that has to
		// take time: at the top rate it is over before the moved
		// window is heard.
		if rate := dca.Rates[dca.End]; rate == 0 || rate > 50 {
			t.Errorf("%s: DCA end stage rate %d, want a fall slow enough to hear", v.Name, rate)
		}
	}
}
