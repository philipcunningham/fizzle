// Package voicepatch resolves voice-header edits into stale-safe byte patches.
package voicepatch

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/model"
)

// ErrUnsupportedSize reports an edit whose integer width is not supported.
var ErrUnsupportedSize = errors.New("voicepatch: unsupported edit size")

// Edit describes a modification relative to the start of a voice header.
type Edit struct {
	Offset int
	Size   int
	Value  uint16
	Bytes  []byte
}

// ResolveHeader resolves edits against the voice header at base.
func ResolveHeader(data []byte, base int, edits []Edit) ([]model.Patch, error) {
	if base < 0 || base+disk.VoiceHeaderUsed > len(data) {
		return nil, fmt.Errorf("voice header at %d extends beyond data", base)
	}
	original := data[base : base+disk.VoiceHeaderUsed]
	updated := bytes.Clone(original)
	touched := make([]bool, len(updated))
	for _, edit := range edits {
		size := edit.Size
		if edit.Bytes != nil {
			size = len(edit.Bytes)
		}
		if edit.Offset < 0 || edit.Offset+size > disk.VoiceHeaderUsed {
			return nil, fmt.Errorf("patch offset %d (size %d) out of voice header range", edit.Offset, size)
		}
		if edit.Bytes != nil {
			copy(updated[edit.Offset:edit.Offset+size], edit.Bytes)
		} else {
			switch edit.Size {
			case 1:
				updated[edit.Offset] = byte(edit.Value) //nolint:gosec // builders validate values
			case 2:
				binary.LittleEndian.PutUint16(updated[edit.Offset:], edit.Value)
			default:
				return nil, fmt.Errorf("%w: %d", ErrUnsupportedSize, edit.Size)
			}
		}
		for i := edit.Offset; i < edit.Offset+size; i++ {
			touched[i] = true
		}
	}

	var patches []model.Patch
	for start := 0; start < len(touched); {
		for start < len(touched) && (!touched[start] || original[start] == updated[start]) {
			start++
		}
		if start == len(touched) {
			break
		}
		end := start + 1
		for end < len(touched) && touched[end] && original[end] != updated[end] {
			end++
		}
		patches = append(patches, model.Patch{
			Offset: base + start,
			Old:    bytes.Clone(original[start:end]),
			New:    bytes.Clone(updated[start:end]),
		})
		start = end
	}
	return patches, nil
}

// ResolveFZFSlot resolves edits for one slot and mirrors key-range changes to
// every bank area that references it, using the caller's retained layout.
func ResolveFZFSlot(data []byte, layout fzutil.FZFLayout, slot int, edits []Edit) ([]model.Patch, error) {
	if slot < 0 || slot >= layout.VoiceCount() {
		return nil, fmt.Errorf("voice slot must be 0 to %d, got %d", layout.VoiceCount()-1, slot)
	}
	voiceOffset := disk.VoiceSlotOffset(layout.VoiceStart(), slot)
	resolved, err := ResolveHeader(data, voiceOffset, edits)
	if err != nil {
		return nil, err
	}
	updatedHeader := bytes.Clone(data[voiceOffset : voiceOffset+disk.VoiceHeaderUsed])
	if err := model.Apply(updatedHeader, relativePatches(resolved, voiceOffset)); err != nil {
		return nil, err
	}
	header := &fzutil.FZFHeader{
		NVoice: layout.VoiceCount(), NBankSectors: layout.BankCount(), VoiceAreaStart: layout.VoiceStart(),
	}
	sites := fzutil.FindBankSitesForVoice(data, header, slot)
	seen := make(map[int]struct{})
	for _, edit := range edits {
		bankOffset, ok := bankOffsetFor(edit.Offset)
		if !ok {
			continue
		}
		for _, site := range sites {
			offset := site.BankIdx*disk.SectorSize + bankOffset + site.SplitIdx
			if offset >= len(data) {
				return nil, fmt.Errorf("bank site write at %d beyond data", offset)
			}
			if _, ok := seen[offset]; ok {
				continue
			}
			seen[offset] = struct{}{}
			resolved = append(resolved, model.Patch{
				Offset: offset, Old: []byte{data[offset]}, New: []byte{updatedHeader[edit.Offset]},
			})
		}
	}
	return resolved, nil
}

func bankOffsetFor(headerOffset int) (int, bool) {
	switch headerOffset {
	case disk.VoiceKeyHighOffset:
		return disk.BankKeyHighOffset, true
	case disk.VoiceKeyLowOffset:
		return disk.BankKeyLowOffset, true
	case disk.VoiceKeyCentOffset:
		return disk.BankKeyCentOffset, true
	default:
		return 0, false
	}
}

func relativePatches(patches []model.Patch, base int) []model.Patch {
	relative := make([]model.Patch, len(patches))
	for i, patch := range patches {
		relative[i] = patch
		relative[i].Offset -= base
	}
	return relative
}
