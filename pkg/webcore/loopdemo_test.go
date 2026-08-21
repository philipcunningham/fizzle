// The loop chain demo fixture, read through the facade the browser
// reads it through. LOOPDEMO.img exists so the cap rule can be heard:
// five voices carry the same sample and the same loop table, and
// differ only in which loops they name. A voice whose designations
// drift stops demonstrating anything, and nothing else in the suite
// would say so, hence this file.
package webcore

import (
	"os"
	"path/filepath"
	"testing"
)

// loopDemoWindows are the three loop windows every voice in the
// fixture carries, in voice frames. The sample runs at 18 kHz, so
// each window is one second: a low sine, a mid sawtooth, and a high
// pulsing tone, each unmistakable against the others.
var loopDemoWindows = [3][2]int{
	{3600, 21600},
	{21600, 39600},
	{39600, 57600},
}

const (
	loopDemoFrames = 61200
	loopDemoRate   = 18000
	// The format's "none" for both designations.
	loopNone = 8
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
		// held is the loop a held key repeats. Note on caps the chain
		// at min(loop_sus, loop_end) (F000:122B), so it is the lower
		// of the two designations rather than the sustain one.
		held int
		// freed is the loop the chain moves to when the key comes up,
		// since note off raises the cap to loop_end (F000:1515).
		freed int
	}{
		// The headline: a held key repeats the low window, and the key
		// coming up moves the chain to the high one.
		{name: "1 LOW HIGH", sustain: 0, release: 2, held: 0, freed: 2},
		// One loop in both roles, so the cap never moves.
		{name: "2 MID BOTH", sustain: 1, release: 1, held: 1, freed: 1},
		// No sustain loop and an end loop at 2: the cap is the end
		// loop from note on, which is the half of the rule a voice
		// naming only a sustain loop cannot show.
		{name: "3 HIGH ONLY", sustain: loopNone, release: 2, held: 2, freed: 2},
		// A sustain loop and no end loop: nothing moves at the key.
		{name: "4 LOW ONLY", sustain: 0, release: loopNone, held: 0, freed: loopNone},
		// Nothing designated, so the sample plays through once.
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

			// Every voice carries the same three windows, so what
			// differs between them is the designations alone.
			for j, window := range loopDemoWindows {
				loop := d.Loops[j]
				if loop.Start != window[0] || loop.End != window[1] {
					t.Errorf("loop %d is %d to %d, want %d to %d",
						j+1, loop.Start, loop.End, window[0], window[1])
				}
			}
			// The five loops the fixture leaves undesignated have to
			// read as no loop, or a rule that walks the table finds a
			// window where the voice names none.
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

// The end loop is only audible if the voice keeps sounding after the
// key comes up, which is the DCA's job: a sustain stage below the end
// stage leaves stages to run on note off. A fixture that lost its
// release would still name its loops and demonstrate nothing.
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
			// Sustain at or past end is how the format spells a voice
			// that plays through and frees its own slot.
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
		// A release that falls to nothing at the top rate is over
		// before the moved window is heard. The end stage carries the
		// fall, so it is the one that has to take time.
		if rate := dca.Rates[dca.End]; rate == 0 || rate > 50 {
			t.Errorf("%s: DCA end stage rate %d, want a fall slow enough to hear", v.Name, rate)
		}
	}
}
