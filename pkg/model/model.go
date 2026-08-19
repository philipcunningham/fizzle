// Package model holds the patch type the container surgery and the
// session facade pass between them.
package model

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
