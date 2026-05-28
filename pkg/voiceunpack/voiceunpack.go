// Package voiceunpack implements the fizzle voice unpack command. It extracts
// individual FZV voice files from an FZF full data dump.
//
// Hardware abstraction: the on-device DIR / COPY workflow.
//
// On the sampler, recovering individual voices from a full dump means loading
// the dump, navigating the DIR menu, selecting each voice by name, and using
// the COPY function to write it back out as a separate file, once per voice.
// voiceunpack.Unpack does the same thing in one operation: it parses the
// bank sector (or sectors, for multi-bank dumps), decodes each voice header
// in the voice area, slices its audio block out of the audio area, and writes
// one .fzv per voice with the audio offsets rewritten as if the voice had
// always been alone.
//
// Multi-disk dumps are handled by UnpackMultiDisk, which stitches disk 2's
// audio continuation onto disk 1 before slicing.
package voiceunpack

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/fileutil"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
)

// Unpack reads the FZF full dump at fzfPath and writes one FZV file per voice
// into outputDir. Output files are named after the voice name embedded in
// each voice header, e.g. "HOOVER.fzv". If two voices share the same name a
// numeric suffix is appended. outputDir is created if it does not exist.
func Unpack(fzfPath, outputDir string) error {
	data, hdr, err := fzutil.ReadFZF(fzfPath)
	if err != nil {
		return fmt.Errorf("voiceunpack: %w", err)
	}

	voices, _, err := unpack(data, hdr)
	if err != nil {
		return err
	}

	return writeVoices(outputDir, voices)
}

// UnpackData reads the FZF full dump at fzfPath and returns one FZV byte
// slice per voice paired with that voice's file-level slot index. The slot
// index is the position in the voice area (spec §2-1's `vp[]` target),
// which is what bank metadata is keyed on. NoSound placeholder slots are
// compacted out of the returned slice; the parallel slotIndices slice tells
// the caller which slot each emitted FZV belongs to so per-voice bank
// lookups stay aligned.
//
// Returning slot indices alongside the bytes is a load-bearing change: the
// previous signature dropped silent placeholder slots and left callers to
// index bank arrays by the *compacted* position, which silently mismapped
// names, key ranges, and outputs when a dump started with NoSound slots
// (see CASIO139.FZF / sfzexport's F6).
func UnpackData(fzfPath string) ([][]byte, []int, error) {
	data, hdr, err := fzutil.ReadFZF(fzfPath)
	if err != nil {
		return nil, nil, fmt.Errorf("voiceunpack: %w", err)
	}
	return unpack(data, hdr)
}

// UnpackBank reads the FZF full dump at fzfPath and writes only the voices
// referenced by the bank at bankIdx's `vp[]` array (0-based). Multi-bank
// dumps store one bank sector per bank, and each bank's vp[] maps its
// key-split positions to voice-slot indices; those mappings can repeat
// (one voice covering several splits) and the slot ranges across banks
// overlap freely, so the legal voices for bankIdx are vp[0..bstep-1], not
// a sequential slice of the unpacked array.
//
// Duplicates in vp[] surface as a single emitted voice; the on-hardware
// key-split sharing is informational only at the file-extraction level.
func UnpackBank(fzfPath, outputDir string, bankIdx int) error {
	data, hdr, err := fzutil.ReadFZF(fzfPath)
	if err != nil {
		return fmt.Errorf("voiceunpack: %w", err)
	}

	nBanks := fzutil.CountBankSectors(data)
	if bankIdx < 0 || bankIdx >= nBanks {
		return fmt.Errorf("voiceunpack: bank index %d out of range [0, %d)", bankIdx, nBanks)
	}

	bankOff := bankIdx * disk.SectorSize
	bstep := int(binary.LittleEndian.Uint16(data[bankOff+disk.BankVoiceCountOffset : bankOff+disk.BankVoiceCountOffset+2]))
	if bstep > disk.MaxVoices {
		bstep = disk.MaxVoices
	}

	allVoices, slotIndices, err := unpack(data, hdr)
	if err != nil {
		return err
	}

	// Build slot -> emitted voice index map so we can pick by vp[].
	slotToVoice := make(map[int]int, len(slotIndices))
	for i, s := range slotIndices {
		slotToVoice[s] = i
	}

	seenSlots := make(map[int]bool, bstep)
	wanted := make([][]byte, 0, bstep)
	for s := 0; s < bstep; s++ {
		vpOff := bankOff + disk.BankVoiceNumOffset + 2*s
		if vpOff+2 > len(data) {
			break
		}
		vp := int(binary.LittleEndian.Uint16(data[vpOff : vpOff+2]))
		if seenSlots[vp] {
			// Same voice referenced by several key splits in this bank.
			// Emit once; key-split sharing is preserved in the FZF, not at
			// the FZV extraction level.
			continue
		}
		seenSlots[vp] = true
		if idx, ok := slotToVoice[vp]; ok {
			wanted = append(wanted, allVoices[idx])
		}
	}

	if len(wanted) == 0 {
		return fmt.Errorf("voiceunpack: bank %d references no extractable voices (bstep=%d)", bankIdx, bstep)
	}

	return writeVoices(outputDir, wanted)
}

