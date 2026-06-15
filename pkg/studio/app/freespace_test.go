package app

import (
	"testing"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

// TestFreeSpaceBytes_UsesUsableCapacity pins F-08: studio computes free
// space from disk.UsableDataSize (physical capacity minus the 2
// reserved sectors), the same basis the CLI's disk-listing free figure
// uses, so the TUI and CLI report the same number. The old code used
// the raw physical image size, which over-reported free space by one
// reserved area (2048 bytes).
func TestFreeSpaceBytes_UsesUsableCapacity(t *testing.T) {
	if int64(disk.UsableDataSize) == int64(disk.ImageSize) {
		t.Fatal("precondition: UsableDataSize should exclude the reserved sectors")
	}
	if got := freeSpaceBytes(0); got != int64(disk.UsableDataSize) {
		t.Errorf("freeSpaceBytes(0) = %d, want UsableDataSize %d", got, disk.UsableDataSize)
	}
	const dump = 1044480
	want := int64(disk.UsableDataSize) - dump
	if got := freeSpaceBytes(dump); got != want {
		t.Errorf("freeSpaceBytes(%d) = %d, want %d", dump, got, want)
	}
}

// TestPlural pins R-03: the count-label helper avoids "1 banks".
func TestPlural(t *testing.T) {
	if got := plural(1, "bank", "banks"); got != "bank" {
		t.Errorf("plural(1) = %q, want bank", got)
	}
	if got := plural(0, "bank", "banks"); got != "banks" {
		t.Errorf("plural(0) = %q, want banks", got)
	}
	if got := plural(2, "voice", "voices"); got != "voices" {
		t.Errorf("plural(2) = %q, want voices", got)
	}
}

// TestFreeSpaceLabel pins R-01: the free label always carries a unit,
// rolls to MB at >=1000 KB, and never exceeds six cells (so "Free: " +
// label fits the 12-column minimap rail and a 140-col terminal can't
// clip the unit).
func TestFreeSpaceLabel(t *testing.T) {
	cases := []struct {
		bytes int64
		want  string
	}{
		{0, "0 KB"},
		{259 * 1024, "259 KB"},
		{999 * 1024, "999 KB"},
		{1000 * 1024, "1.0 MB"}, // rolls to MB before KB hits four digits
		{1205 * 1024, "1.2 MB"}, // the value that used to clip to "1205"
		{-5, "0 KB"},            // negative clamps to zero
	}
	for _, c := range cases {
		got := freeSpaceLabel(c.bytes)
		if got != c.want {
			t.Errorf("freeSpaceLabel(%d) = %q, want %q", c.bytes, got, c.want)
		}
		if len(got) > 6 {
			t.Errorf("freeSpaceLabel(%d) = %q is %d cells; must be <= 6", c.bytes, got, len(got))
		}
	}
}
