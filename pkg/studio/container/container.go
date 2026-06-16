// Package container holds the pure FZF/disk container byte-surgery the
// studio TUI uses to reshape an in-memory container (compaction, area
// edits, etc.). Functions operate on the raw container bytes and return
// new bytes with no dependency on the TUI App, so this format logic (the
// riskiest code in studio, since a wrong offset corrupts a disk image)
// is unit-testable in isolation.
//
// Two mutation paradigms, split by whether the operation changes the
// container's length:
//   - Length-changing ops (CompactVoiceArea, CompactEmptyBanks, GrowBanks)
//     rebuild and return the whole buffer; the caller Replaces it.
//   - In-place ops (SwapAreaPatches, DeleteAreaPatches, DuplicateAreaPatches,
//     and the *Patch helpers) return []model.Patch the caller ApplyBatches.
//
// Operations with three or more same-typed parameters take a params struct
// so a transposed argument is a named-field mismatch, not silent corruption.
package container

import (
	"encoding/binary"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/studio/model"
)

// swapPerAreaFields are the per-area byte fields SwapAreaPatches swaps.
// NOTE: BankMIDIRecvChanOffset is intentionally absent: area swap does
// not move the per-area MIDI receive channel (see the studio report's
// REMAIN-008). Do not "complete" this list without a behaviour decision.
var swapPerAreaFields = []int{
	disk.BankKeyHighOffset,
	disk.BankKeyLowOffset,
	disk.BankVelHighOffset,
	disk.BankVelLowOffset,
	disk.BankKeyCentOffset,
	disk.BankAudioOutOffset,
	disk.BankVolumeOffset,
}

// SwapAreaParams identifies the two areas to swap within the bank sector
// at byte offset Base.
type SwapAreaParams struct {
	Base, SrcArea, TgtArea int
}

// SwapAreaPatches returns the patches that swap p.SrcArea and p.TgtArea
// within the bank sector at p.Base: each per-area byte field in
// swapPerAreaFields plus the 2-byte vp[] entry. Caller validates indices
// and applies the batch.
func SwapAreaPatches(data []byte, p SwapAreaParams) []model.Patch {
	patches := make([]model.Patch, 0, len(swapPerAreaFields)*2+2)
	for _, fieldBase := range swapPerAreaFields {
		srcOff := p.Base + fieldBase + p.SrcArea
		tgtOff := p.Base + fieldBase + p.TgtArea
		s, tg := data[srcOff], data[tgtOff]
		patches = append(patches,
			model.Patch{Offset: srcOff, Old: []byte{s}, New: []byte{tg}},
			model.Patch{Offset: tgtOff, Old: []byte{tg}, New: []byte{s}},
		)
	}
	srcVPOff := p.Base + disk.BankVoiceNumOffset + disk.VPEntrySize*p.SrcArea
	tgtVPOff := p.Base + disk.BankVoiceNumOffset + disk.VPEntrySize*p.TgtArea
	// srcVP/tgtVP are fresh snapshots taken before any write; model never
	// mutates patch slices, so each can serve as one patch's New and the
	// other's Old without a further copy.
	srcVP := append([]byte(nil), data[srcVPOff:srcVPOff+disk.VPEntrySize]...)
	tgtVP := append([]byte(nil), data[tgtVPOff:tgtVPOff+disk.VPEntrySize]...)
	patches = append(patches,
		model.Patch{Offset: srcVPOff, Old: srcVP, New: tgtVP},
		model.Patch{Offset: tgtVPOff, Old: tgtVP, New: srcVP},
	)
	return patches
}