// UnpackMultiDisk extracts voices from a 2-disk full dump. It reads voice
// headers from disk 1 and concatenates audio from both disks so that all
// voices (including those whose audio is on disk 2) are extracted with
// complete audio data.
func UnpackMultiDisk(disk1ImgPath, disk2ImgPath, outputDir string) error {
	tmpDir, err := os.MkdirTemp("", "voiceunpack-multi-*")
	if err != nil {
		return fmt.Errorf("voiceunpack: %w", err)
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck

	d1FZF := filepath.Join(tmpDir, "d1.fzf")
	if err := diskget.Get(disk1ImgPath, disk.FullDumpName, d1FZF); err != nil {
		return fmt.Errorf("voiceunpack: extracting FZF from disk 1: %w", err)
	}
	d1Data, hdr, err := fzutil.ReadFZF(d1FZF)
	if err != nil {
		return fmt.Errorf("voiceunpack: %w", err)
	}

	d2FZF := filepath.Join(tmpDir, "d2.dat")
	if err := diskget.Get(disk2ImgPath, disk.FullDumpName, d2FZF); err != nil {
		return fmt.Errorf("voiceunpack: extracting audio from disk 2: %w", err)
	}
	d2Data, err := os.ReadFile(d2FZF)
	if err != nil {
		return fmt.Errorf("voiceunpack: reading disk 2 data: %w", err)
	}

	voiceSectors := disk.VoiceAreaSectors(hdr.NVoice)
	voiceAreaEnd := hdr.VoiceAreaStart + voiceSectors*disk.SectorSize
	if len(d1Data) < voiceAreaEnd {
		return fmt.Errorf("voiceunpack: disk 1 FZF too small for voice area")
	}

	combined := make([]byte, len(d1Data)+len(d2Data))
	copy(combined, d1Data[:voiceAreaEnd])
	copy(combined[voiceAreaEnd:], d1Data[voiceAreaEnd:])
	copy(combined[len(d1Data):], d2Data)

	voices, _, err := unpack(combined, hdr)
	if err != nil {
		return err
	}

	return writeVoices(outputDir, voices)
}

func writeVoices(outputDir string, voices [][]byte) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("voiceunpack: creating output directory: %w", err)
	}

	seen := make(map[string]int)
	for i, v := range voices {
		name := sanitizeFilename(voiceName(v))
		count := seen[name]
		seen[name]++

		var filename string
		if count == 0 {
			filename = name + ".fzv"
		} else {
			filename = fmt.Sprintf("%s-%d.fzv", name, count)
		}

		outPath := filepath.Join(outputDir, filename)
		if err := fileutil.WriteAtomic(outPath, v); err != nil {
			return fmt.Errorf("voiceunpack: writing voice %d: %w", i, err)
		}
		log.Info().
			Str("file", filename).
			Str("progress", fmt.Sprintf("%d/%d", i+1, len(voices))).
			Msg("extracted voice")
	}

	return nil
}

