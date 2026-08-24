// Package voiceunpack implements the fizzle voice unpack command. It extracts
// individual FZV voice files from an FZF full data dump.
//
// It stands in for the on-device DIR / COPY workflow: on the sampler,
// recovering voices from a full dump means loading the dump, then picking
// each voice in the DIR menu and COPYing it back out, once per voice.
// Unpack parses the bank sector (or sectors, for multi-bank dumps), decodes
// each voice header in the voice area, slices its audio block out of the
// audio area, and writes one .fzv per voice with the audio offsets rewritten
// as if the voice had always been alone.
//
// UnpackMultiDisk handles multi-disk dumps, stitching disk 2's audio
// continuation onto disk 1 before slicing.
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
	"github.com/philipcunningham/fizzle/pkg/fzf"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
)

// Unpack reads the FZF full dump at fzfPath and writes one FZV file per voice
// into outputDir. Output files are named after the voice name embedded in
// each voice header, e.g. "HOOVER.fzv". If two voices share the same name a
// numeric suffix is appended. outputDir is created if it does not exist.
func Unpack(fzfPath, outputDir string) error {
	doc, err := readStandalone(fzfPath)
	if err != nil {
		return fmt.Errorf("voiceunpack: %w", err)
	}

	voices, _, err := unpack(doc.Bytes(), doc.Layout())
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
// The slot indices are load-bearing: indexing bank arrays by the compacted
// position instead silently mismaps names, key ranges, and outputs whenever
// a dump opens with NoSound slots, as CASIO139.FZF does. sfzexport
// tracks the export side of the same defect as F6.
func UnpackData(fzfPath string) ([][]byte, []int, error) {
	doc, err := readStandalone(fzfPath)
	if err != nil {
		return nil, nil, fmt.Errorf("voiceunpack: %w", err)
	}
	return unpack(doc.Bytes(), doc.Layout())
}

// UnpackBank reads the FZF full dump at fzfPath and writes only the voices
// the bank at bankIdx references through its `vp[]` array (0-based).
// Multi-bank dumps store one bank sector per bank, and each vp[] maps that
// bank's key-split positions to voice-slot indices. Those mappings can repeat
// (one voice covering several splits) and slot ranges can overlap across
// banks, so the legal voices for bankIdx are vp[0..bstep-1], not a sequential
// slice of the unpacked array.
//
// Duplicates in vp[] emit a single voice; key-split sharing lives in the
// FZF, not at the file-extraction level.
func UnpackBank(fzfPath, outputDir string, bankIdx int) error {
	doc, err := readStandalone(fzfPath)
	if err != nil {
		return fmt.Errorf("voiceunpack: %w", err)
	}
	data := doc.Bytes()

	nBanks := doc.Layout().BankCount()
	if bankIdx < 0 || bankIdx >= nBanks {
		return fmt.Errorf("voiceunpack: bank index %d out of range [0, %d)", bankIdx, nBanks)
	}

	bankOff := bankIdx * disk.SectorSize
	bstep := int(binary.LittleEndian.Uint16(data[bankOff+disk.BankVoiceCountOffset : bankOff+disk.BankVoiceCountOffset+2]))
	if bstep > disk.MaxVoices {
		bstep = disk.MaxVoices
	}

	allVoices, slotIndices, err := unpack(data, doc.Layout())
	if err != nil {
		return err
	}

	// Map each slot to its emitted voice index so vp[] can pick from it.
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
	doc, err := readStandalone(d1FZF)
	if err != nil {
		return fmt.Errorf("voiceunpack: %w", err)
	}
	d1Data := doc.Bytes()

	d2FZF := filepath.Join(tmpDir, "d2.dat")
	if err := diskget.Get(disk2ImgPath, disk.FullDumpName, d2FZF); err != nil {
		return fmt.Errorf("voiceunpack: extracting audio from disk 2: %w", err)
	}
	d2Data, err := os.ReadFile(d2FZF)
	if err != nil {
		return fmt.Errorf("voiceunpack: reading disk 2 data: %w", err)
	}

	voiceAreaEnd := doc.Layout().AudioStart()
	if len(d1Data) < voiceAreaEnd {
		return fmt.Errorf("voiceunpack: disk 1 FZF too small for voice area")
	}

	combined := make([]byte, len(d1Data)+len(d2Data))
	copy(combined, d1Data[:voiceAreaEnd])
	copy(combined[voiceAreaEnd:], d1Data[voiceAreaEnd:])
	copy(combined[len(d1Data):], d2Data)

	voices, _, err := unpack(combined, doc.Layout())
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
func unpack(data []byte, layout fzutil.FZFLayout) ([][]byte, []int, error) {
	return unpackAt(data, layout.VoiceCount(), layout.VoiceStart(), layout.AudioStart())
}

func unpackAt(data []byte, nvoice, voiceAreaStart, voiceAreaEnd int) ([][]byte, []int, error) {
	if len(data) < voiceAreaEnd {
		return nil, nil, fmt.Errorf("voiceunpack: FZF too small for %d voices (need %d bytes, have %d)",
			nvoice, voiceAreaEnd, len(data))
	}

	voiceArea := data[voiceAreaStart:voiceAreaEnd]
	audioArea := data[voiceAreaEnd:]

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

	// The audio area is the concatenation of the per-voice blocks. Derive each
	// voice's boundaries from its own waveStart and waveEnd, the absolute
	// sample addresses in the combined area exactly as voicebuild wrote them.
	// waveEnd deltas don't work: voicebuild pads blocks to sector boundaries
	// but records unpadded waveEnd values, so a delta isn't the block size.
	for i := range infos {
		waveStart := int(binary.LittleEndian.Uint32(infos[i].hdr[disk.VoiceWaveStartOffset:disk.VoiceWaveEndOffset]))
		waveEnd := int(binary.LittleEndian.Uint32(infos[i].hdr[disk.VoiceWaveEndOffset:disk.VoiceGenStartOffset]))
		voiceSamples := waveEnd - waveStart
		if voiceSamples < 0 {
			voiceSamples = 0
		}
		byteStart := waveStart * disk.BytesPerSample
		// Zero-sample voices (waveEnd <= waveStart: NoSound placeholders, or
		// voices whose audio was wiped) own no audio bytes. Forcing a
		// non-zero byteSize slices into the next voice's block, and
		// re-packing then writes that foreign audio into the silent slot.
		// A header-only FZV preserves silence across the round trip;
		// voicebuild.Build skips the audio copy when len(v) == SectorSize.
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
		// (the spec allows them, and CASIO139.FZF carries them) or garbage
		// byte patterns that survived earlier validation by accident.
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

		// Grow the 192-byte packed header into a full 1024-byte sector, then
		// rebase its sample pointers onto this voice's own audio block.
		hdr := make([]byte, disk.SectorSize)
		copy(hdr, info.hdr)
		// The name field needs 2 null terminator bytes after the 12-byte name.
		hdr[disk.VoiceNameOffset+disk.LabelSize] = 0
		hdr[disk.VoiceNameOffset+disk.LabelSize+1] = 0
		// waveStart is the cumulative sample address every other pointer is
		// relative to, so it's the offset to subtract. audioStart/2 diverges
		// from it: blocks are padded to sector boundaries, waveStart is not.
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

func readStandalone(path string) (*fzf.Document, error) {
	data, err := fzutil.ReadBounded(path, fzutil.MaxReadSize)
	if err != nil {
		return nil, err
	}
	return fzf.NewStandalone(data)
}

// subtractSampleOffsets adjusts the sample pointer fields in a voice header
// so they are relative to the voice's own audio block rather than the combined
// wave area. offsetSamples is the number of samples preceding this voice.
//
// Loop-pointer fields reserve flag bits the address adjustment must not
// disturb (spec §2-1: loopst[i] upper 8 bits = loop-fine, looped[i] MSB =
// skip flag). For those fields, mask the address bits out of the raw 32-bit
// value before comparing and subtracting, then OR the flag bits back in.
// Comparing the raw value misbehaves for a looped[i] with the skip flag
// set: the MSB makes it larger than any plausible offset.
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

// sanitizeFilename makes a voice name safe as one filename component. Real
// FZF dumps carry names with '/' ("BRASS/BASS 2"), which filepath.Join reads
// as a directory separator, silently writing voices into subdirectories.
// Only the on-disk filename is sanitized; the name in the FZV header bytes
// is untouched. Dedup counting runs on the sanitized form, so "A/B" and
// "A_B" still produce distinct filenames.
//
// Dot-only names (".", "..", "....") fall back to disk.DefaultVoiceName. The
// `.fzv` suffix the caller appends already stops a bare `..` from escaping
// outputDir (it becomes `...fzv`), so rejecting them makes the defence
// deliberate rather than accidental.
//
// Unlike sfzexport.sanitizeFilename, which needs a strict allowlist because
// its output lands in SFZ "sample=" references that parsers split on
// whitespace, this one stays faithful to the voice name: path separators,
// Windows-illegal characters (* ? < > | " :), and dot-only names are
// normalized, and everything else passes through.
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