// CompactVoiceArea drops trailing orphan voice slots (slots not
// referenced by any bank's vp[]) and truncates the audio area to the
// last live sample address. Mid-array orphans are not moved (that would
// renumber vp[] entries across all banks); trailing-only compaction
// handles the common "delete the last assignment" case.
//
// bankCount is the number of bank sectors; audioAreaStart is the byte
// offset where the audio area begins. It returns the rebuilt bytes and
// the new audio-area start. When nothing can be reclaimed it returns
// (data, audioAreaStart, false): the original slice unchanged, so a caller
// that overlooks the changed flag does a wasteful copy rather than a wipe.
func CompactVoiceArea(data []byte, bankCount, audioAreaStart int) (newData []byte, newAudioStart int, changed bool) {
	voiceAreaStart, requiredVoiceSectors, requiredAudioBytes, ok := compactPlan(data, bankCount, audioAreaStart)
	if !ok {
		return data, audioAreaStart, false
	}
	currentVoiceSectors := (audioAreaStart - voiceAreaStart) / disk.SectorSize
	voiceShrink := (currentVoiceSectors - requiredVoiceSectors) * disk.SectorSize
	if voiceShrink < 0 {
		voiceShrink = 0
	}
	currentAudioBytes := len(data) - audioAreaStart
	audioShrink := currentAudioBytes - requiredAudioBytes
	if audioShrink < 0 {
		audioShrink = 0
	}
	if voiceShrink == 0 && audioShrink == 0 {
		return data, audioAreaStart, false
	}

	newAudioStart = audioAreaStart - voiceShrink
	newTotal := voiceAreaStart + requiredVoiceSectors*disk.SectorSize + requiredAudioBytes
	out := make([]byte, newTotal)
	copy(out[:voiceAreaStart], data[:voiceAreaStart])
	copy(out[voiceAreaStart:newAudioStart],
		data[voiceAreaStart:voiceAreaStart+requiredVoiceSectors*disk.SectorSize])
	copy(out[newAudioStart:], data[audioAreaStart:audioAreaStart+requiredAudioBytes])
	return out, newAudioStart, true
}

// compactPlan computes the post-compaction geometry of the voice and
// audio areas without allocating. ok is false for a degenerate layout
// (no audio area yet, or sizes out of range), in which case callers
// leave the buffer unchanged.
func compactPlan(data []byte, bankCount, audioAreaStart int) (voiceAreaStart, requiredVoiceSectors, requiredAudioBytes int, ok bool) {
	voiceAreaStart = bankCount * disk.SectorSize
	if audioAreaStart <= voiceAreaStart || audioAreaStart > len(data) {
		return voiceAreaStart, 0, 0, false
	}
	if (audioAreaStart-voiceAreaStart)/disk.SectorSize <= 0 {
		return voiceAreaStart, 0, 0, false
	}
	maxLive := maxReferencedSlot(data, bankCount)
	requiredVoiceSectors = 1
	if maxLive >= 0 {
		requiredVoiceSectors = disk.VoiceAreaSectors(maxLive + 1)
	}
	if requiredVoiceSectors < 1 {
		requiredVoiceSectors = 1
	}
	maxSample := maxLiveSample(data, voiceAreaStart, maxLive)
	currentAudioBytes := len(data) - audioAreaStart
	requiredAudioBytes = 0
	if maxSample >= 0 {
		requiredAudioBytes = int(maxSample+1) * disk.BytesPerSample
	}
	if requiredAudioBytes > currentAudioBytes {
		requiredAudioBytes = currentAudioBytes
	}
	return voiceAreaStart, requiredVoiceSectors, requiredAudioBytes, true
}

// CompactedSize returns the byte length CompactVoiceArea would shrink
// the container to right now, computed without allocating. The App uses
// it for the free-space figure so reclaimable orphan audio (for example
// after an import is undone) is reflected immediately, not only once a
// save compacts it (N-04).
func CompactedSize(data []byte, bankCount, audioAreaStart int) int {
	voiceAreaStart, vsec, abytes, ok := compactPlan(data, bankCount, audioAreaStart)
	if !ok {
		return len(data)
	}
	return voiceAreaStart + vsec*disk.SectorSize + abytes
}

