package app

import (
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

// TestSanitizeImageLabel pins the disk-label sanitiser behaviour so the
// shared-helper refactor (delegating the per-char rule to
// normaliseRenameKey) is proven equivalent: extension stripped,
// lowercase upper-cased, only A-Z/0-9/space/hyphen kept, capped at
// disk.LabelSize, and the "FZ-DISK" fallback for an empty result.
func TestSanitizeImageLabel(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"my song.wav", "MY SONG"},
		{"lowercase", "LOWERCASE"},
		{"my-disk.img", "MY-DISK"},
		{"weird@name!.fzf", "WEIRDNAME"},
		{"", defaultDiskLabel},
		{"!!!.img", defaultDiskLabel},
		{"abcdefghijklmnop.img", "ABCDEFGHIJKL"}, // capped to disk.LabelSize (12)
	}
	for _, c := range cases {
		if got := sanitizeImageLabel(c.in); got != c.want {
			t.Errorf("sanitizeImageLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if disk.LabelSize != 12 {
		t.Fatalf("disk.LabelSize = %d, test assumes 12", disk.LabelSize)
	}
}

// TestNormaliseRenameKey pins the per-byte FZ-name rule that
// sanitizeImageLabel now shares.
func TestNormaliseRenameKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   byte
		want byte
	}{
		{'A', 'A'}, {'Z', 'Z'},
		{'a', 'A'}, {'z', 'Z'},
		{'0', '0'}, {'9', '9'},
		{' ', ' '}, {'-', '-'},
		{'_', 0}, {'@', 0}, {'!', 0}, {0x80, 0},
	}
	for _, c := range cases {
		if got := normaliseRenameKey(c.in); got != c.want {
			t.Errorf("normaliseRenameKey(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
