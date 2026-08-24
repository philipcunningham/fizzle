package webcore

import (
	"encoding/binary"
	"errors"

	"github.com/philipcunningham/fizzle/pkg/disk"
	fzfmodel "github.com/philipcunningham/fizzle/pkg/fzf"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/model"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
)

// The slot-addressed voice editing family: the same edits the
// file-addressed setters make on loose .fzv files, applied to a voice
// slot inside the document's full dump. This is the surface the voice
// editor screen drives; a dump's voices have no directory file of
// their own. Loop and generation pointers inside a dump are absolute
// sample addresses into the shared audio area, so the ops here rebase
// between voice-relative frames (the boundary's unit) and absolute
// cells (the format's), while the loose-file ops need no rebase
// because a standalone voice starts at zero.

// patchSlotVoice runs a header-level edit on one voice slot inside the
// document's dump: build receives the slot's header slice (for reads
// of current values) and returns voiceedit patches, which apply
// through the same slot patcher the CLI's fzf edit uses, bank
// key-range fan-out included.
func (s *Session) patchSlotVoice(slot int, build func(hdr []byte) ([]voiceedit.Edit, error)) (Snapshot, *Error) {
	return s.patchDump(func(d *dumpState) ([]model.Patch, *Error) {
		hdr, cerr := slotHeader(d, slot)
		if cerr != nil {
			return nil, cerr
		}
		patches, err := build(hdr)
		if err != nil {
			var known *Error
			if errors.As(err, &known) {
				return nil, known
			}
			return nil, errf(codeInvalidValue, "%v", err)
		}
		if err := voiceedit.ApplyToFZFSlotBytesWithHeader(d.fzf, d.header, slot, patches); err != nil {
			return nil, errf(codeInvalidValue, "%v", err)
		}
		return nil, nil
	})
}

// slotHeader bounds-checks slot and returns its header slice.
func slotHeader(d *dumpState, slot int) ([]byte, *Error) {
	if slot < 0 || slot >= d.header.NVoice {
		return nil, errf(codeInvalidValue, "voice slot %d out of range", slot)
	}
	off := disk.VoiceSlotOffset(d.header.VoiceAreaStart, slot)
	if off+disk.VoiceHeaderUsed > len(d.fzf) {
		return nil, errf("invalid-image", "voice slot %d header extends past the dump", slot)
	}
	return d.fzf[off : off+disk.VoiceHeaderUsed], nil
}

// slotParams parses a slot header the way the loose-file path parses a
// voice file; the parser wants a whole sector, so the header is padded.
func slotParams(hdr []byte) (*fzvinfo.VoiceParams, error) {
	padded := make([]byte, disk.SectorSize)
	copy(padded, hdr)
	return fzvinfo.ParseBytes(padded)
}

// slotWaveBounds reads the slot's absolute wave start and end sample
// addresses; their difference is the voice's frame count.
func slotWaveBounds(hdr []byte) (start, end uint32) {
	start = binary.LittleEndian.Uint32(hdr[disk.VoiceWaveStartOffset : disk.VoiceWaveStartOffset+4])
	end = binary.LittleEndian.Uint32(hdr[disk.VoiceWaveEndOffset : disk.VoiceWaveEndOffset+4])
	if end < start {
		end = start
	}
	return start, end
}

// SetSlotParamNumber sets a numeric schema field on an instrument
// voice slot, clamping to the field's declared range (R14).
func (s *Session) SetSlotParamNumber(slot int, fieldID string, value int) (Snapshot, *Error) {
	value, cerr := clampNumberField(fieldID, value)
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	return s.patchSlotVoice(slot, func(hdr []byte) ([]voiceedit.Edit, error) {
		return numberPatches(fieldID, value, hdr)
	})
}

// SetSlotParamOption sets a select schema field on an instrument
// voice slot.
func (s *Session) SetSlotParamOption(slot int, fieldID, option string) (Snapshot, *Error) {
	if cerr := checkSelectField(fieldID); cerr != nil {
		return s.Snapshot(), cerr
	}
	return s.patchSlotVoice(slot, func(hdr []byte) ([]voiceedit.Edit, error) {
		return optionPatches(fieldID, option, hdr)
	})
}

