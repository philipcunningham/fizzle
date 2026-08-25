// Package fzf provides the canonical byte-preserving model of an FZF dump.
package fzf

import (
	"bytes"
	"encoding/binary"
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
	ErrBankFull             = errors.New("fzf: bank is full")
	ErrLastArea             = errors.New("fzf: bank must retain one area")
	ErrAreaVoiceOutOfRange  = errors.New("fzf: area voice is out of range")
	ErrVoiceHeaderBounds    = errors.New("fzf: voice header is out of bounds")
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

// AreaVoiceError identifies a bank area whose voice pointer is outside the
// resolved document voice count.
type AreaVoiceError struct {
	Area, Voice, VoiceCount int
}

func (e *AreaVoiceError) Error() string {
	return fmt.Sprintf("%v: area %d points to voice %d of %d", ErrAreaVoiceOutOfRange, e.Area, e.Voice, e.VoiceCount)
}

func (e *AreaVoiceError) Unwrap() error { return ErrAreaVoiceOutOfRange }

// Document owns an FZF dump and the layout resolved for its source context.
// Construction copies the input, and Bytes returns a fresh copy, so callers
// cannot change the bytes independently of the retained layout.
type Document struct {
	data     []byte
	layout   fzutil.FZFLayout
	diskMode bool
}

// NewStandalone constructs a document using standalone marker and walk policy.
func NewStandalone(data []byte) (*Document, error) {
	owned := bytes.Clone(data)
	layout, err := fzutil.ResolveStandaloneFZFLayout(owned)
	if err != nil {
		return nil, err
	}
	return newDocument(owned, layout, false), nil
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
	return newDocument(owned, layout, layout.VoiceCount() == disVoiceCount), nil
}

func newDocument(data []byte, layout fzutil.FZFLayout, diskMode bool) *Document {
	return &Document{data: data, layout: layout, diskMode: diskMode}
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

// AddArea returns a structural operation that appends an area playing an
// existing voice slot and keeps the voice and audio boundary readable.
func (d *Document) AddArea(bankIndex, voiceIndex int) (OperationResult, error) {
	bank, err := d.Bank(bankIndex)
	if err != nil {
		return OperationResult{}, &IndexError{Err: ErrBankIndexOutOfRange, Index: bankIndex, Limit: d.layout.BankCount()}
	}
	voice, err := d.Voice(voiceIndex)
	if err != nil {
		return OperationResult{}, &IndexError{Err: ErrVoiceIndexOutOfRange, Index: voiceIndex, Limit: d.layout.VoiceCount()}
	}
	area := bank.AreaCount()
	if area >= disk.MaxVoices {
		return OperationResult{}, fmt.Errorf("%w: bank %d", ErrBankFull, bankIndex)
	}

	working := bytes.Clone(d.data)
	resized, err := container.ResizeVoiceAreaOwned(working, d.resizeParams(1, -1))
	if err != nil {
		return OperationResult{}, err
	}
	base := bankIndex * disk.SectorSize
	voicePointer := make([]byte, disk.VPEntrySize)
	binary.LittleEndian.PutUint16(voicePointer, uint16(voiceIndex)) //nolint:gosec // bounded voice index
	patches := []model.Patch{{
		Offset: base + disk.BankVoiceNumOffset + area*disk.VPEntrySize,
		Old:    bytes.Clone(resized.Data[base+disk.BankVoiceNumOffset+area*disk.VPEntrySize : base+disk.BankVoiceNumOffset+(area+1)*disk.VPEntrySize]),
		New:    voicePointer,
	}}
	patches = append(patches, container.DefaultBankRangePatches(resized.Data, bankIndex, area)...)
	root := voice.RootKey()
	if root == 0 || root > disk.MaxMIDINote {
		root = 60
	}
	patches = append(patches,
		oneBytePatch(resized.Data, base+disk.BankKeyCentOffset+area, root),
		oneBytePatch(resized.Data, base+disk.BankAudioOutOffset+area, disk.PolyphonicAudioOut),
	)
	if bump, ok := container.BankBstepBumpPatch(resized.Data, bankIndex, area); ok {
		patches = append(patches, bump)
	}
	if err := model.Apply(resized.Data, patches); err != nil {
		return OperationResult{}, fmt.Errorf("fzf: adding area: %w", err)
	}
	d.stampMarker(resized.Data, resized.VoiceCount)
	return structuralAreaOperation(d.data, resized.Data, resized.VoiceCount, resized.AudioStart), nil
}

// DeleteArea returns a structural operation that removes one area and gives
// its unreferenced voice slot back where the resolved walk requires it.
func (d *Document) DeleteArea(bankIndex, areaIndex int) (OperationResult, error) {
	bank, err := d.Bank(bankIndex)
	if err != nil {
		return OperationResult{}, &IndexError{Err: ErrBankIndexOutOfRange, Index: bankIndex, Limit: d.layout.BankCount()}
	}
	if areaIndex < 0 || areaIndex >= bank.AreaCount() {
		return OperationResult{}, &IndexError{Err: ErrAreaIndexOutOfRange, Index: areaIndex, Limit: bank.AreaCount()}
	}
	if bank.AreaCount() <= 1 {
		return OperationResult{}, fmt.Errorf("%w: bank %d area %d", ErrLastArea, bankIndex, areaIndex)
	}
	freed, err := bank.VoiceSlot(areaIndex)
	if err != nil {
		return OperationResult{}, err
	}
	working := bytes.Clone(d.data)
	patches := container.DeleteAreaPatches(working, container.DeleteAreaParams{
		Base: bankIndex * disk.SectorSize, AreaIdx: areaIndex, Bstep: bank.AreaCount(),
	})
	if err := model.Apply(working, patches); err != nil {
		return OperationResult{}, fmt.Errorf("fzf: deleting area: %w", err)
	}
	resized, err := container.ResizeVoiceAreaOwned(working, d.resizeParams(0, freed))
	if err != nil {
		return OperationResult{}, err
	}
	d.stampMarker(resized.Data, resized.VoiceCount)
	return structuralAreaOperation(d.data, resized.Data, resized.VoiceCount, resized.AudioStart), nil
}

// DuplicateArea returns a structural operation that appends a copy of one
// area and its voice header. The new slot shares the source voice's audio.
func (d *Document) DuplicateArea(bankIndex, areaIndex int) (OperationResult, error) {
	bank, err := d.Bank(bankIndex)
	if err != nil {
		return OperationResult{}, &IndexError{Err: ErrBankIndexOutOfRange, Index: bankIndex, Limit: d.layout.BankCount()}
	}
	if areaIndex < 0 || areaIndex >= bank.AreaCount() {
		return OperationResult{}, &IndexError{Err: ErrAreaIndexOutOfRange, Index: areaIndex, Limit: bank.AreaCount()}
	}
	if bank.AreaCount() >= disk.MaxVoices {
		return OperationResult{}, fmt.Errorf("%w: bank %d", ErrBankFull, bankIndex)
	}
	sourceSlot, err := bank.VoiceSlot(areaIndex)
	if err != nil {
		base := bankIndex*disk.SectorSize + disk.BankVoiceNumOffset + areaIndex*disk.VPEntrySize
		voice := int(binary.LittleEndian.Uint16(d.data[base : base+disk.VPEntrySize]))
		return OperationResult{}, &AreaVoiceError{Area: areaIndex, Voice: voice, VoiceCount: d.layout.VoiceCount()}
	}
	sourceOffset := disk.VoiceSlotOffset(d.layout.VoiceStart(), sourceSlot)
	if sourceOffset+disk.VoicePackSize > len(d.data) {
		return OperationResult{}, fmt.Errorf("%w: slot %d", ErrVoiceHeaderBounds, sourceSlot)
	}
	sourceHeader := bytes.Clone(d.data[sourceOffset : sourceOffset+disk.VoicePackSize])
	newSlot := d.layout.VoiceCount()
	working := bytes.Clone(d.data)
	resized, err := container.ResizeVoiceAreaToOwned(working, d.resizeParams(0, -1), newSlot+1)
	if err != nil {
		return OperationResult{}, err
	}
	patches := container.DuplicateAreaPatches(resized.Data, container.DuplicateAreaParams{
		Base:       bankIndex * disk.SectorSize,
		NewOff:     disk.VoiceSlotOffset(d.layout.VoiceStart(), newSlot),
		SrcAreaIdx: areaIndex,
		Bstep:      bank.AreaCount(),
		NewSlot:    newSlot,
		SrcHeader:  sourceHeader,
	})
	if err := model.Apply(resized.Data, patches); err != nil {
		return OperationResult{}, fmt.Errorf("fzf: duplicating area: %w", err)
	}
	d.stampMarker(resized.Data, resized.VoiceCount)
	return structuralAreaOperation(d.data, resized.Data, resized.VoiceCount, resized.AudioStart), nil
}

func (d *Document) resizeParams(delta, freed int) container.VoiceAreaResizeParams {
	bound := 0
	for bank := 0; bank < d.layout.BankCount(); bank++ {
		view, _ := d.Bank(bank)
		bound += view.AreaCount()
	}
	return container.VoiceAreaResizeParams{
		BankCount: d.layout.BankCount(), VoiceCount: d.layout.VoiceCount(),
		VoiceStart: d.layout.VoiceStart(), AudioStart: d.layout.AudioStart(),
		WalkBound: min(bound, disk.MaxVoices), BStepDelta: delta,
		FreedSlot: freed, DiskMode: d.diskMode,
	}
}

func (d *Document) stampMarker(data []byte, voiceCount int) {
	if d.layout.VoiceCountSource() == fzutil.VoiceCountMarker {
		fzutil.StampVoiceCountMarker(data, voiceCount)
	}
}

func oneBytePatch(data []byte, offset int, value byte) model.Patch {
	return model.Patch{Offset: offset, Old: []byte{data[offset]}, New: []byte{value}}
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
