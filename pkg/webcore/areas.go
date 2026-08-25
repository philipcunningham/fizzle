package webcore

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/container"
	"github.com/philipcunningham/fizzle/pkg/disk"
	fzfmodel "github.com/philipcunningham/fizzle/pkg/fzf"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/model"
)

// Area field names, shared by the setter and the tests that drive it.
const (
	fieldAreaVelLow  = "velLow"
	fieldAreaVelHigh = "velHigh"
)

// defaultRootKey is middle C, the root a fresh area falls back to when
// the voice slot carries no usable one. The empty instrument's
// placeholder area is seeded with the same note.
const defaultRootKey = 60

// noFreedSlot marks a voice-area change with no slot of its own to give
// back first.
const noFreedSlot = -1

// codeVoiceWalk refuses a change that a reader would walk differently
// from the way the operation built it, which is the same thing as
// saying the audio would move.
const codeVoiceWalk = "voice-walk"

// codeSpareVoice refuses a change that would drop a voice no area
// plays. Such a voice is exactly what R13's Map button rescues, so it
// goes only when the user says so.
const codeSpareVoice = "spare-voice"

// dumpState carries the parsed context every area op needs.
type dumpState struct {
	fzf    []byte
	header *fzutil.FZFHeader
	doc    *fzfmodel.Document
	// audioStart is the byte the audio area physically begins at. Every
	// reader derives that same byte from the voice count it walks, so
	// the two have to agree once an operation finishes; checkGeometry
	// is where they are held to it.
	audioStart int
	// walkBound is the summed bstep values the dump arrived with, which
	// is the bound header.NVoice was walked under. Where the count
	// reached it, the bound is what stopped the walk and moving it moves
	// the count; where the count fell short, the walk ended on a byte
	// pattern that is not a voice slot and the bound stops nothing.
	walkBound int
	// disVN is the DIS tail's voice count where it is the authority,
	// 0 when header.NVoice was walked.
	disVN int
}

// newDumpState parses a full dump into the context the area ops work
// over. disVN is the document's DIS-mode count, 0 for walk mode.
func newDumpState(fzf []byte, disVN int) (*dumpState, *Error) {
	var doc *fzfmodel.Document
	var err error
	if disVN > 0 {
		doc, err = fzfmodel.NewDiskFile(fzf, disVN)
	} else {
		doc, err = fzfmodel.NewStandalone(fzf)
	}
	if err != nil {
		return nil, errf("invalid-image", "%v", err)
	}
	layout := doc.Layout()
	// Keep an already-recorded DIS mode when its count agrees with the
	// resolved document, even where the byte walk reaches the same count and
	// therefore needs no DIS authority to parse. History owns that mode.
	if disVN > 0 && layout.VoiceCount() != disVN {
		disVN = 0
	}
	hdr := &fzutil.FZFHeader{
		NVoice:         layout.VoiceCount(),
		BStep0:         layout.BStep0(),
		NBankSectors:   layout.BankCount(),
		VoiceAreaStart: layout.VoiceStart(),
	}
	return &dumpState{
		fzf:        fzf,
		header:     hdr,
		doc:        doc,
		audioStart: layout.AudioStart(),
		walkBound:  min(bstepSum(fzf, hdr.NBankSectors), disk.MaxVoices),
		disVN:      disVN,
	}, nil
}

// dumpHeaderFor is the compatibility parser used by callers that have not yet
// migrated to the resolved document context.
func dumpHeaderFor(fzf []byte, vn int) (*fzutil.FZFHeader, error) {
	if vn > 0 {
		return fzutil.ParseFZFHeaderWithVoiceCount(fzf, vn)
	}
	return fzutil.ParseFZFHeader(fzf)
}

