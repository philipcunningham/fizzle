package fzf

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/model"
)

// AreaField identifies one fixed-size field in a bank area. The boundary maps
// its public string contract to this type; the document owns storage widths,
// display conversions, and format limits.
type AreaField uint8

const (
	// AreaKeyLow edits the area's lowest key.
	AreaKeyLow AreaField = iota
	// AreaKeyHigh edits the area's highest key.
	AreaKeyHigh
	// AreaRootKey edits the area's root key.
	AreaRootKey
	// AreaVelocityLow edits the area's minimum velocity.
	AreaVelocityLow
	// AreaVelocityHigh edits the area's maximum velocity.
	AreaVelocityHigh
	// AreaVolume edits the area's display-scale level.
	AreaVolume
	// AreaMIDIChannel edits the area's display-scale MIDI channel.
	AreaMIDIChannel
	// AreaOutput edits the area's audio-output byte.
	AreaOutput
	// AreaVoiceSlot edits the area's two-byte voice pointer.
	AreaVoiceSlot
)

// SetAreaField returns an atomic fixed-size operation for one area value.
// Display-scale values are clamped and converted before the stale-safe patch
// is built; standalone marker authority is restamped in the same operation.
func (d *Document) SetAreaField(bankIndex, areaIndex int, field AreaField, value int) (OperationResult, error) {
	bank, err := d.Bank(bankIndex)
	if err != nil {
		return OperationResult{}, &IndexError{Err: ErrBankIndexOutOfRange, Index: bankIndex, Limit: d.layout.BankCount()}
	}
	if areaIndex < 0 || areaIndex >= bank.AreaCount() {
		return OperationResult{}, &IndexError{Err: ErrAreaIndexOutOfRange, Index: areaIndex, Limit: bank.AreaCount()}
	}
	base := bankIndex * disk.SectorSize
	var patch model.Patch
	switch field {
	case AreaKeyLow:
		patch = areaBytePatch(d.data, base+disk.BankKeyLowOffset+areaIndex, clampArea(value, 0, 127))
	case AreaKeyHigh:
		patch = areaBytePatch(d.data, base+disk.BankKeyHighOffset+areaIndex, clampArea(value, 0, 127))
	case AreaRootKey:
		patch = areaBytePatch(d.data, base+disk.BankKeyCentOffset+areaIndex, clampArea(value, 0, 127))
	case AreaVelocityLow:
		patch = areaBytePatch(d.data, base+disk.BankVelLowOffset+areaIndex, clampArea(value, disk.MinVelocity, 127))
	case AreaVelocityHigh:
		patch = areaBytePatch(d.data, base+disk.BankVelHighOffset+areaIndex, clampArea(value, disk.MinVelocity, 127))
	case AreaVolume:
		patch = areaBytePatch(d.data, base+disk.BankVolumeOffset+areaIndex, disk.AreaLevelToByte(value))
	case AreaMIDIChannel:
		patch = areaBytePatch(d.data, base+disk.BankMIDIRecvChanOffset+areaIndex, clampArea(value-1, 0, 15))
	case AreaOutput:
		patch = areaBytePatch(d.data, base+disk.BankAudioOutOffset+areaIndex, clampArea(value, 0, 255))
	case AreaVoiceSlot:
		if value < 0 || value >= d.layout.VoiceCount() {
			return OperationResult{}, &IndexError{Err: ErrVoiceIndexOutOfRange, Index: value, Limit: d.layout.VoiceCount()}
		}
		offset := base + disk.BankVoiceNumOffset + areaIndex*disk.VPEntrySize
		old := bytes.Clone(d.data[offset : offset+disk.VPEntrySize])
		updated := make([]byte, disk.VPEntrySize)
		binary.LittleEndian.PutUint16(updated, uint16(value)) //nolint:gosec // value is bounded by the format voice count
		patch = model.Patch{Offset: offset, Old: old, New: updated}
	default:
		return OperationResult{}, fmt.Errorf("%w: %d", ErrAreaFieldInvalid, field)
	}
	patches, err := d.withMarkerPatches([]model.Patch{patch})
	if err != nil {
		return OperationResult{}, err
	}
	return fixedOperation(patches), nil
}

func areaBytePatch(data []byte, offset int, value byte) model.Patch {
	return model.Patch{Offset: offset, Old: []byte{data[offset]}, New: []byte{value}}
}

func clampArea(value, low, high int) byte {
	return byte(min(max(value, low), high)) //nolint:gosec // callers provide byte-bounded limits
}
