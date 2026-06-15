package fznote

import "testing"

// TestName pins the note-name formatting and the out-of-range "?" guard.
func TestName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		midi int
		want string
	}{
		{0, "C-1"}, {21, "A0"}, {60, "C4"}, {69, "A4"}, {127, "G9"},
		{-1, "?"}, {128, "?"},
	}
	for _, c := range cases {
		if got := Name(c.midi); got != c.want {
			t.Errorf("Name(%d) = %q, want %q", c.midi, got, c.want)
		}
	}
}
