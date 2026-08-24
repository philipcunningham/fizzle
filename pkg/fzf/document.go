// Package fzf provides the canonical byte-preserving model of an FZF dump.
package fzf

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/container"
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
	ErrBankIndexOutOfRange  = errors.New("fzf: bank index out of range")
	ErrBankNameEmpty        = errors.New("fzf: bank name is empty")
	ErrBankNameTooLong      = errors.New("fzf: bank name is too long")
	ErrBankNameNotASCII     = errors.New("fzf: bank name is not printable ASCII")
	ErrAreaIndexOutOfRange  = errors.New("fzf: area index out of range")
)

// NameError identifies the invalid character in a printable-ASCII name.
// Err is one of the exported name sentinels.
type NameError struct {
	Err       error
	Character rune
}

func (e *NameError) Error() string {
	return fmt.Sprintf("%v: %q", e.Err, e.Character)
}

// Unwrap supports errors.Is against the exported name sentinel.
func (e *NameError) Unwrap() error { return e.Err }

// IndexError identifies an out-of-range document index and its exclusive
// upper bound. Err identifies the indexed domain object.
type IndexError struct {
	Err   error
	Index int
	Limit int
}

func (e *IndexError) Error() string {
	return fmt.Sprintf("%v: index %d outside 0..%d", e.Err, e.Index, e.Limit-1)
}

// Unwrap supports errors.Is against the exported index sentinel.
func (e *IndexError) Unwrap() error { return e.Err }

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

// RenameVoice returns an atomic fixed-size operation for changing a voice
// slot's name. Standalone marker authority is part of the same result because
// the marker covers bytes in the bank and voice headers.
func (d *Document) RenameVoice(index int, name string) (OperationResult, error) {
	if err := validateName(name, ErrVoiceNameEmpty, ErrVoiceNameTooLong, ErrVoiceNameNotASCII); err != nil {
		return OperationResult{}, err
	}
	if index < 0 || index >= d.layout.VoiceCount() {
		return OperationResult{}, &IndexError{Err: ErrVoiceIndexOutOfRange, Index: index, Limit: d.layout.VoiceCount()}
	}
	voice, err := d.Voice(index)
	if err != nil {
		return OperationResult{}, err
	}
	patch, err := voice.NamePatch(name)
	if err != nil {
		return OperationResult{}, err
	}
	patches, err := d.withMarkerPatches([]model.Patch{patch})
	if err != nil {
		return OperationResult{}, err
	}
	return fixedOperation(patches), nil
}

// RenameBank returns an atomic fixed-size operation for changing a bank's
// name. A marker re-stamp, where required, is part of the same result.
func (d *Document) RenameBank(index int, name string) (OperationResult, error) {
	if err := validateName(name, ErrBankNameEmpty, ErrBankNameTooLong, ErrBankNameNotASCII); err != nil {
		return OperationResult{}, err
	}
	if index < 0 || index >= d.layout.BankCount() {
		return OperationResult{}, &IndexError{Err: ErrBankIndexOutOfRange, Index: index, Limit: d.layout.BankCount()}
	}
	bank, err := d.Bank(index)
	if err != nil {
		return OperationResult{}, err
	}
	patch, err := bank.NamePatch(name)
	if err != nil {
		return OperationResult{}, err
	}
	patches, err := d.withMarkerPatches([]model.Patch{patch})
	if err != nil {
		return OperationResult{}, err
	}
	return fixedOperation(patches), nil
}

// SwapAreas returns an atomic fixed-size operation that exchanges two areas
// and all their parallel bank fields.
func (d *Document) SwapAreas(bankIndex, first, second int) (OperationResult, error) {
	bank, err := d.Bank(bankIndex)
	if err != nil {
		return OperationResult{}, &IndexError{Err: ErrBankIndexOutOfRange, Index: bankIndex, Limit: d.layout.BankCount()}
	}
	for _, area := range []int{first, second} {
		if area < 0 || area >= bank.AreaCount() {
			return OperationResult{}, &IndexError{Err: ErrAreaIndexOutOfRange, Index: area, Limit: bank.AreaCount()}
		}
	}
	if first == second {
		return fixedOperation(nil), nil
	}
	patches := container.SwapAreaPatches(d.data, container.SwapAreaParams{
		Base: bankIndex * disk.SectorSize, SrcArea: first, TgtArea: second,
	})
	patches, err = d.withMarkerPatches(patches)
	if err != nil {
		return OperationResult{}, err
	}
	return fixedOperation(patches), nil
}

func validateName(name string, empty, tooLong, notASCII error) error {
	if name == "" {
		return empty
	}
	if len(name) > disk.LabelSize {
		return fmt.Errorf("%w: got %d bytes", tooLong, len(name))
	}
	for _, r := range name {
		if r < disk.PrintableASCIIMin || r > disk.PrintableASCIIMax {
			return &NameError{Err: notASCII, Character: r}
		}
	}
	return nil
}

func (d *Document) withMarkerPatches(patches []model.Patch) ([]model.Patch, error) {
	if d.layout.VoiceCountSource() == fzutil.VoiceCountMarker {
		updated := bytes.Clone(d.data)
		if err := model.Apply(updated, patches); err != nil {
			return nil, fmt.Errorf("fzf: preparing marker update: %w", err)
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