// CompactEmptyBanks removes every bank sector with bstep=0 (both
// trailing and middle gaps), packs the kept banks in order, and shifts
// the voice+audio areas earlier by the dropped bytes. At least one bank
// is always kept (an empty container is still a valid untitled disk).
// bankCount is the current bank count; audioAreaStart is the audio-area
// byte offset. Returns the rebuilt bytes, the new bank count, and the
// new audio-area start, or (data, bankCount, audioAreaStart, false) when
// no bank is removed (the original slice unchanged, never nil).
func CompactEmptyBanks(data []byte, bankCount, audioAreaStart int) (newData []byte, newBankCount, newAudioStart int, changed bool) {
	keep := make([]int, 0, bankCount)
	for b := 0; b < bankCount; b++ {
		base := b * disk.SectorSize
		if base+disk.BankVoiceCountOffset+2 > len(data) {
			break
		}
		bstep := int(binary.LittleEndian.Uint16(
			data[base+disk.BankVoiceCountOffset : base+disk.BankVoiceCountOffset+2]))
		if bstep > 0 {
			keep = append(keep, b)
		}
	}
	if len(keep) == 0 {
		keep = []int{0}
	}
	if len(keep) == bankCount {
		return data, bankCount, audioAreaStart, false
	}

	newBankCount = len(keep)
	voiceAreaStart := bankCount * disk.SectorSize
	newSize := newBankCount*disk.SectorSize + (len(data) - voiceAreaStart)
	out := make([]byte, newSize)
	for i, oldIdx := range keep {
		oldBase := oldIdx * disk.SectorSize
		copy(out[i*disk.SectorSize:(i+1)*disk.SectorSize], data[oldBase:oldBase+disk.SectorSize])
	}
	copy(out[newBankCount*disk.SectorSize:], data[voiceAreaStart:])
	droppedBytes := (bankCount - newBankCount) * disk.SectorSize
	return out, newBankCount, audioAreaStart - droppedBytes, true
}

// perAreaMetadataOffsets enumerates the one-byte-per-Area arrays inside
// a bank sector, shifted/copied in sync with vp[] by DeleteAreaPatches
// and DuplicateAreaPatches. Unlike swap (REMAIN-008) this list includes
// BankMIDIRecvChanOffset.
var perAreaMetadataOffsets = []int{
	disk.BankKeyHighOffset,
	disk.BankKeyLowOffset,
	disk.BankVelHighOffset,
	disk.BankVelLowOffset,
	disk.BankKeyCentOffset,
	disk.BankMIDIRecvChanOffset,
	disk.BankAudioOutOffset,
	disk.BankVolumeOffset,
}

// cloneVoiceHeaderPatch builds a 256-byte patch overwriting the voice
// slot at dstOff with srcHeader.
func cloneVoiceHeaderPatch(data []byte, dstOff int, srcHeader []byte) model.Patch {
	oldHdr := make([]byte, disk.VoicePackSize)
	copy(oldHdr, data[dstOff:dstOff+disk.VoicePackSize])
	return model.Patch{Offset: dstOff, Old: oldHdr, New: srcHeader}
}

// setVPEntryPatch builds the 2-byte patch that points vp[areaIdx] at
// slotIdx for the bank starting at base.
func setVPEntryPatch(data []byte, base, areaIdx, slotIdx int) model.Patch {
	off := base + disk.BankVoiceNumOffset + areaIdx*disk.VPEntrySize
	oldVP := make([]byte, disk.VPEntrySize)
	copy(oldVP, data[off:off+disk.VPEntrySize])
	newVP := make([]byte, disk.VPEntrySize)
	binary.LittleEndian.PutUint16(newVP, uint16(slotIdx)) //nolint:gosec // G115: slotIdx is a voice-slot index bounded by the voice-area capacity, well under uint16 max.
	return model.Patch{Offset: off, Old: oldVP, New: newVP}
}

// copyPerAreaMetadataPatches copies the per-area metadata bytes from
// srcAreaIdx into dstAreaIdx within the bank at base. No-op-valued
// patches are skipped.
func copyPerAreaMetadataPatches(data []byte, base, srcAreaIdx, dstAreaIdx int) []model.Patch {
	patches := make([]model.Patch, 0, len(perAreaMetadataOffsets))
	for _, arrOff := range perAreaMetadataOffsets {
		srcByte := data[base+arrOff+srcAreaIdx]
		dstPos := base + arrOff + dstAreaIdx
		if dstPos >= len(data) {
			continue
		}
		oldByte := data[dstPos]
		if srcByte == oldByte {
			continue
		}
		patches = append(patches, model.Patch{Offset: dstPos, Old: []byte{oldByte}, New: []byte{srcByte}})
	}
	return patches
}

