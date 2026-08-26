// Package document owns the canonical bytes and provenance of an editable FZ
// disk document. Application facades keep history and project this state, but
// cannot mutate its images independently.
package document

import (
	"bytes"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

// Authority identifies the source context retained with document bytes.
type Authority uint8

const (
	// AuthorityWalk means layout was inferred from bounded document bytes.
	AuthorityWalk Authority = iota
	// AuthorityDIS means layout was resolved under the disk entry's metadata.
	AuthorityDIS
)

// State is an immutable one- or two-image FZ document.
type State struct {
	image1    []byte
	image2    []byte
	authority Authority
}

// NewState validates and owns a document's images and parse authority.
func NewState(image1, image2 []byte, authority Authority) (State, error) {
	if image1 == nil {
		if image2 != nil {
			return State{}, fmt.Errorf("document: disk 2 cannot exist without disk 1")
		}
		return State{authority: authority}, nil
	}
	for i, data := range [][]byte{image1, image2} {
		if data == nil {
			continue
		}
		img, err := disk.ReadImage(bytes.NewReader(data))
		if err != nil {
			return State{}, fmt.Errorf("document: disk %d: %w", i+1, err)
		}
		if img.Bytes()[disk.DiskNameTagOffset] != disk.DiskNameTag {
			return State{}, fmt.Errorf("document: disk %d has no FZ identification tag", i+1)
		}
	}
	return State{image1: bytes.Clone(image1), image2: bytes.Clone(image2), authority: authority}, nil
}

// IsOpen reports whether the state contains disk 1.
func (s State) IsOpen() bool { return s.image1 != nil }

// HasSecondDisk reports whether the state contains disk 2.
func (s State) HasSecondDisk() bool { return s.image2 != nil }

// UsesDIS reports whether disk metadata supplied the retained parse authority.
func (s State) UsesDIS() bool { return s.authority == AuthorityDIS }

// Authority returns the retained parse authority.
func (s State) Authority() Authority { return s.authority }

// SizeBytes returns the total owned image bytes.
func (s State) SizeBytes() int { return len(s.image1) + len(s.image2) }

// Image1 returns an owned copy of disk 1, or nil for an empty state.
func (s State) Image1() []byte { return bytes.Clone(s.image1) }

// Image2 returns an owned copy of disk 2, or nil for a one-disk state.
func (s State) Image2() []byte { return bytes.Clone(s.image2) }

// Image returns an owned copy of disk index 0 or 1.
func (s State) Image(index int) ([]byte, error) {
	switch index {
	case 0:
		if s.image1 == nil {
			return nil, fmt.Errorf("document: no disk is open")
		}
		return bytes.Clone(s.image1), nil
	case 1:
		if s.image2 == nil {
			return nil, fmt.Errorf("document: disk 2 is not open")
		}
		return bytes.Clone(s.image2), nil
	default:
		return nil, fmt.Errorf("document: disk index must be 0 or 1, got %d", index)
	}
}