// checkGeometry refuses an operation that leaves the audio at a byte
// a reader would not derive: walk mode re-derives the count (see
// bstepSum), DIS mode validates under the count the write-back
// stamps. A moved audio start plays every voice from the wrong bytes.
func (d *dumpState) checkGeometry() *Error {
	var (
		layout fzutil.FZFLayout
		err    error
	)
	if d.disVN > 0 {
		layout, err = fzutil.ResolveDiskFZFLayout(d.fzf, d.header.NVoice)
	} else {
		layout, err = fzutil.ResolveStandaloneFZFLayout(d.fzf)
	}
	if err != nil {
		return errf(codeVoiceWalk, "the change leaves a dump no reader can parse: %v", err)
	}
	read := layout.AudioStart()
	if read != d.audioStart {
		return errf(codeVoiceWalk,
			"the change moves the audio: a reader walks %d voices and looks for the audio at byte %d, where this dump holds it at %d",
			layout.VoiceCount(), read, d.audioStart)
	}
	return nil
}

func bankBstep(fzf []byte, bank int) int {
	base := bank * disk.SectorSize
	return int(binary.LittleEndian.Uint16(fzf[base+disk.BankVoiceCountOffset:]))
}

// bstepSum totals the areas the dump's banks hold. Nothing stores a
// dump's voice count: every fizzle reader walks the voice area with
// this sum as the bound and stops at the first byte pattern that is
// not a voice slot (fzutil.CountAllVoices). The count it yields sizes
// the voice area, and the voice area's end is where the audio starts,
// so an area operation is a voice-area operation: an area that arrives
// needs a slot to grow into, and one that goes gives its slot back.
// Bumping bstep on its own walks the reader into the audio, and every
// voice then plays from a sector too late.
func bstepSum(fzf []byte, nBanks int) int {
	total := 0
	for b := 0; b < nBanks; b++ {
		if b*disk.SectorSize+disk.BankVoiceCountOffset+2 > len(fzf) {
			break
		}
		total += bankBstep(fzf, b)
	}
	return total
}

// ensureVoiceSlots brings the voice area back in step with the banks
// whenever an operation moves a bstep (see bstepSum). It is the one
// place that does so.
//
// delta is the change to the summed bstep values that d.fzf does not
// carry yet, so an operation that raises a bstep through a patch
// passes 1 and one that has already written its change passes 0. An
// operation that lowers a bstep writes first and passes 0 on purpose:
// which slots are still played is read off the areas that survive.
// freed names the slot the caller's own area gave up, or noFreedSlot.
//
// Where the summed bsteps already run above the walked count (hardware
// dumps share voices through vp[], so the bound stops nothing and the
// walk ends on the audio's own bytes) one more area changes neither
// and there is nothing to do.
func ensureVoiceSlots(d *dumpState, delta, freed int) *Error {
	return resizeVoiceArea(d, delta, freed, 0)
}

// resizeVoiceArea makes the voice area hold exactly target slots,
// moving the voice and audio boundary to suit. Wave pointers are
// sample addresses into the audio area, so the audio moving with its
// own start leaves every voice playing the same samples.
func resizeVoiceArea(d *dumpState, delta, freed, target int) *Error {
	params := container.VoiceAreaResizeParams{
		BankCount: d.header.NBankSectors, VoiceCount: d.header.NVoice,
		VoiceStart: d.header.VoiceAreaStart, AudioStart: d.audioStart,
		WalkBound: d.walkBound, BStepDelta: delta, FreedSlot: freed,
		DiskMode: d.disVN > 0,
	}
	var (
		resized container.VoiceAreaResize
		err     error
	)
	if target > 0 {
		resized, err = container.ResizeVoiceAreaToOwned(d.fzf, params, target)
	} else {
		resized, err = container.ResizeVoiceAreaOwned(d.fzf, params)
	}
	if err != nil {
		return voiceAreaBoundaryError(err)
	}
	d.fzf, d.header.NVoice, d.audioStart = resized.Data, resized.VoiceCount, resized.AudioStart
	return nil
}

