package webcore

import (
	"encoding/binary"
	"errors"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
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
func (s *Session) patchSlotVoice(slot int, build func(hdr []byte) ([]voiceedit.Patch, error)) (Snapshot, *Error) {
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
		if err := voiceedit.ApplyToFZFSlotBytes(d.fzf, slot, patches); err != nil {
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
	field, ok := schemaField(fieldID)
	if !ok || field.Kind == kindSelect {
		return s.Snapshot(), errf("invalid-field", "%q is not a numeric schema field", fieldID)
	}
	value = clampInt(value, field.Min, field.Max)
	return s.patchSlotVoice(slot, func(hdr []byte) ([]voiceedit.Patch, error) {
		return numberPatches(fieldID, value, hdr)
	})
}

// SetSlotParamOption sets a select schema field on an instrument
// voice slot.
func (s *Session) SetSlotParamOption(slot int, fieldID, option string) (Snapshot, *Error) {
	field, ok := schemaField(fieldID)
	if !ok || field.Kind != kindSelect {
		return s.Snapshot(), errf("invalid-field", "%q is not a select schema field", fieldID)
	}
	return s.patchSlotVoice(slot, func(hdr []byte) ([]voiceedit.Patch, error) {
		return optionPatches(fieldID, option, hdr)
	})
}

// SetSlotGeneration sets an instrument voice slot's generation window
// (R14's generation start and end) in voice-relative frames, clamped to
// the slot's own frame count. The cells hold absolute sample addresses
// into the shared audio area, so the write rebases exactly as
// SetSlotLoop does; without the rebase a window written as frames would
// address another voice's samples.
func (s *Session) SetSlotGeneration(slot, startFrame, endFrame int) (Snapshot, *Error) {
	return s.patchSlotVoice(slot, func(hdr []byte) ([]voiceedit.Patch, error) {
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
	return s.patchSlotVoice(slot, func(hdr []byte) ([]voiceedit.Patch, error) {
		base, waveEnd := slotWaveBounds(hdr)
		frames := int(waveEnd - base)
		if frames < 2 {
			return nil, errf(codeInvalidValue, "voice slot holds no loopable audio")
		}
		start := clampInt(startFrame, 0, frames-1)
		end := clampInt(endFrame, start+1, frames)
		stOff := disk.VoiceLoopSt0Offset + index*4
		edOff := disk.VoiceLoopEd0Offset + index*4
		origSt := binary.LittleEndian.Uint32(hdr[stOff : stOff+4])
		origEd := binary.LittleEndian.Uint32(hdr[edOff : edOff+4])
		// #nosec G115 -- start and end are clamped non-negative above.
		return voiceedit.BuildLoopPatch(index, base+uint32(start), base+uint32(end), origSt, origEd)
	})
}

// SetSlotLoopAttr sets loop index's cross-fade and multi-loop time on
// an instrument voice slot, clamped to the format's ranges.
func (s *Session) SetSlotLoopAttr(slot, index, xf, tm int) (Snapshot, *Error) {
	xf = clampInt(xf, 0, disk.MaxLoopXF)
	tm = clampInt(tm, 0, disk.MaxLoopTm)
	return s.patchSlotVoice(slot, func([]byte) ([]voiceedit.Patch, error) {
		return voiceedit.BuildLoopAttrPatch(index, xf, tm)
	})
}

// SetSlotLoopSelect sets the sustain and release loop designations on
// an instrument voice slot, clamped to 0..8 where 8 means none.
func (s *Session) SetSlotLoopSelect(slot, sustain, release int) (Snapshot, *Error) {
	sustain = clampInt(sustain, 0, disk.NoSustainLoop)
	release = clampInt(release, 0, disk.NoSustainLoop)
	return s.patchSlotVoice(slot, func([]byte) ([]voiceedit.Patch, error) {
		return voiceedit.BuildLoopSelectPatch(sustain, release)
	})
}

// SetSlotEnvelope sets a whole envelope on an instrument voice slot:
// the same display-scale contract as the loose-file setter (R16).
func (s *Session) SetSlotEnvelope(slot int, which string, sustain, end int, rates, stops []int) (Snapshot, *Error) {
	if which != envDCA && which != envDCF {
		return s.Snapshot(), errf("invalid-field", "envelope must be dca or dcf, got %q", which)
	}
	if len(rates) != disk.EnvelopeStages || len(stops) != disk.EnvelopeStages {
		return s.Snapshot(), errf(codeInvalidValue, "envelopes carry %d stages", disk.EnvelopeStages)
	}
	sustain = clampInt(sustain, 0, disk.EnvelopeStages-1)
	end = clampInt(end, 0, disk.EnvelopeStages-1)
	var r, st [disk.EnvelopeStages]int
	for i, v := range rates {
		r[i] = clampInt(v, 0, 99)
	}
	for i, v := range stops {
		st[i] = clampInt(v, 0, 99)
	}
	return s.patchSlotVoice(slot, func(hdr []byte) ([]voiceedit.Patch, error) {
		vp, err := slotParams(hdr)
		if err != nil {
			return nil, err
		}
		if which == envDCA {
			return voiceedit.BuildDCAPatches(sustain, end, r, st, vp.DCARates)
		}
		return voiceedit.BuildDCFPatches(sustain, end, r, st, vp.DCFRates)
	})
}

// RenameVoiceSlot sets a slot's 12-character printable ASCII name.
func (s *Session) RenameVoiceSlot(slot int, name string) (Snapshot, *Error) {
	if len(name) == 0 || len(name) > disk.LabelSize {
		return s.Snapshot(), errf(codeInvalidValue, "voice name must be 1 to %d characters", disk.LabelSize)
	}
	for _, r := range name {
		if r < disk.PrintableASCIIMin || r > disk.PrintableASCIIMax {
			return s.Snapshot(), errf(codeInvalidValue, "voice name contains non-ASCII character %q", string(r))
		}
	}
	return s.patchSlotVoice(slot, func([]byte) ([]voiceedit.Patch, error) {
		return voiceedit.BuildNamePatch(name)
	})
}