// unpack splits a raw FZF byte slice into individual FZV byte slices,
// returning each FZV alongside the voice-slot index it came from. NoSound
// placeholder slots and invalid headers are filtered out of the FZV slice;
// the parallel slot-index slice lets callers reattach bank metadata (which
// is keyed on slot, not on emit position) without re-walking the voice
// area.
func unpack(data []byte, hdr *fzutil.FZFHeader) ([][]byte, []int, error) {
	nvoice := hdr.NVoice
	voiceAreaStart := hdr.VoiceAreaStart

	voiceSectors := disk.VoiceAreaSectors(nvoice)
	voiceAreaSize := voiceSectors * disk.SectorSize
	voiceAreaEnd := voiceAreaStart + voiceAreaSize

	if len(data) < voiceAreaEnd {
		return nil, nil, fmt.Errorf("voiceunpack: FZF too small for %d voices (need %d bytes, have %d)",
			nvoice, voiceAreaEnd, len(data))
	}

	voiceArea := data[voiceAreaStart:voiceAreaEnd]
	audioArea := data[voiceAreaEnd:]

	// Compute per-voice audio sizes by finding each voice's sample range.
	// The audio area is the concatenation of all voice audio blocks. We
	// determine each voice's block by looking at the waved field and the
	// cumulative offset of preceding voices.
	//
	// Because the pointer fields in the packed voice headers are relative to
	// the combined audio area we need to derive block boundaries from them.
	// The simplest approach: for each voice, waved gives the end sample
	// address relative to the combined area. The start of voice i's audio is
	// the end of voice i-1's audio block (aligned to a sector boundary).

	type voiceInfo struct {
		hdr        []byte // 192-byte packed header
		audioStart int    // byte offset into audioArea
		audioEnd   int    // byte offset into audioArea (exclusive)
	}

	infos := make([]voiceInfo, nvoice)
	for i := range nvoice {
		off := disk.VoiceSlotOffset(0, i)
		if off+disk.VoiceHeaderUsed > len(voiceArea) {
			return nil, nil, fmt.Errorf("voiceunpack: voice %d header at offset %d extends beyond voice area", i, off)
		}
		voiceHdr := make([]byte, disk.VoiceHeaderUsed)
		copy(voiceHdr, voiceArea[off:off+disk.VoiceHeaderUsed])
		infos[i].hdr = voiceHdr
	}

	// Derive per-voice audio block boundaries using waveStart and waveEnd
	// directly from each voice header. waveStart is the absolute sample
	// address of this voice's audio in the combined area, exactly as written
	// by voicebuild. waveEnd is the absolute sample end.
	//
	// We cannot reconstruct block boundaries from waveEnd deltas because
	// voicebuild pads blocks to sector boundaries but records unpadded
	// waveEnd values, so the deltas don't equal the padded block sizes.
	for i := range infos {
		waveStart := int(binary.LittleEndian.Uint32(infos[i].hdr[disk.VoiceWaveStartOffset:disk.VoiceWaveEndOffset]))
		waveEnd := int(binary.LittleEndian.Uint32(infos[i].hdr[disk.VoiceWaveEndOffset:disk.VoiceGenStartOffset]))
		voiceSamples := waveEnd - waveStart
		if voiceSamples < 0 {
			voiceSamples = 0
		}
		byteStart := waveStart * disk.BytesPerSample
		// Zero-sample voices (waveEnd <= waveStart, e.g. NoSound placeholders
		// or voices whose audio was wiped) own no audio bytes. Forcing a
		// non-zero byteSize here would slice into the *next* voice's audio
		// block, then re-packing would write that foreign audio back into
		// the silent slot. Produce a header-only FZV in that case so the
		// round-trip preserves silence; voicebuild.Build skips the audio
		// copy when len(v) == SectorSize.
		byteSize := disk.PadToSector(voiceSamples * disk.BytesPerSample)
		byteEnd := byteStart + byteSize
		if byteEnd > len(audioArea) {
			byteEnd = len(audioArea)
		}
		infos[i].audioStart = byteStart
		infos[i].audioEnd = byteEnd
	}

	voices := make([][]byte, 0, nvoice)
	slotIndices := make([]int, 0, nvoice)
	for slotIdx, info := range infos {
		// Skip slots that aren't real voices: PlaybackModeNoSound placeholders
		// (the spec allows them; see CASIO139.FZF) or garbage byte patterns
		// that survived earlier-stage validation by accident.
		if !disk.IsPlausibleVoiceSlot(info.hdr) {
			continue
		}
		// Multi-disk continuation: a plausible voice header whose audio
		// extends past the local audio area must have its audio on disk 2.
		// All later voices in the bank order will also be past the boundary,
		// so stop iterating here.
		if info.audioStart >= len(audioArea) {
			break
		}
		audioBlock := audioArea[info.audioStart:info.audioEnd]

		// Build a full 1024-byte voice header sector from the 192-byte packed
		// header, then adjust sample pointer fields to be relative to this
		// voice's own audio block rather than the combined area.
		hdr := make([]byte, disk.SectorSize)
		copy(hdr, info.hdr)
		// The name field requires 2 null terminator bytes after the 12-byte display name.
		hdr[disk.VoiceNameOffset+disk.LabelSize] = 0
		hdr[disk.VoiceNameOffset+disk.LabelSize+1] = 0
		// Use the waveStart pointer from the header as the sample offset.
		// it is the cumulative sample address at the start of this voice's
		// audio in the combined area, which is what all other pointers are
		// relative to. audioStart/2 diverges from waveStart because audio
		// blocks are padded to sector boundaries but waveStart is not.
		waveStart := int(binary.LittleEndian.Uint32(info.hdr[disk.VoiceWaveStartOffset:disk.VoiceWaveEndOffset]))
		subtractSampleOffsets(hdr, waveStart)

		fzv := make([]byte, disk.SectorSize+len(audioBlock))
		copy(fzv, hdr)
		copy(fzv[disk.SectorSize:], audioBlock)
		voices = append(voices, fzv)
		slotIndices = append(slotIndices, slotIdx)
	}

	return voices, slotIndices, nil
}