func voiceAreaBoundaryError(err error) *Error {
	var areaErr *container.VoiceAreaError
	if !errors.As(err, &areaErr) {
		return errf("invalid-image", "voice area could not be resized")
	}
	switch {
	case errors.Is(err, container.ErrVoiceLimit) && areaErr.Extra > 0:
		return errf("voice-limit", "the instrument already holds %d of %d voice slots, and the areas arriving need %d more",
			areaErr.VoiceCount, disk.MaxVoices, areaErr.Extra)
	case errors.Is(err, container.ErrVoiceLimit):
		return errf("voice-limit", "the instrument already holds %d voices; this one needs a free slot", disk.MaxVoices)
	case errors.Is(err, container.ErrMinimumArea):
		return errf(codeInvalidValue, "an instrument needs at least one area")
	case errors.Is(err, container.ErrSpareVoice):
		label := fmt.Sprintf("in slot %d", areaErr.Slot)
		if areaErr.Name != "" {
			label = fmt.Sprintf("%q", areaErr.Name)
		}
		return errf(codeSpareVoice, "voice %s is played by no area, and the voice area has to give a slot back; map it or delete it first", label)
	case errors.Is(err, container.ErrNoSpareVoice):
		return errf("voice-limit", "every voice slot is still played by an area; giving one back would drop a voice")
	case errors.Is(err, container.ErrInvalidVoiceArea):
		return errf("invalid-image", "voice slot %d extends past the voice area", areaErr.Slot)
	default:
		return errf("invalid-image", "voice area could not be resized")
	}
}

// patchDump extracts the document's full dump (stitched across a
// split pair), lets build mutate it (in place or via patches), and
// writes it back, re-splitting or collapsing as its size dictates.
func (s *Session) patchDump(build func(d *dumpState) ([]model.Patch, *Error)) (Snapshot, *Error) {
	if s.image == nil {
		return s.Snapshot(), errf(codeNoDisk, "no disk is open")
	}
	if s.instrument == nil {
		return s.Snapshot(), errf("no-instrument", "the disk has no full dump to edit")
	}
	img, ierr := s.openedImage()
	if ierr != nil {
		return s.Snapshot(), ierr
	}
	fzf, cerr := s.stitchedDump(img)
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	disVN := 0
	if s.usesDIS() {
		disVN = disVoiceCount(img)
	}
	out, outVN, cerr := patchDumpBytes(fzf, disVN, build)
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	return s.replaceDump(img, out, outVN, modeKeep)
}

// patchDumpBytes runs one operation over a full dump's bytes: build
// mutates the state in place or through the patches it returns, each
// patch verifies its pre-image, and the result has to read back as the
// dump the operation meant to write. It also returns the count the
// write-back stamps: the parsed count, never the bstep sum.
func patchDumpBytes(fzf []byte, disVN int, build func(d *dumpState) ([]model.Patch, *Error)) ([]byte, int, *Error) {
	d, cerr := newDumpState(fzf, disVN)
	if cerr != nil {
		return nil, 0, cerr
	}
	patches, cerr := build(d)
	if cerr != nil {
		return nil, 0, cerr
	}
	if err := model.Apply(d.fzf, patches); err != nil {
		return nil, 0, errf("patch-failed", "%v", err)
	}
	if cerr := d.checkGeometry(); cerr != nil {
		return nil, 0, cerr
	}
	// DIS mode: retire the stale slots between the count and the voice
	// area's end, or a reopen whose bstep walk now reaches them would
	// resurrect the firmware's leftovers as voices.
	if d.disVN > 0 {
		for slot := d.header.NVoice; ; slot++ {
			off := disk.VoiceSlotOffset(d.header.VoiceAreaStart, slot)
			if off+disk.VoicePackSize > d.audioStart {
				break
			}
			clear(d.fzf[off : off+disk.VoicePackSize])
		}
	}
	return d.fzf, d.header.NVoice, nil
}

