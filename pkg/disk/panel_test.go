package disk

import "testing"

// Every case here is a value read off the FZ-1 front panel running
// under the fizzlab emulator. The numbers are measurements, not
// choices, so don't change them to make code pass.

func TestTuneWordToDisplayMatchesThePanel(t *testing.T) {
	for _, c := range []struct{ word, display int }{
		{0, 0}, {51, 20}, {127, 50}, {128, 51}, {200, 79}, {255, 100},
		{256, 0}, {257, 1}, {300, 18}, {512, 0}, {1000, 91}, {5000, 54},
		{-1, -1}, {-50, -20}, {-127, -50}, {-128, -51}, {-255, -100},
		{-256, -101}, {-300, -18}, {-512, -101}, {-513, -1}, {-1000, -91}, {-5000, -54},
	} {
		if got := TuneWordToDisplay(int16(c.word)); got != c.display { //nolint:gosec // fixture values are in range
			t.Errorf("TuneWordToDisplay(%d) = %d, want %d", c.word, got, c.display)
		}
	}
}

func TestTuneDisplayToWordClampsToThePanel(t *testing.T) {
	for _, c := range []struct{ display, word int }{
		{0, 0}, {1, 2}, {10, 25}, {50, 127}, {100, 255},
		{-100, -255}, {900, 255}, {-900, -255},
	} {
		if got := TuneDisplayToWord(c.display); int(got) != c.word {
			t.Errorf("TuneDisplayToWord(%d) = %d, want %d", c.display, got, c.word)
		}
	}
}

// Property: inside the panel's own span, a display value survives a
// trip through the stored word. Outside it the panel wraps, which the
// table above pins; this is the range fizzle lets a user reach.
func TestTuneRoundTripsInsideThePanelRange(t *testing.T) {
	for display := MinTuneDisplay; display <= MaxTuneDisplay; display++ {
		if got := TuneWordToDisplay(TuneDisplayToWord(display)); got != display {
			t.Errorf("tune %d round trips to %d", display, got)
		}
	}
}

// Property: no display value, however wild, produces a word outside
// the span the panel can dial. That is the hard limit, stated once.
func TestTuneDisplayToWordNeverLeavesThePanelSpan(t *testing.T) {
	for display := -40000; display <= 40000; display += 7 {
		word := int(TuneDisplayToWord(display))
		if word < -255 || word > 255 {
			t.Fatalf("TuneDisplayToWord(%d) = %d, outside -255 to 255", display, word)
		}
	}
}

func TestLFODelayAndAttackMatchThePanel(t *testing.T) {
	for _, c := range []struct {
		display, word int
		attack        uint8
	}{
		{1, 16, 17}, {2, 32, 17}, {10, 160, 16},
		{50, 800, 11}, {100, 1600, 5}, {127, 2032, 2},
	} {
		if got := LFODelayDisplayToWord(c.display); int(got) != c.word {
			t.Errorf("delay display %d gives word %d, want %d", c.display, got, c.word)
		}
		if got := LFOAttackForDelay(c.display); got != c.attack {
			t.Errorf("delay display %d gives attack %d, want %d", c.display, got, c.attack)
		}
	}
	if got := LFODelayWordToDisplay(5000); got != 312 {
		t.Errorf("word 5000 gives display %d, want 312", got)
	}
	if got := LFODelayDisplayToWord(900); got != 2032 {
		t.Errorf("delay display 900 gives word %d, want 2032", got)
	}
}

// Property: every delay the panel can dial round trips, and its word is
// always a whole number of steps. The panel moves in 16s.
func TestLFODelayRoundTripsAndStepsBySixteen(t *testing.T) {
	for display := 0; display <= MaxLFODelayDisplay; display++ {
		word := LFODelayDisplayToWord(display)
		if int(word)%LFODelayStep != 0 {
			t.Errorf("delay %d gives word %d, not a multiple of %d", display, word, LFODelayStep)
		}
		if got := LFODelayWordToDisplay(word); got != display {
			t.Errorf("delay %d round trips to %d", display, got)
		}
	}
}

// Property: the attack the panel derives never leaves the span the
// panel itself writes, which measurement puts at 2 to 18.
func TestLFOAttackForDelayStaysInThePanelSpan(t *testing.T) {
	for display := -50; display <= 500; display++ {
		if got := LFOAttackForDelay(display); got < 2 || got > 18 {
			t.Fatalf("delay %d gives attack %d, outside 2 to 18", display, got)
		}
	}
}

func TestVelDCQByteToDisplayMatchesThePanel(t *testing.T) {
	for _, c := range []struct {
		b       uint8
		display int
	}{
		{0, 0}, {50, 50}, {127, 127}, {128, 128}, {200, 56}, {206, 50}, {255, 1},
	} {
		if got := VelDCQByteToDisplay(c.b); got != c.display {
			t.Errorf("VelDCQByteToDisplay(%d) = %d, want %d", c.b, got, c.display)
		}
	}
}

func TestAreaLevelConversionMatchesThePanel(t *testing.T) {
	for _, c := range []struct {
		display int
		stored  uint8
	}{
		{0, 127}, {1, 126}, {30, 97}, {64, 63}, {127, 0},
	} {
		if got := AreaLevelFromByte(c.stored); got != c.display {
			t.Errorf("AreaLevelFromByte(%d) = %d, want %d", c.stored, got, c.display)
		}
		if got := AreaLevelToByte(c.display); got != c.stored {
			t.Errorf("AreaLevelToByte(%d) = %d, want %d", c.display, got, c.stored)
		}
	}
	if got := AreaLevelToByte(-5); got != 127 {
		t.Errorf("AreaLevelToByte(-5) = %d, want 127", got)
	}
	if got := AreaLevelToByte(900); got != 0 {
		t.Errorf("AreaLevelToByte(900) = %d, want 0", got)
	}
}

// Property: the area level is its own inverse across the whole span, so
// reading a level and writing it back never moves the byte.
func TestAreaLevelRoundTripsBothWays(t *testing.T) {
	for display := 0; display <= MaxAreaLevel; display++ {
		if got := AreaLevelFromByte(AreaLevelToByte(display)); got != display {
			t.Errorf("area level %d round trips to %d", display, got)
		}
	}
	for b := 0; b <= MaxAreaLevel; b++ {
		if got := AreaLevelToByte(AreaLevelFromByte(uint8(b))); int(got) != b { //nolint:gosec // bounded by the loop
			t.Errorf("area byte %d round trips to %d", b, got)
		}
	}
}
