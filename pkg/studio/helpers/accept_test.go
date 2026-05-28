package helpers

import "testing"

func TestAcceptUnsignedHappy(t *testing.T) {
	t.Parallel()
	fn := AcceptUnsigned(127)
	cases := []struct {
		text string
		want bool
	}{
		{"", true},
		{"0", true},
		{"5", true},
		{"127", true},
		{"128", false},
		{"-1", false},
		{"abc", false},
	}
	for _, c := range cases {
		if got := fn(c.text, 0); got != c.want {
			t.Errorf("AcceptUnsigned(127)(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestAcceptSigned(t *testing.T) {
	t.Parallel()
	fn := AcceptSigned(-15, 15)
	cases := []struct {
		text string
		want bool
	}{
		{"", true},
		{"-", true},
		{"+", true},
		{"0", true},
		{"15", true},
		{"-15", true},
		{"16", false},
		{"-16", false},
		{"abc", false},
	}
	for _, c := range cases {
		if got := fn(c.text, 0); got != c.want {
			t.Errorf("AcceptSigned(-15,15)(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestAcceptNote(t *testing.T) {
	t.Parallel()
	fn := AcceptNote()
	cases := []struct {
		text string
		want bool
	}{
		{"", true},      // empty accepted
		{"C", true},     // letter alone (partial)
		{"C#", true},    // sharp partial
		{"Cb", true},    // flat partial
		{"C4", true},    // full note
		{"C-1", true},   // negative octave
		{"C 4", true},   // space between letter and octave
		{"127", true},   // raw MIDI
		{"0", true},     // raw MIDI
		{"128", false},  // out of range MIDI
		{"Z3", false},   // not a pitch
		{"C##4", false}, // double-sharp
		{"A9", false},   // A9 = MIDI 129, out of range
	}
	for _, c := range cases {
		got := fn(c.text, 0)
		if got != c.want {
			t.Errorf("AcceptNote()(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestAcceptName(t *testing.T) {
	t.Parallel()
	fn := AcceptName(12)
	cases := []struct {
		text string
		want bool
	}{
		{"", true},
		{"A", true},
		{"123456789012", true},   // exactly 12
		{"1234567890123", false}, // 13 > 12
	}
	for _, c := range cases {
		if got := fn(c.text, 0); got != c.want {
			t.Errorf("AcceptName(12)(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}