func applyDocumentOperation(d *dumpState, result fzfmodel.OperationResult) *Error {
	updated, err := result.ApplyOwned(d.fzf)
	if err != nil {
		return errf("patch-failed", "%v", err)
	}
	if result.IsStructural() {
		d.fzf = updated
		if voiceCount, audioStart, ok := result.VoiceGeometry(); ok {
			d.header.NVoice = voiceCount
			d.audioStart = audioStart
		}
		return nil
	}
	d.fzf = updated
	return nil
}

// checkArea validates bank and area indices against the dump.
func (d *dumpState) checkArea(bank, area int) *Error {
	if bank < 0 || bank >= d.header.NBankSectors {
		return errf(codeInvalidValue, "bank must be 0 to %d, got %d", d.header.NBankSectors-1, bank)
	}
	if area < 0 || area >= bankBstep(d.fzf, bank) {
		return errf(codeInvalidValue, "area %d out of range", area)
	}
	return nil
}

func bytePatch(data []byte, offset int, value byte) model.Patch {
	return model.Patch{Offset: offset, Old: []byte{data[offset]}, New: []byte{value}}
}

// clampByte clamps to [0, hi]; gosec sees the byte-safe bound.
func clampByte(v, hi int) byte {
	return clampByteFrom(v, 0, hi)
}

// clampByteFrom clamps into an explicit span, for rows whose floor
// isn't zero. The panel's MAX TOUCH and MIN TOUCH both start at 001,
// and a velocity of zero silences the voice.
func clampByteFrom(v, lo, hi int) byte {
	return byte(clampInt(v, lo, hi) & 0xff) // #nosec G115 -- clamped to [lo,hi] within 0..255
}

// SetAreaField edits one Area field (R12). Numeric fields clamp to
// their ranges; midiChannel speaks the display scale (1 to 16);
// voiceSlot re-points the area at another voice.
func (s *Session) SetAreaField(bank, area int, field string, value int) (Snapshot, *Error) {
	return s.patchDump(func(d *dumpState) ([]model.Patch, *Error) {
		if cerr := d.checkArea(bank, area); cerr != nil {
			return nil, cerr
		}
		base := bank * disk.SectorSize
		switch field {
		case "keyLow":
			return []model.Patch{bytePatch(d.fzf, base+disk.BankKeyLowOffset+area, clampByte(value, 127))}, nil
		case "keyHigh":
			return []model.Patch{bytePatch(d.fzf, base+disk.BankKeyHighOffset+area, clampByte(value, 127))}, nil
		case "root":
			return []model.Patch{bytePatch(d.fzf, base+disk.BankKeyCentOffset+area, clampByte(value, 127))}, nil
		case fieldAreaVelLow:
			return []model.Patch{bytePatch(d.fzf, base+disk.BankVelLowOffset+area, clampByteFrom(value, disk.MinVelocity, 127))}, nil
		case fieldAreaVelHigh:
			return []model.Patch{bytePatch(d.fzf, base+disk.BankVelHighOffset+area, clampByteFrom(value, disk.MinVelocity, 127))}, nil
		case "volume":
			// The panel's AREA LEVEL counts the opposite way to the
			// stored byte, so convert rather than clamping the raw value.
			return []model.Patch{bytePatch(d.fzf, base+disk.BankVolumeOffset+area, disk.AreaLevelToByte(value))}, nil
		case "midiChannel":
			return []model.Patch{bytePatch(d.fzf, base+disk.BankMIDIRecvChanOffset+area, clampByte(value-1, 15))}, nil
		case "output":
			return []model.Patch{bytePatch(d.fzf, base+disk.BankAudioOutOffset+area, clampByte(value, 255))}, nil
		case "voiceSlot":
			if value < 0 || value >= d.header.NVoice {
				return nil, errf(codeInvalidValue, "voice slot %d out of range", value)
			}
			off := base + disk.BankVoiceNumOffset + area*disk.VPEntrySize
			old := make([]byte, disk.VPEntrySize)
			copy(old, d.fzf[off:off+disk.VPEntrySize])
			replacement := make([]byte, disk.VPEntrySize)
			binary.LittleEndian.PutUint16(replacement, uint16(value)) // #nosec G115 -- bounded by NVoice above
			return []model.Patch{{Offset: off, Old: old, New: replacement}}, nil
		default:
			return nil, errf("invalid-field", "%q is not an area field", field)
		}
	})
}