// bumpBstepPatch builds the 2-byte patch writing newBstep into the
// bank's bstep field at bstepOff.
func bumpBstepPatch(data []byte, bstepOff, newBstep int) model.Patch {
	var newBuf [2]byte
	binary.LittleEndian.PutUint16(newBuf[:], uint16(newBstep)) //nolint:gosec // G115: bstep is a per-bank area count bounded by disk.MaxVoices (64), fits uint16.
	oldBuf := make([]byte, 2)
	copy(oldBuf, data[bstepOff:bstepOff+2])
	return model.Patch{Offset: bstepOff, Old: oldBuf, New: append([]byte(nil), newBuf[:]...)}
}

// DeleteAreaParams identifies the area to delete: AreaIdx within the bank
// at byte offset Base, where Bstep is the bank's current area count.
type DeleteAreaParams struct {
	Base, AreaIdx, Bstep int
}

// DeleteAreaPatches returns the patches that delete p.AreaIdx from the
// bank at p.Base: every per-area byte array and the vp[] entries shift down
// by one past p.AreaIdx, the freed last slot is zeroed, and bstep is
// decremented. The caller validates p.AreaIdx < p.Bstep.
func DeleteAreaPatches(data []byte, p DeleteAreaParams) []model.Patch {
	bstepOff := p.Base + disk.BankVoiceCountOffset
	patches := []model.Patch{
		bumpBstepPatch(data, bstepOff, p.Bstep-1),
	}
	for _, arrOff := range perAreaMetadataOffsets {
		for i := p.AreaIdx; i < p.Bstep; i++ {
			pos := p.Base + arrOff + i
			if pos+1 > len(data) {
				continue
			}
			var newByte byte
			if i < p.Bstep-1 {
				newByte = data[p.Base+arrOff+i+1]
			}
			if newByte == data[pos] {
				continue
			}
			patches = append(patches, model.Patch{Offset: pos, Old: []byte{data[pos]}, New: []byte{newByte}})
		}
	}
	for i := p.AreaIdx; i < p.Bstep; i++ {
		pos := p.Base + disk.BankVoiceNumOffset + i*disk.VPEntrySize
		if pos+disk.VPEntrySize > len(data) {
			continue
		}
		oldVP := append([]byte(nil), data[pos:pos+disk.VPEntrySize]...)
		newVP := make([]byte, disk.VPEntrySize)
		if i < p.Bstep-1 {
			copy(newVP, data[p.Base+disk.BankVoiceNumOffset+(i+1)*disk.VPEntrySize:])
		}
		if newVP[0] == oldVP[0] && newVP[1] == oldVP[1] {
			continue
		}
		patches = append(patches, model.Patch{Offset: pos, Old: oldVP, New: newVP})
	}
	return patches
}

// DuplicateAreaParams describes appending a clone of an existing area:
// the source voice header SrcHeader is written to the slot at NewOff,
// vp[Bstep] is pointed at NewSlot, and the per-area metadata is copied
// from SrcAreaIdx, all within the bank at byte offset Base.
type DuplicateAreaParams struct {
	Base, NewOff, SrcAreaIdx, Bstep, NewSlot int
	SrcHeader                                []byte
}

// DuplicateAreaPatches returns the patches that append a clone of
// p.SrcAreaIdx at index p.Bstep in the bank at p.Base, and increment bstep.
func DuplicateAreaPatches(data []byte, p DuplicateAreaParams) []model.Patch {
	patches := make([]model.Patch, 0, 3+len(perAreaMetadataOffsets))
	patches = append(patches, cloneVoiceHeaderPatch(data, p.NewOff, p.SrcHeader))
	patches = append(patches, setVPEntryPatch(data, p.Base, p.Bstep, p.NewSlot))
	patches = append(patches, copyPerAreaMetadataPatches(data, p.Base, p.SrcAreaIdx, p.Bstep)...)
	patches = append(patches, bumpBstepPatch(data, p.Base+disk.BankVoiceCountOffset, p.Bstep+1))
	return patches
}

