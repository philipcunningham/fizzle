// Package fzf provides the canonical byte-preserving model of an FZF dump.
package fzf

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/model"
)

// Voice rename errors let application boundaries provide stable user-facing
// messages without matching domain error text.
var (
	ErrVoiceIndexOutOfRange = errors.New("fzf: voice index out of range")
	ErrVoiceNameEmpty       = errors.New("fzf: voice name is empty")
	ErrVoiceNameTooLong     = errors.New("fzf: voice name is too long")
	ErrVoiceNameNotASCII    = errors.New("fzf: voice name is not printable ASCII")
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

// RenameVoice returns the stale-safe patches for changing a voice slot's name.
// Standalone marker authority is included in the same patch batch because the
// marker covers bytes in the bank and voice headers.
func (d *Document) RenameVoice(index int, name string) ([]model.Patch, error) {
	if name == "" {
		return nil, ErrVoiceNameEmpty
	}
	if len(name) > disk.LabelSize {
		return nil, fmt.Errorf("%w: got %d bytes", ErrVoiceNameTooLong, len(name))
	}
	for _, r := range name {
		if r < disk.PrintableASCIIMin || r > disk.PrintableASCIIMax {
			return nil, fmt.Errorf("%w: %q", ErrVoiceNameNotASCII, r)
		}
	}
	if index < 0 || index >= d.layout.VoiceCount() {
		return nil, fmt.Errorf("%w: %d", ErrVoiceIndexOutOfRange, index)
	}
	voice, err := d.Voice(index)
	if err != nil {
		return nil, err
	}
	patch, err := voice.NamePatch(name)
	if err != nil {
		return nil, err
	}
	patches := []model.Patch{patch}
	if d.layout.VoiceCountSource() == fzutil.VoiceCountMarker {
		updated := bytes.Clone(d.data)
		if err := model.Apply(updated, patches); err != nil {
			return nil, fmt.Errorf("fzf: preparing voice %d rename: %w", index, err)
		}
		fzutil.StampVoiceCountMarker(updated, d.layout.VoiceCount())
		markerOffset := disk.BankVoiceMarkerOffset
		patches = append(patches, model.Patch{
			Offset: markerOffset,
			Old:    bytes.Clone(d.data[markerOffset : markerOffset+disk.BankVoiceMarkerSize]),
			New:    bytes.Clone(updated[markerOffset : markerOffset+disk.BankVoiceMarkerSize]),
		})
	}
	return patches, nil
}
