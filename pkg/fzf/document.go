// Package fzf provides the canonical byte-preserving model of an FZF dump.
package fzf

import (
	"bytes"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
)

// Document owns an FZF dump and the layout resolved for its source context.
// Construction copies the input, and Bytes returns a fresh copy, so callers
// cannot change the bytes independently of the retained layout.
type Document struct {
	data   []byte
	layout fzutil.FZFLayout
}

// NewStandalone constructs a document using standalone marker and walk policy.
func NewStandalone(data []byte) (*Document, error) {
	owned := bytes.Clone(data)
	layout, err := fzutil.ResolveStandaloneFZFLayout(owned)
	if err != nil {
		return nil, err
	}
	return newDocument(owned, layout), nil
}

// NewDiskFile constructs a document using the disk directory's voice count.
// A count that is in range but cannot be validated above the bounded walk
// falls back to the walk; callers can distinguish that case through
// Layout().VoiceCountSource().
func NewDiskFile(data []byte, disVoiceCount int) (*Document, error) {
	if disVoiceCount < 1 || disVoiceCount > disk.MaxVoices {
		return nil, fmt.Errorf("fzf: DIS voice count %d outside 1..%d", disVoiceCount, disk.MaxVoices)
	}
	owned := bytes.Clone(data)
	layout, err := fzutil.ResolveDiskFZFLayout(owned, disVoiceCount)
	if err != nil {
		return nil, err
	}
	return newDocument(owned, layout), nil
}

func newDocument(data []byte, layout fzutil.FZFLayout) *Document {
	return &Document{data: data, layout: layout}
}

// Bytes returns a copy of the original dump bytes.
func (d *Document) Bytes() []byte {
	return bytes.Clone(d.data)
}

// Layout returns the document's immutable resolved layout.
func (d *Document) Layout() fzutil.FZFLayout {
	return d.layout
}