// RenameBank sets a bank's 12-character printable ASCII name (R11).
func (s *Session) RenameBank(bank int, name string) (Snapshot, *Error) {
	return s.patchDump(func(d *dumpState) ([]model.Patch, *Error) {
		result, err := d.doc.RenameBank(bank, name)
		if err == nil {
			return nil, applyDocumentOperation(d, result)
		}
		if boundaryErr := nameBoundaryError(err, "bank", fzfmodel.ErrBankNameEmpty, fzfmodel.ErrBankNameTooLong, fzfmodel.ErrBankNameNotASCII); boundaryErr != nil {
			return nil, boundaryErr
		}
		if errors.Is(err, fzfmodel.ErrBankIndexOutOfRange) {
			return nil, errf(codeInvalidValue, "bank %d out of range", bank)
		}
		return nil, errf(codeInvalidValue, "bank could not be renamed")
	})
}

// SwapAreas reorders two areas within a bank (R11).
func (s *Session) SwapAreas(bank, a, b int) (Snapshot, *Error) {
	return s.patchDump(func(d *dumpState) ([]model.Patch, *Error) {
		result, err := d.doc.SwapAreas(bank, a, b)
		if err != nil {
			var indexErr *fzfmodel.IndexError
			if errors.As(err, &indexErr) {
				switch {
				case errors.Is(err, fzfmodel.ErrBankIndexOutOfRange):
					return nil, errf(codeInvalidValue, "bank must be 0 to %d, got %d", indexErr.Limit-1, indexErr.Index)
				case errors.Is(err, fzfmodel.ErrAreaIndexOutOfRange):
					return nil, errf(codeInvalidValue, "area %d out of range", indexErr.Index)
				}
			}
			return nil, errf(codeInvalidValue, "areas could not be swapped")
		}
		return nil, applyDocumentOperation(d, result)
	})
}

// DeleteArea removes an area, shifting later areas down (R11), and
// gives its voice slot back to the voice area with it: the format sizes
// a dump's voice area from the banks' area counts, so a voice no area
// references has nowhere to live. The freed voice's samples stay in the
// audio area until the next rebuild, and undo restores the area and the
// voice together.
func (s *Session) DeleteArea(bank, area int) (Snapshot, *Error) {
	return s.patchDump(func(d *dumpState) ([]model.Patch, *Error) {
		return deleteAreaPatches(d, bank, area)
	})
}

func deleteAreaPatches(d *dumpState, bank, area int) ([]model.Patch, *Error) {
	result, err := d.doc.DeleteArea(bank, area)
	if err != nil {
		return nil, documentAreaBoundaryError(err, bank, area)
	}
	return nil, applyDocumentOperation(d, result)
}

// AddArea appends an area playing an existing voice slot, with the
// container's default full key and velocity ranges, the voice's own
// root key, and every generator as its output.
func (s *Session) AddArea(bank, voiceSlot int) (Snapshot, *Error) {
	return s.patchDump(func(d *dumpState) ([]model.Patch, *Error) {
		return addAreaPatches(d, bank, voiceSlot)
	})
}

func addAreaPatches(d *dumpState, bank, voiceSlot int) ([]model.Patch, *Error) {
	result, err := d.doc.AddArea(bank, voiceSlot)
	if err != nil {
		return nil, documentAreaBoundaryError(err, bank, voiceSlot)
	}
	return nil, applyDocumentOperation(d, result)
}