// GrowBanks inserts (targetCount-oldBankCount) empty bank sectors at the
// bank/voice boundary, each seeded with a space-filled name field to match
// the NewUntitled shape, shifting the voice+audio areas later. Returns the
// rebuilt bytes and the number of inserted bytes. The caller validates
// targetCount > oldBankCount and the insertion point.
func GrowBanks(data []byte, oldBankCount, targetCount int) (newData []byte, growBytes int) {
	growSectors := targetCount - oldBankCount
	growBytes = growSectors * disk.SectorSize
	insertAt := oldBankCount * disk.SectorSize
	out := make([]byte, len(data)+growBytes)
	copy(out[0:insertAt], data[0:insertAt])
	for b := 0; b < growSectors; b++ {
		base := insertAt + b*disk.SectorSize
		for i := 0; i < disk.VoiceNameFieldSize; i++ {
			out[base+disk.BankNameOffset+i] = ' '
		}
	}
	copy(out[insertAt+growBytes:], data[insertAt:])
	return out, growBytes
}

// RewriteWavePointers shifts every wave/gen pointer and loop start/end
// address in the voice header hdr by startSamples, preserving the loop-fine
// bits (upper byte of loopst) and the skip flag (MSB of looped). Used when
// an assigned voice's PCM lands at a new absolute offset in the audio area.
func RewriteWavePointers(hdr []byte, startSamples uint32) {
	addToPointer := func(off int) {
		v := binary.LittleEndian.Uint32(hdr[off : off+4])
		binary.LittleEndian.PutUint32(hdr[off:off+4], v+startSamples)
	}
	addToPointer(disk.VoiceWaveStartOffset)
	addToPointer(disk.VoiceWaveEndOffset)
	addToPointer(disk.VoiceGenStartOffset)
	addToPointer(disk.VoiceGenEndOffset)
	for i := 0; i < 8; i++ {
		stOff := disk.VoiceLoopSt0Offset + i*4
		edOff := disk.VoiceLoopEd0Offset + i*4
		rawSt := binary.LittleEndian.Uint32(hdr[stOff : stOff+4])
		rawEd := binary.LittleEndian.Uint32(hdr[edOff : edOff+4])
		stAddr := disk.LoopStartAddress(rawSt) + startSamples
		edAddr := disk.LoopEndAddress(rawEd) + startSamples
		stFlags := rawSt &^ disk.LoopStartAddressMask
		edFlags := rawEd &^ disk.LoopEndAddressMask
		binary.LittleEndian.PutUint32(hdr[stOff:stOff+4], stAddr|stFlags)
		binary.LittleEndian.PutUint32(hdr[edOff:edOff+4], edAddr|edFlags)
	}
}

// BankBstepBumpPatch returns the patch that raises the bank's bstep to
// areaIdx+1 when the area lies beyond the current count, and ok=false when
// no bump is needed or the bank is out of bounds.
func BankBstepBumpPatch(data []byte, bankIdx, areaIdx int) (model.Patch, bool) {
	base := bankIdx * disk.SectorSize
	if base+disk.BankVoiceCountOffset+2 > len(data) {
		return model.Patch{}, false
	}
	cur := int(binary.LittleEndian.Uint16(data[base+disk.BankVoiceCountOffset:]))
	want := areaIdx + 1
	if want <= cur {
		return model.Patch{}, false
	}
	old := make([]byte, 2)
	copy(old, data[base+disk.BankVoiceCountOffset:base+disk.BankVoiceCountOffset+2])
	newBuf := make([]byte, 2)
	binary.LittleEndian.PutUint16(newBuf, uint16(want)) //nolint:gosec // G115: want = areaIdx+1, bounded by disk.MaxVoices (64), fits uint16.
	return model.Patch{Offset: base + disk.BankVoiceCountOffset, Old: old, New: newBuf}, true
}