// SetSlotGeneration sets an instrument voice slot's generation window
// (R14's generation start and end) in voice-relative frames, clamped to
// the slot's own frame count. The cells hold absolute addresses, so the
// write rebases; without that a window written as frames would address
// another voice's samples.
func (s *Session) SetSlotGeneration(slot, startFrame, endFrame int) (Snapshot, *Error) {
	return s.patchSlotVoice(slot, func(hdr []byte) ([]voiceedit.Edit, error) {
		base, waveEnd := slotWaveBounds(hdr)
		start, end := clampGeneration(int(waveEnd-base), startFrame, endFrame)
		return generationPatches(base, start, end), nil
	})
}

// SetSlotLoop sets loop index's start and end on an instrument voice
// slot, in voice-relative frames clamped to the slot's frame count;
// the cells store absolute addresses, so the write rebases (R17).
func (s *Session) SetSlotLoop(slot, index, startFrame, endFrame int) (Snapshot, *Error) {
	if index < 0 || index >= disk.MaxGenerators {
		return s.Snapshot(), errf(codeInvalidValue, "loop index must be 0 to %d, got %d", disk.MaxGenerators-1, index)
	}
	return s.patchSlotVoice(slot, func(hdr []byte) ([]voiceedit.Edit, error) {
		base, waveEnd := slotWaveBounds(hdr)
		return buildLoopPatch(hdr, index, startFrame, endFrame, int(waveEnd-base), base)
	})
}

// SetSlotLoopAttr sets loop index's cross-fade and multi-loop time on
// an instrument voice slot, clamped to the format's ranges.
func (s *Session) SetSlotLoopAttr(slot, index, xf, tm int) (Snapshot, *Error) {
	xf = clampInt(xf, 0, disk.MaxLoopXF)
	tm = clampInt(tm, 0, disk.MaxLoopTm)
	return s.patchSlotVoice(slot, func([]byte) ([]voiceedit.Edit, error) {
		return voiceedit.BuildLoopAttrPatch(index, xf, tm)
	})
}

// SetSlotLoopSelect sets the sustain and release loop designations on
// an instrument voice slot, clamped to 0..8 where 8 means none.
func (s *Session) SetSlotLoopSelect(slot, sustain, release int) (Snapshot, *Error) {
	return s.patchSlotVoice(slot, loopSelectBuilder(sustain, release))
}

// SetSlotEnvelope sets a whole envelope on an instrument voice slot:
// the same display-scale contract as the loose-file setter (R16).
func (s *Session) SetSlotEnvelope(slot int, which string, sustain, end int, rates, stops []int) (Snapshot, *Error) {
	build, cerr := buildEnvelopePatches(which, sustain, end, rates, stops)
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	return s.patchSlotVoice(slot, func(hdr []byte) ([]voiceedit.Edit, error) {
		vp, err := slotParams(hdr)
		if err != nil {
			return nil, err
		}
		return build(vp)
	})
}

// RenameVoiceSlot sets a slot's 12-character printable ASCII name.
func (s *Session) RenameVoiceSlot(slot int, name string) (Snapshot, *Error) {
	return s.patchDump(func(d *dumpState) ([]model.Patch, *Error) {
		var (
			doc *fzfmodel.Document
			err error
		)
		if d.disVN > 0 {
			doc, err = fzfmodel.NewDiskFile(d.fzf, d.disVN)
		} else {
			doc, err = fzfmodel.NewStandalone(d.fzf)
		}
		if err != nil {
			return nil, errf("invalid-image", "%v", err)
		}
		patches, err := doc.RenameVoice(slot, name)
		if err != nil {
			switch {
			case errors.Is(err, fzfmodel.ErrVoiceNameEmpty), errors.Is(err, fzfmodel.ErrVoiceNameTooLong):
				return nil, errf(codeInvalidValue, "voice name must be 1 to %d characters", disk.LabelSize)
			case errors.Is(err, fzfmodel.ErrVoiceNameNotASCII):
				for _, r := range name {
					if r < disk.PrintableASCIIMin || r > disk.PrintableASCIIMax {
						return nil, errf(codeInvalidValue, "voice name contains non-ASCII character %q", string(r))
					}
				}
			case errors.Is(err, fzfmodel.ErrVoiceIndexOutOfRange):
				return nil, errf(codeInvalidValue, "voice slot %d out of range", slot)
			}
			return nil, errf(codeInvalidValue, "could not rename voice: %v", err)
		}
		return patches, nil
	})
}