func documentAreaBoundaryError(err error, bank, index int) *Error {
	var indexErr *fzfmodel.IndexError
	if errors.As(err, &indexErr) {
		switch {
		case errors.Is(err, fzfmodel.ErrBankIndexOutOfRange):
			return errf(codeInvalidValue, "bank %d out of range", indexErr.Index)
		case errors.Is(err, fzfmodel.ErrVoiceIndexOutOfRange):
			return errf(codeInvalidValue, "voice slot %d out of range", indexErr.Index)
		case errors.Is(err, fzfmodel.ErrAreaIndexOutOfRange):
			return errf(codeInvalidValue, "area %d out of range", indexErr.Index)
		}
	}
	switch {
	case errors.Is(err, fzfmodel.ErrBankFull):
		return errf("bank-full", "bank %d already holds %d areas", bank, disk.MaxVoices)
	case errors.Is(err, fzfmodel.ErrLastArea):
		return errf(codeLastArea, "area %d is bank %d's only area; a bank with no areas drops out of the dump", index, bank)
	case errors.Is(err, container.ErrVoiceLimit), errors.Is(err, container.ErrMinimumArea),
		errors.Is(err, container.ErrSpareVoice), errors.Is(err, container.ErrNoSpareVoice),
		errors.Is(err, container.ErrInvalidVoiceArea):
		return voiceAreaBoundaryError(err)
	default:
		return errf(codeInvalidValue, "area operation could not be completed")
	}
}

// MapVoice maps a voice into the first bank with room, in one action
// (R13). The new area arrives with the container's playable defaults.
func (s *Session) MapVoice(voiceSlot int) (Snapshot, *Error) {
	return s.patchDump(func(d *dumpState) ([]model.Patch, *Error) {
		return mapVoicePatches(d, voiceSlot)
	})
}

func mapVoicePatches(d *dumpState, voiceSlot int) ([]model.Patch, *Error) {
	for bank := 0; bank < d.header.NBankSectors; bank++ {
		if bankBstep(d.fzf, bank) < disk.MaxVoices {
			return addAreaPatches(d, bank, voiceSlot)
		}
	}
	return nil, errf("bank-full", "every bank already holds %d areas", disk.MaxVoices)
}

// DuplicateArea appends a clone of an area: the velocity switch
// workflow (R11). The clone's voice header is copied into a fresh slot
// at the end of the voice area (growing it by a sector when full), so
// the two areas share audio and the PCM footprint is unchanged.
func (s *Session) DuplicateArea(bank, area int) (Snapshot, *Error) {
	return s.patchDump(func(d *dumpState) ([]model.Patch, *Error) {
		return duplicateAreaPatches(d, bank, area)
	})
}

func duplicateAreaPatches(d *dumpState, bank, area int) ([]model.Patch, *Error) {
	result, err := d.doc.DuplicateArea(bank, area)
	if err == nil {
		return nil, applyDocumentOperation(d, result)
	}
	var indexErr *fzfmodel.IndexError
	if errors.As(err, &indexErr) {
		switch {
		case errors.Is(err, fzfmodel.ErrBankIndexOutOfRange):
			return nil, errf(codeInvalidValue, "bank must be 0 to %d, got %d", indexErr.Limit-1, indexErr.Index)
		case errors.Is(err, fzfmodel.ErrAreaIndexOutOfRange):
			return nil, errf(codeInvalidValue, "area %d out of range", indexErr.Index)
		}
	}
	var voiceErr *fzfmodel.AreaVoiceError
	if errors.As(err, &voiceErr) {
		return nil, errf(codeInvalidValue, "area %d plays voice slot %d, and this instrument holds %d slots",
			voiceErr.Area, voiceErr.Voice, voiceErr.VoiceCount)
	}
	switch {
	case errors.Is(err, fzfmodel.ErrBankFull):
		return nil, errf("bank-full", "bank %d already holds %d areas", bank, disk.MaxVoices)
	case errors.Is(err, fzfmodel.ErrVoiceHeaderBounds):
		return nil, errf("invalid-image", "source voice out of bounds")
	case errors.Is(err, container.ErrVoiceLimit):
		return nil, voiceAreaBoundaryError(err)
	default:
		return nil, errf(codeInvalidValue, "area could not be duplicated")
	}
}