// subtractSampleOffsets adjusts the sample pointer fields in a voice header
// so they are relative to the voice's own audio block rather than the combined
// wave area. offsetSamples is the number of samples preceding this voice.
//
// Loop-pointer fields reserve flag bits the address adjustment must not
// disturb (spec §2-1: loopst[i] upper 8 bits = loop-fine, looped[i] MSB =
// skip flag). For those fields the address bits are masked off the raw
// 32-bit value before comparison and subtraction, and the preserved flag
// bits are OR'd back in before writing. Comparing the raw 32-bit value
// would misbehave for looped[i] with the skip flag set, since the MSB
// would make the value larger than any plausible offset.
func subtractSampleOffsets(voice []byte, offsetSamples int) {
	off := bitconv.NarrowU32(offsetSamples)
	disk.ForEachSamplePointer(voice, func(field []byte, kind disk.SamplePointerKind) {
		v := binary.LittleEndian.Uint32(field)
		switch kind {
		case disk.WavePointer:
			if off <= v {
				binary.LittleEndian.PutUint32(field, v-off)
			}
		case disk.LoopStartPointer:
			addr := disk.LoopStartAddress(v)
			fine := v & ^uint32(disk.LoopStartAddressMask)
			if off <= addr {
				addr -= off
			}
			binary.LittleEndian.PutUint32(field, (addr&disk.LoopStartAddressMask)|fine)
		case disk.LoopEndPointer:
			addr := disk.LoopEndAddress(v)
			skip := v & disk.LoopEndSkipMask
			if off <= addr {
				addr -= off
			}
			binary.LittleEndian.PutUint32(field, (addr&disk.LoopEndAddressMask)|skip)
		}
	})
}

// sanitizeFilename replaces path separators in a voice name with underscores
// so the result can be safely used as a single filename component. Real-world
// voice names from FZF dumps sometimes contain '/' (e.g. "BRASS/BASS 2"),
// which filepath.Join would otherwise interpret as a directory separator,
// silently writing voices into subdirectories. The voice name embedded in
// the FZV header bytes is left untouched; only the on-disk filename derived
// from it is sanitized. Dedup collision counting is done on the sanitized
// form so that two voices named "A/B" and "A_B" still produce distinct
// filenames.
//
// Names consisting entirely of dots (e.g. ".", ".." or "....") are rejected
// and fall back to disk.DefaultVoiceName. The `.fzv` suffix appended by the
// caller already prevents a bare `..` from escaping outputDir (it becomes
// `...fzv`), but rejecting dot-only names removes the accidental defense.
//
// Unlike sfzexport.sanitizeFilename, which uses a strict allowlist because
// it builds SFZ "sample=" references that downstream parsers split on
// whitespace, this sanitizer preserves the voice name as faithfully as
// possible. Path separators, Windows-illegal characters (* ? < > | " :),
// and dot-only names are normalized; everything else passes through.
func sanitizeFilename(name string) string {
	if isDotOnly(name) {
		return disk.DefaultVoiceName
	}
	r := strings.NewReplacer(
		"/", "_", "\\", "_",
		"*", "_", "?", "_", "<", "_", ">", "_",
		"|", "_", "\"", "_", ":", "_",
	)
	return r.Replace(name)
}

// isDotOnly reports whether s is non-empty and consists entirely of '.'
// characters. Such names ("." / ".." / "...") are path-traversal hazards
// when used as filename stems.
func isDotOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != '.' {
			return false
		}
	}
	return true
}

// voiceName returns the trimmed voice name from a voice header.
func voiceName(fzv []byte) string {
	if len(fzv) < disk.VoiceNameOffset+disk.LabelSize {
		return disk.DefaultVoiceName
	}
	b := fzv[disk.VoiceNameOffset : disk.VoiceNameOffset+disk.LabelSize]
	if !disk.IsPrintableName(b) {
		return disk.DefaultVoiceName
	}
	name := disk.TrimPadded(b)
	if name == "" {
		return disk.DefaultVoiceName
	}
	return name
}
