// Package fzf provides the canonical byte-preserving model of an FZF dump.
package fzf

import (
	"bytes"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/model"
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

// RenameVoice returns a new document with the voice slot's name changed. The
// receiver remains unchanged. Standalone marker authority is re-stamped after
// the edit because the marker covers bytes in the bank and voice headers.
func (d *Document) RenameVoice(index int, name string) (*Document, error) {
	if name == "" {
		return nil, fmt.Errorf("fzf: voice name must not be empty")
	}
	voice, err := d.Voice(index)
	if err != nil {
		return nil, err
	}
	patch, err := voice.NamePatch(name)
	if err != nil {
		return nil, err
	}
	updated := bytes.Clone(d.data)
	if err := model.Apply(updated, []model.Patch{patch}); err != nil {
		return nil, fmt.Errorf("fzf: renaming voice %d: %w", index, err)
	}
	if d.layout.VoiceCountSource() == fzutil.VoiceCountMarker {
		fzutil.StampVoiceCountMarker(updated, d.layout.VoiceCount())
	}
	return newDocument(updated, d.layout), nil
}