// DefaultBankRangePatches returns patches that seed an area's key/velocity
// range to the full default span, but only for fields currently zero (so a
// fresh assignment is audible without clobbering user-set ranges).
func DefaultBankRangePatches(data []byte, bankIdx, areaIdx int) []model.Patch {
	base := bankIdx * disk.SectorSize
	patches := []model.Patch{}
	setIfZero := func(off int, value byte) {
		if off >= len(data) || data[off] != 0 {
			return
		}
		patches = append(patches, model.Patch{Offset: off, Old: []byte{0}, New: []byte{value}})
	}
	setIfZero(base+disk.BankKeyLowOffset+areaIdx, 0x00)  // C-1
	setIfZero(base+disk.BankKeyHighOffset+areaIdx, 0x7F) // G9
	setIfZero(base+disk.BankVelLowOffset+areaIdx, 0x01)
	setIfZero(base+disk.BankVelHighOffset+areaIdx, 0x7F)
	return patches
}

// IsBareSingleVoice reports whether the container holds exactly one voice
// and no bank metadata that a single FZV file cannot represent (no bank
// names). This is the only state in which a wrapped single-voice .img can
// be saved back faithfully as a bare FZV; any richer content (a second
// voice, or a bank name) must be promoted to a full dump on save so it
// isn't dropped (UXF / UXD).
func IsBareSingleVoice(data []byte, bankCount int) bool {
	// maxReferencedSlot: -1 = no voices, 0 = exactly one (slot 0),
	// >0 = multiple. Only the single-voice case can round-trip as an FZV.
	if maxReferencedSlot(data, bankCount) != 0 {
		return false
	}
	for b := 0; b < bankCount; b++ {
		base := b * disk.SectorSize
		off := base + disk.BankNameOffset
		if off+disk.VoiceNameFieldSize > len(data) {
			continue
		}
		for i := 0; i < disk.VoiceNameFieldSize; i++ {
			if c := data[off+i]; c != ' ' && c != 0 {
				return false
			}
		}
	}
	return true
}

// maxReferencedSlot returns the highest voice-slot index referenced by
// any bank's vp[] table, or -1 when no bank references any slot.
func maxReferencedSlot(data []byte, bankCount int) int {
	maxLive := -1
	for b := 0; b < bankCount; b++ {
		base := b * disk.SectorSize
		bstepOff := base + disk.BankVoiceCountOffset
		if bstepOff+2 > len(data) {
			break
		}
		bstep := int(binary.LittleEndian.Uint16(data[bstepOff : bstepOff+2]))
		for i := 0; i < bstep; i++ {
			vpOff := base + disk.BankVoiceNumOffset + i*disk.VPEntrySize
			if vpOff+disk.VPEntrySize > len(data) {
				break
			}
			if vp := int(binary.LittleEndian.Uint16(data[vpOff : vpOff+disk.VPEntrySize])); vp > maxLive {
				maxLive = vp
			}
		}
	}
	return maxLive
}

// maxLiveSample returns the highest sample address referenced by the
// wave-end, gen-end, and loop-end pointers of voice slots 0..maxLive,
// or -1 when maxLive < 0.
func maxLiveSample(data []byte, voiceAreaStart, maxLive int) int64 {
	maxSample := int64(-1)
	for slot := 0; slot <= maxLive; slot++ {
		off := disk.VoiceSlotOffset(voiceAreaStart, slot)
		if off+disk.VoiceHeaderUsed > len(data) {
			break
		}
		for _, fieldOff := range []int{disk.VoiceWaveEndOffset, disk.VoiceGenEndOffset} {
			if p := int64(binary.LittleEndian.Uint32(data[off+fieldOff:])); p > maxSample {
				maxSample = p
			}
		}
		for i := 0; i < 8; i++ {
			rawEd := binary.LittleEndian.Uint32(data[off+disk.VoiceLoopEd0Offset+i*4:])
			if addr := int64(disk.LoopEndAddress(rawEd)); addr > maxSample {
				maxSample = addr
			}
		}
	}
	return maxSample
}
