package webcore

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/container"
	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/model"
)

// applyModelPatches applies container patches to raw dump
// bytes, verifying each pre-image before it writes. A
// mismatch means the caller's read of the container went stale, which
// must fail loudly rather than corrupt a bank sector.
func applyModelPatches(data []byte, patches []model.Patch) error {
	for _, p := range patches {
		if p.Offset < 0 || p.Offset+len(p.Old) > len(data) {
			return fmt.Errorf("patch at %d out of bounds", p.Offset)
		}
		if !bytes.Equal(data[p.Offset:p.Offset+len(p.Old)], p.Old) {
			return fmt.Errorf("patch pre-image mismatch at %d", p.Offset)
		}
		copy(data[p.Offset:p.Offset+len(p.New)], p.New)
	}
	return nil
}

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
}

// newDumpState parses a full dump into the context the area ops work
// over.
func newDumpState(fzf []byte) (*dumpState, *Error) {
	hdr, err := fzutil.ParseFZFHeader(fzf)
	if err != nil {
		return nil, errf("invalid-image", "%v", err)
	}
	start := hdr.VoiceAreaStart + disk.VoiceAreaSectors(hdr.NVoice)*disk.SectorSize
	if start > len(fzf) {
		return nil, errf("invalid-image", "the voice area runs past the dump")
	}
	return &dumpState{
		fzf:        fzf,
		header:     hdr,
		audioStart: start,
		walkBound:  min(bstepSum(fzf, hdr.NBankSectors), disk.MaxVoices),
	}, nil
}

// voiceAreaSectors is the number of sectors the voice area physically
// spans, which is four slots each.
func (d *dumpState) voiceAreaSectors() int {
	return (d.audioStart - d.header.VoiceAreaStart) / disk.SectorSize
}

