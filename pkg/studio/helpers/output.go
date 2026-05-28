package helpers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

// OutputAll is the display string for the "all generators" gchn bitmask
// (0xff). Exposed so widget code that builds DropDown option lists can
// share the literal with FormatOutput / ParseOutput.
const OutputAll = "all"

// FormatOutput renders a gchn bitmask byte for the bank-area Output column.
// Delegates to disk.FormatAudioOut so the studio and CLI agree on the
// display:
//
//	0xff       -> "all"
//	0x00       -> "none"
//	single bit -> "N"   (e.g. 0x04 -> "3")
//	multi bit  -> comma-separated, ascending (e.g. 0x05 -> "1,3")
func FormatOutput(b uint8) string {
	return disk.FormatAudioOut(b)
}

// ParseOutput is the inverse of FormatOutput. It accepts:
//   - "all" or "poly" (case-insensitive) -> 0xff
//   - a single generator number "1".."8" -> the corresponding single-bit mask
//   - a comma list "1,3" -> the OR of the listed generator bits
//
// "none" / empty string returns an error; an output of 0 would silently route
// the area nowhere, and forcing the caller to provide at least one generator
// avoids the silent-misroute trap.
func ParseOutput(s string) (uint8, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, fmt.Errorf("helpers: empty output")
	}
	low := strings.ToLower(t)
	if low == OutputAll || low == "poly" {
		return disk.PolyphonicAudioOut, nil
	}
	var mask uint8
	parts := strings.Split(t, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, fmt.Errorf("helpers: parsing output %q: %w", s, err)
		}
		if n < 1 || n > disk.MaxGenerators {
			return 0, fmt.Errorf("helpers: output %d out of range (1-%d)", n, disk.MaxGenerators)
		}
		mask |= 1 << (n - 1)
	}
	return mask, nil
}
