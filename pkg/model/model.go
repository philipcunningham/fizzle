// Package model holds the patch type the container surgery and the
// session facade pass between them.
package model

import (
	"bytes"
	"fmt"
	"sort"
)

// Patch records a single byte-range mutation.
//
// Offset is the absolute byte offset into the container. Old is the
// expected pre-image at that offset, so an applier rejects a patch
// whose bytes went stale. New is the post-image, the same length as
// Old.
type Patch struct {
	Offset int
	Old    []byte
	New    []byte
}

// Apply validates and atomically applies fixed-size patches to data. Every
// pre-image is checked before writing, so a stale caller fails loudly rather
// than corrupting the container. Empty patches are allowed at offsets from
// zero through len(data).
func Apply(data []byte, patches []Patch) error {
	type patchRange struct {
		start int
		end   int
	}
	ranges := make([]patchRange, 0, len(patches))
	for _, p := range patches {
		if len(p.Old) != len(p.New) {
			return fmt.Errorf("patch at %d changes length from %d to %d", p.Offset, len(p.Old), len(p.New))
		}
		if p.Offset < 0 || p.Offset > len(data)-len(p.Old) {
			return fmt.Errorf("patch at %d out of bounds", p.Offset)
		}
		if !bytes.Equal(data[p.Offset:p.Offset+len(p.Old)], p.Old) {
			return fmt.Errorf("patch pre-image mismatch at %d", p.Offset)
		}
		if len(p.Old) > 0 {
			ranges = append(ranges, patchRange{start: p.Offset, end: p.Offset + len(p.Old)})
		}
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	for i := 1; i < len(ranges); i++ {
		if ranges[i].start < ranges[i-1].end {
			return fmt.Errorf("patch at %d overlaps patch ending at %d", ranges[i].start, ranges[i-1].end)
		}
	}
	for _, p := range patches {
		copy(data[p.Offset:p.Offset+len(p.New)], p.New)
	}
	return nil
}