// checkGeometry holds an operation to the one thing every reader agrees
// on: the audio starts at the byte after the voice area the walked
// count sizes (see bstepSum). When that byte is not where this
// operation left the audio, every voice plays from the wrong offset, so
// the operation is refused rather than written.
func (d *dumpState) checkGeometry() *Error {
	hdr, err := fzutil.ParseFZFHeader(d.fzf)
	if err != nil {
		return errf(codeVoiceWalk, "the change leaves a dump no reader can parse: %v", err)
	}
	read := hdr.VoiceAreaStart + disk.VoiceAreaSectors(hdr.NVoice)*disk.SectorSize
	if read != d.audioStart {
		return errf(codeVoiceWalk,
			"the change moves the audio: a reader walks %d voices and looks for the audio at byte %d, where this dump holds it at %d",
			hdr.NVoice, read, d.audioStart)
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

// slotReferenced reports whether any bank's vp[] plays the voice in
// this slot.
func slotReferenced(d *dumpState, slot int) bool {
	return len(fzutil.FindBankSitesForVoice(d.fzf, d.header, slot)) > 0
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
	count := d.header.NVoice
	sum := bstepSum(d.fzf, d.header.NBankSectors) + delta
	target := count
	switch {
	case sum < count:
		// The lowered bound cuts the walk short, so the slots past it
		// leave the voice area and the audio comes up to meet them.
		target = sum
	case count == d.walkBound:
		// The bound is what stops the walk, so raising it has to land on
		// real empty slots rather than on the audio.
		if sum > disk.MaxVoices {
			return errf("voice-limit",
				"the instrument already holds %d of %d voice slots, and the areas arriving need %d more",
				count, disk.MaxVoices, sum-disk.MaxVoices)
		}
		target = sum
	}
	if target < 1 {
		return errf(codeInvalidValue, "an instrument needs at least one area")
	}
	return resizeVoiceArea(d, target, freed)
}

// allocVoiceSlot reserves the next slot for an operation that writes a
// real voice header into it: duplicate clones one, and a joining voice
// brings its own. Those raise a bstep and fill a slot in the same
// move, so the slot is taken outright rather than derived from the
// bound.
func allocVoiceSlot(d *dumpState) (int, *Error) {
	slot := d.header.NVoice
	if slot >= disk.MaxVoices {
		return 0, errf("voice-limit",
			"the instrument already holds %d voices; this one needs a free slot", disk.MaxVoices)
	}
	if cerr := resizeVoiceArea(d, slot+1, noFreedSlot); cerr != nil {
		return 0, cerr
	}
	return slot, nil
}

// resizeVoiceArea makes the voice area hold exactly target slots,
// moving the voice and audio boundary to suit. Wave pointers are
// sample addresses into the audio area, so the audio moving with its
// own start leaves every voice playing the same samples.
func resizeVoiceArea(d *dumpState, target, freed int) *Error {
	switch {
	case target > d.header.NVoice:
		if cerr := growVoiceSlots(d, target); cerr != nil {
			return cerr
		}
	case target < d.header.NVoice:
		if cerr := shrinkVoiceSlots(d, target, freed); cerr != nil {
			return cerr
		}
	default:
		return nil
	}
	d.header.NVoice = target
	return nil
}

// growVoiceSlots opens the slots between the current count and target,
// growing the voice area by whole sectors when the count crosses one.
func growVoiceSlots(d *dumpState, target int) *Error {
	if grow := disk.VoiceAreaSectors(target) - d.voiceAreaSectors(); grow > 0 {
		growBytes := grow * disk.SectorSize
		grown := make([]byte, len(d.fzf)+growBytes)
		copy(grown[:d.audioStart], d.fzf[:d.audioStart])
		copy(grown[d.audioStart+growBytes:], d.fzf[d.audioStart:])
		d.fzf = grown
		d.audioStart += growBytes
	}
	// A claimed slot has to read as an empty slot rather than as
	// whatever the padding held: a stale header there would walk into
	// the count as a voice of its own.
	for slot := d.header.NVoice; slot < target; slot++ {
		off := disk.VoiceSlotOffset(d.header.VoiceAreaStart, slot)
		if off+disk.VoicePackSize > d.audioStart {
			return errf("invalid-image", "voice slot %d extends past the voice area", slot)
		}
		clear(d.fzf[off : off+disk.VoicePackSize])
	}
	return nil
}

// shrinkVoiceSlots gives slots back until the voice area holds target,
// and shrinks it by a sector each time the count crosses one. A slot
// that falls off the end while an area still plays it takes an unused
// slot's place first, so no area is left pointing past the count.
func shrinkVoiceSlots(d *dumpState, target, freed int) *Error {
	sectors := d.voiceAreaSectors()
	capacity := sectors * disk.VoicesPerSector
	for count := d.header.NVoice; count > target; count-- {
		if !slotReferenced(d, count-1) {
			continue // an unplayed top slot just falls off the end
		}
		drop, cerr := spareVoiceSlot(d, freed, count-1)
		if cerr != nil {
			return cerr
		}
		compactVoiceSlot(d, drop, capacity)
		switch {
		case freed == drop:
			freed = noFreedSlot
		case freed > drop:
			freed--
		}
	}
	// Past the count is padding; clear it so no stale header walks back
	// into the count when the next area arrives.
	if tail := disk.VoiceSlotOffset(d.header.VoiceAreaStart, target); tail < d.audioStart {
		clear(d.fzf[tail:d.audioStart])
	}
	if shrink := (sectors - disk.VoiceAreaSectors(target)) * disk.SectorSize; shrink > 0 {
		d.fzf = append(d.fzf[:d.audioStart-shrink], d.fzf[d.audioStart:]...)
		d.audioStart -= shrink
	}
	return nil
}

// spareVoiceSlot picks the slot a shrinking voice area gives up, from
// the slots below limit that no area plays. The deleted area's own
// slot goes first, so an area that leaves takes its voice with it.
// Failing that a silent placeholder goes, which costs nothing. What
// stays is a named voice no area plays, so the operation refuses and
// names it rather than losing it in silence (codeSpareVoice).
func spareVoiceSlot(d *dumpState, freed, limit int) (int, *Error) {
	if freed >= 0 && freed < limit && !slotReferenced(d, freed) {
		return freed, nil
	}
	named := noFreedSlot
	for slot := limit - 1; slot >= 0; slot-- {
		if slotReferenced(d, slot) {
			continue
		}
		if slotIsPlaceholder(d, slot) {
			return slot, nil
		}
		if named < 0 {
			named = slot
		}
	}
	if named >= 0 {
		return 0, errf(codeSpareVoice,
			"voice %s is played by no area, and the voice area has to give a slot back; map it or delete it first",
			slotLabel(d, named))
	}
	return 0, errf("voice-limit",
		"every voice slot is still played by an area; giving one back would drop a voice")
}

// slotIsPlaceholder reports whether a slot holds the silent
// placeholder the format allows: no sound and no samples of its own,
// so dropping it costs nothing.
func slotIsPlaceholder(d *dumpState, slot int) bool {
	off := disk.VoiceSlotOffset(d.header.VoiceAreaStart, slot)
	if off+disk.VoiceHeaderUsed > len(d.fzf) {
		return false
	}
	h := d.fzf[off : off+disk.VoiceHeaderUsed]
	if binary.LittleEndian.Uint16(h[disk.VoiceLoopModeOffset:]) != disk.PlaybackModeNoSound {
		return false
	}
	return binary.LittleEndian.Uint32(h[disk.VoiceWaveStartOffset:]) ==
		binary.LittleEndian.Uint32(h[disk.VoiceWaveEndOffset:])
}

// slotLabel names a voice slot the way the voice list does, falling
// back to the slot number for the blank names factory dumps carry.
func slotLabel(d *dumpState, slot int) string {
	off := disk.VoiceSlotOffset(d.header.VoiceAreaStart, slot)
	if off+disk.VoiceNameOffset+disk.LabelSize <= len(d.fzf) {
		name := disk.TrimPadded(d.fzf[off+disk.VoiceNameOffset : off+disk.VoiceNameOffset+disk.LabelSize])
		if name != "" {
			return fmt.Sprintf("%q", name)
		}
	}
	return fmt.Sprintf("in slot %d", slot)
}

// compactVoiceSlot takes one slot out of the voice area: the slots
// above it shift down by one pack, and every vp[] entry above it
// counts down with them, so each area keeps the voice it had.
func compactVoiceSlot(d *dumpState, slot, count int) {
	start := d.header.VoiceAreaStart
	end := start + disk.VoiceAreaSectors(count)*disk.SectorSize
	if end > len(d.fzf) {
		end = len(d.fzf)
	}
	from := disk.VoiceSlotOffset(start, slot)
	if from+disk.VoicePackSize > end {
		return
	}
	// Slots are contiguous 256 byte packs, four to a sector, so the
	// shift is one move.
	copy(d.fzf[from:], d.fzf[from+disk.VoicePackSize:end])
	clear(d.fzf[end-disk.VoicePackSize : end])
	for b := 0; b < d.header.NBankSectors; b++ {
		base := b * disk.SectorSize
		for i := 0; i < bankBstep(d.fzf, b); i++ {
			off := base + disk.BankVoiceNumOffset + i*disk.VPEntrySize
			if off+disk.VPEntrySize > len(d.fzf) {
				break
			}
			if v := int(binary.LittleEndian.Uint16(d.fzf[off:])); v > slot {
				binary.LittleEndian.PutUint16(d.fzf[off:], uint16(v-1)) // #nosec G115 -- a slot index, bounded by disk.MaxVoices
			}
		}
	}
}

// voiceRootKey reads a voice slot's own root key, the note its samples
// were recorded at. A fresh area plays the voice from there. A zero
// byte is not a usable root: it is what a blank slot carries, and the
// empty instrument's silent placeholder is all zeros, so taking it at
// face value pitches every note from C-1. Middle C stands in for it.
func voiceRootKey(d *dumpState, slot int) byte {
	off := disk.VoiceSlotOffset(d.header.VoiceAreaStart, slot) + disk.VoiceKeyCentOffset
	if off >= len(d.fzf) {
		return defaultRootKey
	}
	root := d.fzf[off]
	if root == 0 || root > disk.MaxMIDINote {
		return defaultRootKey
	}
	return root
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
	out, cerr := patchDumpBytes(fzf, build)
	if cerr != nil {
		return s.Snapshot(), cerr
	}
	return s.replaceDump(img, out)
}

// patchDumpBytes runs one operation over a full dump's bytes: build
// mutates the state in place or through the patches it returns, each
// patch verifies its pre-image, and the result has to read back as the
// dump the operation meant to write. It returns the new dump, leaving
// the caller's bytes untouched on any refusal.
func patchDumpBytes(fzf []byte, build func(d *dumpState) ([]model.Patch, *Error)) ([]byte, *Error) {
	d, cerr := newDumpState(fzf)
	if cerr != nil {
		return nil, cerr
	}
	patches, cerr := build(d)
	if cerr != nil {
		return nil, cerr
	}
	if err := applyModelPatches(d.fzf, patches); err != nil {
		return nil, errf("patch-failed", "%v", err)
	}
	if cerr := d.checkGeometry(); cerr != nil {
		return nil, cerr
	}
	return d.fzf, nil
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
	return byte(clampInt(v, 0, hi) & 0xff) // #nosec G115 -- clamped to [0,hi] within 0..255
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
		case "velLow":
			return []model.Patch{bytePatch(d.fzf, base+disk.BankVelLowOffset+area, clampByte(value, 127))}, nil
		case "velHigh":
			return []model.Patch{bytePatch(d.fzf, base+disk.BankVelHighOffset+area, clampByte(value, 127))}, nil
		case "volume":
			return []model.Patch{bytePatch(d.fzf, base+disk.BankVolumeOffset+area, clampByte(value, 127))}, nil
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
	if len(name) == 0 || len(name) > disk.LabelSize {
		return s.Snapshot(), errf(codeInvalidValue, "bank name must be 1 to %d characters", disk.LabelSize)
	}
	for _, r := range name {
		if r < disk.PrintableASCIIMin || r > disk.PrintableASCIIMax {
			return s.Snapshot(), errf(codeInvalidValue, "bank name contains non-ASCII character %q", string(r))
		}
	}
	return s.patchDump(func(d *dumpState) ([]model.Patch, *Error) {
		if bank < 0 || bank >= d.header.NBankSectors {
			return nil, errf(codeInvalidValue, "bank %d out of range", bank)
		}
		off := bank*disk.SectorSize + disk.BankNameOffset
		padded := disk.PadLabel(name)
		old := make([]byte, disk.LabelSize)
		copy(old, d.fzf[off:off+disk.LabelSize])
		return []model.Patch{{Offset: off, Old: old, New: padded[:]}}, nil
	})
}

// SwapAreas reorders two areas within a bank (R11).
func (s *Session) SwapAreas(bank, a, b int) (Snapshot, *Error) {
	return s.patchDump(func(d *dumpState) ([]model.Patch, *Error) {
		if cerr := d.checkArea(bank, a); cerr != nil {
			return nil, cerr
		}
		if cerr := d.checkArea(bank, b); cerr != nil {
			return nil, cerr
		}
		return container.SwapAreaPatches(d.fzf, container.SwapAreaParams{
			Base: bank * disk.SectorSize, SrcArea: a, TgtArea: b,
		}), nil
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
	if cerr := d.checkArea(bank, area); cerr != nil {
		return nil, cerr
	}
	bstep := bankBstep(d.fzf, bank)
	if bstep <= 1 {
		// A bank with no areas drops out of the dump on the next
		// parse, taking every later bank's mapping with it.
		return nil, errf(codeLastArea,
			"area %d is bank %d's only area; a bank with no areas drops out of the dump", area, bank)
	}
	freed, _ := disk.BankVPLookup(d.fzf, bank, area)
	patches := container.DeleteAreaPatches(d.fzf, container.DeleteAreaParams{
		Base: bank * disk.SectorSize, AreaIdx: area, Bstep: bstep,
	})
	// The lowered bstep goes in first, so the scan for slots areas still
	// play sees the areas that survive rather than the one leaving.
	if err := applyModelPatches(d.fzf, patches); err != nil {
		return nil, errf("patch-failed", "%v", err)
	}
	if cerr := ensureVoiceSlots(d, 0, freed); cerr != nil {
		return nil, cerr
	}
	return nil, nil
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
	if bank < 0 || bank >= d.header.NBankSectors {
		return nil, errf(codeInvalidValue, "bank %d out of range", bank)
	}
	if voiceSlot < 0 || voiceSlot >= d.header.NVoice {
		return nil, errf(codeInvalidValue, "voice slot %d out of range", voiceSlot)
	}
	bstep := bankBstep(d.fzf, bank)
	if bstep >= disk.MaxVoices {
		return nil, errf("bank-full", "bank %d already holds %d areas", bank, disk.MaxVoices)
	}
	root := voiceRootKey(d, voiceSlot)
	// The bstep bump rides in the patches below, so the voice area is
	// brought in step with a change it does not carry yet.
	if cerr := ensureVoiceSlots(d, 1, noFreedSlot); cerr != nil {
		return nil, cerr
	}
	base := bank * disk.SectorSize
	vpOff := base + disk.BankVoiceNumOffset + bstep*disk.VPEntrySize
	oldVP := make([]byte, disk.VPEntrySize)
	copy(oldVP, d.fzf[vpOff:vpOff+disk.VPEntrySize])
	newVP := make([]byte, disk.VPEntrySize)
	binary.LittleEndian.PutUint16(newVP, uint16(voiceSlot)) // #nosec G115 -- bounded by NVoice above
	patches := []model.Patch{{Offset: vpOff, Old: oldVP, New: newVP}}
	patches = append(patches, container.DefaultBankRangePatches(d.fzf, bank, bstep)...)
	// The area arrives playable, as the join path builds one: the
	// voice's own root key, and the CLI builder's output default. A
	// zero gchn routes to no generator, which the sampler plays
	// silently, and a zero root pitches every note from C-1.
	patches = append(patches,
		bytePatch(d.fzf, base+disk.BankKeyCentOffset+bstep, root),
		bytePatch(d.fzf, base+disk.BankAudioOutOffset+bstep, disk.PolyphonicAudioOut),
	)
	if bump, ok := container.BankBstepBumpPatch(d.fzf, bank, bstep); ok {
		patches = append(patches, bump)
	}
	return patches, nil
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
	if cerr := d.checkArea(bank, area); cerr != nil {
		return nil, cerr
	}
	bstep := bankBstep(d.fzf, bank)
	if bstep >= disk.MaxVoices {
		return nil, errf("bank-full", "bank %d already holds %d areas", bank, disk.MaxVoices)
	}
	srcSlot, ok := disk.BankVPLookup(d.fzf, bank, area)
	if !ok {
		return nil, errf(codeInvalidValue, "area %d has no voice", area)
	}
	// The same bound every other area op holds to. An area whose vp[]
	// entry runs past the walked count plays bytes the reader takes for
	// audio, and cloning those would give the instrument a voice made of
	// samples rather than a copy of one it holds.
	if srcSlot >= d.header.NVoice {
		return nil, errf(codeInvalidValue,
			"area %d plays voice slot %d, and this instrument holds %d slots", area, srcSlot, d.header.NVoice)
	}
	voiceAreaStart := d.header.VoiceAreaStart
	srcOff := disk.VoiceSlotOffset(voiceAreaStart, srcSlot)
	if srcOff+disk.VoicePackSize > len(d.fzf) {
		return nil, errf("invalid-image", "source voice out of bounds")
	}
	srcHeader := make([]byte, disk.VoicePackSize)
	copy(srcHeader, d.fzf[srcOff:srcOff+disk.VoicePackSize])

	// The clone takes a fresh voice slot, so the instrument's total
	// voice count bounds it, not this bank's area count. The slot has to
	// sit at the walked count exactly: a gap would hide it from every
	// parser and be miscounted as audio.
	newSlot, cerr := allocVoiceSlot(d)
	if cerr != nil {
		return nil, cerr
	}

	return container.DuplicateAreaPatches(d.fzf, container.DuplicateAreaParams{
		Base:       bank * disk.SectorSize,
		NewOff:     disk.VoiceSlotOffset(voiceAreaStart, newSlot),
		SrcAreaIdx: area,
		Bstep:      bstep,
		NewSlot:    newSlot,
		SrcHeader:  srcHeader,
	}), nil
}
