// Package fzutil provides FZ series format utilities shared across packages.
package fzutil

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/internal/limits"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

// MaxReadSize is the maximum file size accepted when reading files.
const MaxReadSize = limits.MaxRead

// DefaultRate is the highest quality sample rate supported by the FZ series
// samplers. It is used as the default for import and conversion commands.
const DefaultRate = 36000

// ReadBounded reads a file at path, returning an error if it exceeds maxSize bytes.
func ReadBounded(path string, maxSize int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	lr := &io.LimitedReader{R: f, N: maxSize + 1}
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxSize {
		return nil, fmt.Errorf("fzutil: file exceeds maximum size of %d bytes", maxSize)
	}
	return data, nil
}

// ReadWAV opens and decodes a WAV file at path.
func ReadWAV(path string) (*wav.File, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("fzutil: opening WAV %q: %w", path, err)
	}
	defer fh.Close() //nolint:errcheck
	f, err := wav.Read(fh)
	if err != nil {
		return nil, fmt.Errorf("fzutil: reading WAV %q: %w", path, err)
	}
	return f, nil
}

// Refusals callers distinguish: a source rate too low to resample
// from, and audio longer than the sampler's memory holds.
var (
	ErrSourceRateTooLow = errors.New("fzutil: source sample rate below minimum")
	ErrTooLong          = errors.New("fzutil: resampled length exceeds the sampler's memory")
)

// MinSampleRate is the lowest source rate Resample accepts. The WAV spec
// sets no floor, and real audio never falls below 1 kHz. Without the bound
// a tiny SampleRate blows the output up to gigabytes on the way to an FZ
// rate (DefaultRate is 36 kHz).
const MinSampleRate = 1000

// MaxResampleOut caps the resampled output length at the FZ-1's total sample
// memory in samples. Any conversion that would exceed this cannot fit on the
// target hardware, so we refuse rather than allocate the buffer.
const MaxResampleOut = disk.MaxSampleRAM / disk.BytesPerSample

// ResampledLen returns the sample count Resample produces for frames input
// samples converted from src to dst, applying the same guards. Callers
// estimating a conversion's size share it with the conversion, so the two
// can never disagree.
func ResampledLen(frames int, src, dst uint32) (int, error) {
	if frames <= 0 {
		return 0, errors.New("fzutil: WAV contains no samples")
	}
	if src == 0 {
		return 0, errors.New("fzutil: WAV has zero sample rate")
	}
	if src < MinSampleRate {
		return 0, fmt.Errorf("%w: %d Hz is below minimum %d Hz", ErrSourceRateTooLow, src, MinSampleRate)
	}
	if src == dst {
		return frames, nil
	}
	// Compute the length in float64 so a pathological input can't overflow
	// an int, then bounds-check before anything allocates.
	outLenF := math.Round(float64(frames) * float64(dst) / float64(src))
	if outLenF > float64(MaxResampleOut) {
		return 0, fmt.Errorf("%w: %.0f exceeds maximum %d samples", ErrTooLong, outLenF, MaxResampleOut)
	}
	outLen := int(outLenF)
	if outLen < 1 {
		outLen = 1
	}
	return outLen, nil
}

// Resample converts f.Samples to targetRate using linear interpolation.
// Returns a copy when the rates are equal.
func Resample(f *wav.File, targetRate uint32) ([]int16, error) {
	outLen, err := ResampledLen(len(f.Samples), f.SampleRate, targetRate)
	if err != nil {
		return nil, err
	}
	if f.SampleRate == targetRate {
		out := make([]int16, len(f.Samples))
		copy(out, f.Samples)
		return out, nil
	}
	out := make([]int16, outLen)
	src := f.Samples
	srcLen := len(src)
	srcRate := int64(f.SampleRate)
	dstRate := int64(targetRate)
	for i := range outLen {
		// Fixed-point position in the source: (i * srcRate) / dstRate, in
		// int64 to avoid platform-dependent floating point rounding.
		num := int64(i) * srcRate
		lo := int(num / dstRate)
		if lo >= srcLen {
			lo = srcLen - 1
		}
		rem := num % dstRate
		hi := lo + 1
		if hi >= srcLen {
			hi = srcLen - 1
		}
		// Linear interpolation: src[lo] + (src[hi]-src[lo]) * rem / dstRate,
		// with the multiply in int64 so int16 * int64 can't overflow.
		a := int64(src[lo])
		b := int64(src[hi])
		v := a + (b-a)*rem/dstRate
		out[i] = int16(v) //nolint:gosec // G115: value clamped to int16 range on preceding lines
	}
	return out, nil
}

// VoiceName derives a normalised FZ voice name from a file path stem.
// It uppercases, replaces runs of non-alphanumeric characters with a space,
// and truncates to 12 characters, the FZ series display limit.
func VoiceName(path string) string {
	stem := filepath.Base(path)
	stem = strings.TrimSuffix(stem, filepath.Ext(stem))
	stem = strings.ToUpper(stem)
	var b strings.Builder
	prevSpace := false
	for _, r := range stem {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace && b.Len() > 0 {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	name := strings.TrimRight(b.String(), " ")
	if len(name) > disk.LabelSize {
		name = strings.TrimRight(name[:disk.LabelSize], " ")
	}
	if name == "" {
		name = disk.DefaultVoiceName
	}
	return name
}

// ReadFZV reads an FZV voice file at path, enforcing a minimum size of one
// sector (1024 bytes) to ensure the voice header is present.
func ReadFZV(path string) ([]byte, error) {
	data, err := ReadBounded(path, MaxReadSize)
	if err != nil {
		return nil, fmt.Errorf("fzutil: reading FZV %q: %w", path, err)
	}
	if len(data) < disk.SectorSize {
		return nil, fmt.Errorf("fzutil: FZV %q too small (%d bytes, need at least %d)", path, len(data), disk.SectorSize)
	}
	return data, nil
}

// FZFHeader holds the parsed header metadata from an FZF full dump file.
//
// NVoice is the inferred file-level voice count (the spec's `vn`) obtained
// by walking the voice area. BStep0 is the raw bstep value from bank 0 of
// the file (which counts key splits in that bank, not voices); the two
// agree on most files but diverge on factory drum kits where several key
// splits share a single voice via vp[]. Callers that need the spec
// definition of "voice area size" should use NVoice. fzfinfo, fzfmidi, and
// voiceunpack share this header parse.
type FZFHeader struct {
	NVoice         int
	BStep0         int
	NBankSectors   int
	VoiceAreaStart int
}

// ParseFZFHeader reads and validates the header of an FZF full dump.
//
// The voice count is inferred by walking the voice area, not trusted from
// the bank sector's bstep. bstep is per-bank and counts key splits, several
// of which vp[] can map onto one voice. The spec sizes the voice area from
// a file-level `vn` in the dBP file head, and a standalone FZF pulled out
// by `disk get` has lost the dBP. See llm-wiki/topics/voice-area-sizing.md
// for the full rationale. bstep stays available as BStep0 for diagnostics.
func ParseFZFHeader(data []byte) (*FZFHeader, error) {
	if len(data) < disk.SectorSize {
		return nil, fmt.Errorf("fzutil: FZF too small (%d bytes, need at least %d)", len(data), disk.SectorSize)
	}
	bstep := int(binary.LittleEndian.Uint16(data[disk.BankVoiceCountOffset : disk.BankVoiceCountOffset+2]))
	if bstep == 0 || bstep > disk.MaxVoices {
		return nil, fmt.Errorf("fzutil: invalid bstep %d (if this is a multi-disk continuation disk, run fzf info on disk 1 instead)", bstep)
	}
	nBankSectors := countBankSectors(data)
	voiceAreaStart := nBankSectors * disk.SectorSize
	// Bank 0's bstep doesn't cover voices in banks 1..N. countAllVoices sums
	// every bank's bstep for the walk bound, so nvoice spans the whole
	// voice area.
	nvoice := countAllVoices(data)
	if nvoice == 0 {
		return nil, fmt.Errorf("fzutil: no valid voice headers found in voice area")
	}
	return &FZFHeader{
		NVoice:         nvoice,
		BStep0:         bstep,
		NBankSectors:   nBankSectors,
		VoiceAreaStart: voiceAreaStart,
	}, nil
}

// parseFZFHeaderWithVoiceCount parses under the DIS tail's voice
// count, validating the slots it claims. Zeroed bytes read as
// placeholder slots, so the effective ceiling is geometry: a count
// whose voice area runs past the data errors.
// See llm-wiki/topics/voice-area-sizing.md.
func parseFZFHeaderWithVoiceCount(data []byte, vn int) (*FZFHeader, error) {
	if len(data) < disk.SectorSize {
		return nil, fmt.Errorf("fzutil: FZF too small (%d bytes, need at least %d)", len(data), disk.SectorSize)
	}
	if vn < 1 || vn > disk.MaxVoices {
		return nil, fmt.Errorf("fzutil: DIS voice count %d outside 1..%d", vn, disk.MaxVoices)
	}
	bstep := int(binary.LittleEndian.Uint16(data[disk.BankVoiceCountOffset : disk.BankVoiceCountOffset+2]))
	if bstep == 0 || bstep > disk.MaxVoices {
		return nil, fmt.Errorf("fzutil: invalid bstep %d (if this is a multi-disk continuation disk, run fzf info on disk 1 instead)", bstep)
	}
	nBankSectors := countBankSectors(data)
	voiceAreaStart := nBankSectors * disk.SectorSize
	if voiceAreaStart+disk.VoiceAreaSectors(vn)*disk.SectorSize > len(data) {
		return nil, fmt.Errorf("fzutil: DIS voice count %d needs a voice area running past the dump", vn)
	}
	for i := range vn {
		off := disk.VoiceSlotOffset(voiceAreaStart, i)
		if !disk.IsActiveOrEmptyVoiceSlot(data[off : off+disk.VoiceHeaderUsed]) {
			return nil, fmt.Errorf("fzutil: DIS voice count %d claims slot %d, which does not read as a voice header", vn, i)
		}
	}
	return &FZFHeader{
		NVoice:         vn,
		BStep0:         bstep,
		NBankSectors:   nBankSectors,
		VoiceAreaStart: voiceAreaStart,
	}, nil
}

// VoiceCountSource names where a resolved dump's voice count came
// from.
type VoiceCountSource uint8

// The three ways a dump's voice count resolves.
const (
	VoiceCountWalk VoiceCountSource = iota
	VoiceCountDIS
	VoiceCountMarker
)

// voiceMarkerMagic guards the voice-count marker record: the offset
// holds firmware garbage on real dumps. The trailing digit is the
// record version.
var voiceMarkerMagic = [4]byte{'f', 'z', 'v', '1'}

// After the magic: the count, the dump length, and a CRC32 over the
// structural bytes with the record zeroed, so a record that outlives
// an edit dies with it.
const (
	markerCountOffset = disk.BankVoiceMarkerOffset + 4
	markerLenOffset   = disk.BankVoiceMarkerOffset + 6
	markerSumOffset   = disk.BankVoiceMarkerOffset + 12
)

// markerChecksum hashes the dump's structural region (banks and the
// vn-sized voice area) with the marker record zeroed.
func markerChecksum(data []byte, vn int) uint32 {
	end := countBankSectors(data)*disk.SectorSize + disk.VoiceAreaSectors(vn)*disk.SectorSize
	if end > len(data) {
		end = len(data)
	}
	region := append([]byte(nil), data[:end]...)
	clear(region[disk.BankVoiceMarkerOffset : disk.BankVoiceMarkerOffset+disk.BankVoiceMarkerSize])
	return crc32.ChecksumIEEE(region)
}

// StampVoiceCountMarker writes the voice-count record into a
// standalone dump copy.
func StampVoiceCountMarker(data []byte, vn int) {
	if len(data) < disk.BankVoiceMarkerOffset+disk.BankVoiceMarkerSize || vn < 1 || vn > disk.MaxVoices {
		return
	}
	copy(data[disk.BankVoiceMarkerOffset:], voiceMarkerMagic[:])
	binary.LittleEndian.PutUint16(data[markerCountOffset:], uint16(vn))      //nolint:gosec // bounded above
	binary.LittleEndian.PutUint32(data[markerLenOffset:], uint32(len(data))) //nolint:gosec // bounded by MaxReadSize
	binary.LittleEndian.PutUint32(data[markerSumOffset:], markerChecksum(data, vn))
}

// ClearVoiceCountMarker zeroes the marker record where the magic
// matches; firmware garbage at the offset stays byte for byte.
func ClearVoiceCountMarker(data []byte) {
	if len(data) < disk.BankVoiceMarkerOffset+disk.BankVoiceMarkerSize {
		return
	}
	if !bytes.Equal(data[disk.BankVoiceMarkerOffset:disk.BankVoiceMarkerOffset+4], voiceMarkerMagic[:]) {
		return
	}
	clear(data[disk.BankVoiceMarkerOffset : disk.BankVoiceMarkerOffset+disk.BankVoiceMarkerSize])
}

// MarkerVoiceCount decodes the marker record's count, 0 unless the
// magic, the recorded length, and the structural checksum all hold.
// The count acceptance policy lives in the resolver alone.
func MarkerVoiceCount(data []byte) int {
	if len(data) < disk.BankVoiceMarkerOffset+disk.BankVoiceMarkerSize {
		return 0
	}
	if !bytes.Equal(data[disk.BankVoiceMarkerOffset:disk.BankVoiceMarkerOffset+4], voiceMarkerMagic[:]) {
		return 0
	}
	vn := int(binary.LittleEndian.Uint16(data[markerCountOffset:]))
	if vn < 1 || vn > disk.MaxVoices {
		return 0
	}
	if binary.LittleEndian.Uint32(data[markerLenOffset:]) != uint32(len(data)) { //nolint:gosec // bounded by MaxReadSize
		return 0
	}
	if binary.LittleEndian.Uint32(data[markerSumOffset:]) != markerChecksum(data, vn) {
		return 0
	}
	return vn
}

// inferVoiceCount walks voice slots from voiceAreaStart and counts the
// contiguous ones that are plausible active voices or explicit
// PlaybackModeNoSound placeholders. It stops at the first slot that doesn't
// look like a voice header, the boundary where the audio area begins.
// bstep is only an upper bound: it can exceed vn when several key splits
// share a voice via vp[]. A bstep below vn would under-count, since the
// min(bstep, MaxVoices) bound stops the walk early. No real-world FZF does
// that, and an under-count beats over-walking arbitrary bytes past the
// slot region.
func inferVoiceCount(data []byte, voiceAreaStart, bstep int) int {
	upper := bstep
	if upper > disk.MaxVoices {
		upper = disk.MaxVoices
	}
	n := 0
	for i := 0; i < upper; i++ {
		off := disk.VoiceSlotOffset(voiceAreaStart, i)
		if off+disk.VoiceHeaderUsed > len(data) {
			break
		}
		if !disk.IsActiveOrEmptyVoiceSlot(data[off : off+disk.VoiceHeaderUsed]) {
			break
		}
		n = i + 1
	}
	return n
}

// countAllVoices returns the inferred total voice count across all bank
// sectors. It sums bstep from every valid bank sector (clamped to
// MaxVoices) and uses that sum as the upper bound for InferVoiceCount.
//
// Bank 0's bstep alone is an unreliable bound on a multi-bank dump: a bstep
// belongs to that bank's key splits and misses voices living only in later
// banks. The sum is safe, overshooting when vp[] shares voices across
// banks, and the walk-and-validate step trims the overshoot. See
// ParseFZFHeader for why the count is walked rather than read.
func countAllVoices(data []byte) int {
	nBanks := countBankSectors(data)
	total := 0
	for b := range nBanks {
		off := b * disk.SectorSize
		if off+disk.BankVoiceCountOffset+2 > len(data) {
			break
		}
		bstep := int(binary.LittleEndian.Uint16(data[off+disk.BankVoiceCountOffset : off+disk.BankVoiceCountOffset+2]))
		total += bstep
	}
	if total > disk.MaxVoices {
		total = disk.MaxVoices
	}
	voiceAreaStart := nBanks * disk.SectorSize
	return inferVoiceCount(data, voiceAreaStart, total)
}

// countBankSectors returns the number of consecutive valid bank sectors at the
// start of a raw FZF byte slice. Real hardware full dumps can contain up to 8
// banks; each bank sector has a non-zero voice count and a printable name at
// offset 0x282. Returns at least 1.
//
// Note: a sector with bstep=0 is NOT counted as a bank, even with a
// printable name. Single-voice FZFs (from `fizzle voice import`) seed the
// voice-area sector with the voice name at offset 0x282 alongside bstep=0.
// Requiring bstep>0 keeps the bank and voice-area boundary unambiguous.
// Callers that produce empty trailing banks (auto-grow on rename, or
// assign-skip) must compact them at save time, or those banks vanish on
// reload.
func countBankSectors(data []byte) int {
	n := 1
	for i := 1; i < disk.MaxBanks; i++ {
		off := i * disk.SectorSize
		if off+disk.SectorSize > len(data) {
			break
		}
		candidate := data[off : off+disk.SectorSize]
		nv := int(binary.LittleEndian.Uint16(candidate[disk.BankVoiceCountOffset : disk.BankVoiceCountOffset+2]))
		if nv == 0 || nv > disk.MaxVoices {
			break
		}
		if !disk.IsPrintableName(candidate[disk.BankNameOffset : disk.BankNameOffset+disk.LabelSize]) {
			break
		}
		n++
	}
	return n
}

// OverCapacity reports whether sizeBytes exceeds the usable data
// capacity of a single FZ series floppy disk.
func OverCapacity(sizeBytes int) bool {
	return sizeBytes > disk.UsableDataSize
}

// BankVoiceEntry holds the subset of voice metadata extracted from the bank
// sector plus voice header that is common to both bank dumps (FZB) and full
// dumps (FZF). Callers compose this into their own entry types and add any
// format-specific fields on top.
//
// BankVolume is the per-voice bvol byte from the bank sector (spec §2-2,
// `bvol[64]`, range 0-127). Voicebuild writes DefaultBankVolume (0) for
// fresh dumps to match factory Casio disks (which sit in 0..27); factory
// and shareware FZFs commonly carry small non-zero values. The field's
// semantics aren't fully understood; see DefaultBankVolume.
type BankVoiceEntry struct {
	Index       int
	Name        string
	KeyLow      uint8
	KeyHigh     uint8
	RootNote    uint8
	MIDIChannel int
	Output      string
	VelLow      uint8
	VelHigh     uint8
	BankVolume  uint8
}

// VoiceEntry is the rendered voice-slot record shared between fzbinfo
// (bank dumps) and fzfinfo (full dumps). It holds the 11 fields that are
// meaningful for both file types. fzfinfo extends this type with
// audio-only fields (rate index, duration, has-loop) via struct embedding.
type VoiceEntry struct {
	Index        int    `json:"index"`
	Name         string `json:"name"`
	PlaybackMode string `json:"playback_mode"`
	RootNote     uint8  `json:"root_note"`
	KeyLow       uint8  `json:"key_low"`
	KeyHigh      uint8  `json:"key_high"`
	VelLow       uint8  `json:"vel_low"`
	VelHigh      uint8  `json:"vel_high"`
	MIDIChannel  int    `json:"midi_channel"`
	Output       string `json:"output"`
	BankVolume   uint8  `json:"bank_volume"`
}

// BankSite identifies one (bank, key-split) location where a voice slot is
// referenced via a bank sector's `vp[]` array (spec §2-2). On a multi-bank
// FZF a single voice header in the voice area may be referenced from any
// number of (bank, split) sites; each site carries its own htch/ltch/mchn/
// gchn/bvol/cent value indexed by SplitIdx within the bank at BankIdx.
//
// fizzle's own voicebuild emits single-bank dumps with identity `vp[i]=i`,
// so every voice slot there has exactly one site at {BankIdx: 0, SplitIdx:
// voiceSlot}. Real-hardware multi-bank dumps fan one slot across several
// banks (TECHNO.img references slot 10 from banks 1-4 and 6-7 at distinct
// key splits).
type BankSite struct {
	BankIdx  int // 0..hdr.NBankSectors-1
	SplitIdx int // 0..bstep[BankIdx]-1
}

// FindBankSitesForVoice walks every bank sector in data and returns the
// (bank, split) sites whose vp[SplitIdx] equals voiceSlot. The returned
// slice is in bank-then-split order, so the first element is the
// canonical "owning" site for read-display callers. An empty result means
// no bank references the voice (e.g. an orphan voice header); callers
// that mutate per-voice bank metadata must skip such slots rather than
// silently write into bank 0.
//
// Spec context: §2-2 defines `vp[64]` as the per-bank array mapping each
// key-split index to a voice slot (0-63). The bank sector's per-voice
// arrays (hwid, lwid, htch, ltch, cent, mchn, gchn, bvol) are likewise
// indexed by key-split, not by voice slot. Indexing them by voice slot
// silently drops any voice that lives only in banks 1-7.
func FindBankSitesForVoice(data []byte, hdr *FZFHeader, voiceSlot int) []BankSite {
	if hdr == nil {
		return nil
	}
	var sites []BankSite
	for b := range hdr.NBankSectors {
		bankOff := b * disk.SectorSize
		if bankOff+disk.SectorSize > len(data) {
			break
		}
		bstep := int(binary.LittleEndian.Uint16(data[bankOff+disk.BankVoiceCountOffset : bankOff+disk.BankVoiceCountOffset+2]))
		if bstep > disk.MaxVoices {
			bstep = disk.MaxVoices
		}
		for s := 0; s < bstep; s++ {
			vpOff := bankOff + disk.BankVoiceNumOffset + 2*s
			if vpOff+2 > len(data) {
				break
			}
			vp := int(binary.LittleEndian.Uint16(data[vpOff : vpOff+2]))
			if vp == voiceSlot {
				sites = append(sites, BankSite{BankIdx: b, SplitIdx: s})
			}
		}
	}
	return sites
}

// BankSliceAt returns the 1024-byte bank sector at bankIdx, or nil if the
// data is too small to contain it. Use with BankSite.BankIdx when reading
// per-voice bank fields at SplitIdx.
func BankSliceAt(data []byte, bankIdx int) []byte {
	off := bankIdx * disk.SectorSize
	if off+disk.SectorSize > len(data) {
		return nil
	}
	return data[off : off+disk.SectorSize]
}

// BankSectorShowsVelocity reports whether any voice slot in the voice area
// has a non-default velocity range at any of its bank sites. On a
// multi-bank FZF the velocity range belongs to the (bank, split) pair, not
// to the voice slot, so this visits every site of every voice. The (0,0)
// state counts too: it silences the voice. Spec §1-5 puts htch and ltch at
// 1-127, so that range never matches a note-on velocity, and the user
// should see the voice is unreachable.
func BankSectorShowsVelocity(data []byte, hdr *FZFHeader) bool {
	if hdr == nil {
		return false
	}
	for v := range hdr.NVoice {
		for _, site := range FindBankSitesForVoice(data, hdr, v) {
			bank := BankSliceAt(data, site.BankIdx)
			if bank == nil {
				continue
			}
			vl := bank[disk.BankVelLowOffset+site.SplitIdx]
			vh := bank[disk.BankVelHighOffset+site.SplitIdx]
			if vl == 0 && vh == 0 {
				return true
			}
			if vl != disk.DefaultVelLow || vh != disk.DefaultVelHigh {
				return true
			}
		}
	}
	return false
}

// BankSectorShowsVolume reports whether any voice slot in the voice area
// carries a non-default bvol at any of its bank sites (spec §2-2, range
// 0-127). Voicebuild writes DefaultBankVolume (0) on fresh dumps to match
// factory Casio disks; factory voices often carry small non-zero bvols to
// balance per-voice mix levels, so the info commands show the column only
// when it says something. Mirrors BankSectorShowsVelocity.
func BankSectorShowsVolume(data []byte, hdr *FZFHeader) bool {
	if hdr == nil {
		return false
	}
	for v := range hdr.NVoice {
		for _, site := range FindBankSitesForVoice(data, hdr, v) {
			bank := BankSliceAt(data, site.BankIdx)
			if bank == nil {
				continue
			}
			if bank[disk.BankVolumeOffset+site.SplitIdx] != disk.DefaultBankVolume {
				return true
			}
		}
	}
	return false
}

// ParseBankVoiceEntry extracts the shared voice metadata for voice slot
// voiceSlot. The voice header is read from voiceArea at the offset for
// voiceSlot; the bank metadata (key range, MIDI channel, output, bvol) is
// read from the (bank, split) site identified by the bank slice and
// splitIdx.
//
// On a single-bank FZF (anything voicebuild produces) callers pass
// bank=data[:SectorSize] and splitIdx=voiceSlot, the identity vp[i]=i
// case. On multi-bank FZFs callers must first look up the voice's BankSite
// via FindBankSitesForVoice and pass the matching bank slice and SplitIdx.
// The metadata then comes from the bank that defines the voice, not always
// from bank 0.
//
// Returns false when the voice slot is silenced (loop mode
// PlaybackModeNoSound) or when voiceArea is too short to hold the voice
// header.
func ParseBankVoiceEntry(bank, voiceArea []byte, splitIdx, voiceSlot int) (BankVoiceEntry, bool) {
	voff := disk.VoiceSlotOffset(0, voiceSlot)
	if voff+disk.VoiceHeaderUsed > len(voiceArea) {
		return BankVoiceEntry{}, false
	}
	hdr := voiceArea[voff : voff+disk.VoiceHeaderUsed]

	loopMode := binary.LittleEndian.Uint16(hdr[disk.VoiceLoopModeOffset : disk.VoiceLoopModeOffset+2])
	if loopMode == disk.PlaybackModeNoSound {
		return BankVoiceEntry{}, false
	}

	name := disk.TrimPadded(hdr[disk.VoiceNameOffset : disk.VoiceNameOffset+disk.LabelSize])
	if name == "" || !disk.IsPrintableName([]byte(name)) {
		name = fmt.Sprintf("VOICE %d", voiceSlot+1)
	}

	keyHigh := bank[disk.BankKeyHighOffset+splitIdx]
	keyLow := bank[disk.BankKeyLowOffset+splitIdx]
	velHigh := bank[disk.BankVelHighOffset+splitIdx]
	velLow := bank[disk.BankVelLowOffset+splitIdx]
	keyCent := bank[disk.BankKeyCentOffset+splitIdx]
	midiChan := int(bank[disk.BankMIDIRecvChanOffset+splitIdx]) + 1
	gchn := bank[disk.BankAudioOutOffset+splitIdx]

	if keyHigh > disk.MaxMIDINote {
		keyHigh = disk.MaxMIDINote
	}
	if keyLow > disk.MaxMIDINote {
		keyLow = disk.MaxMIDINote
	}
	if keyCent > disk.MaxMIDINote {
		keyCent = disk.MaxMIDINote
	}

	return BankVoiceEntry{
		Index:       voiceSlot + 1,
		Name:        name,
		KeyLow:      keyLow,
		KeyHigh:     keyHigh,
		RootNote:    keyCent,
		MIDIChannel: midiChan,
		Output:      disk.FormatAudioOut(gchn),
		VelLow:      velLow,
		VelHigh:     velHigh,
		BankVolume:  bank[disk.BankVolumeOffset+splitIdx],
	}, true
}

// ReadFZF reads an FZF full dump file at path, validates its header, and
// returns the raw data along with the parsed header.
func ReadFZF(path string) ([]byte, *FZFHeader, error) {
	data, err := ReadBounded(path, MaxReadSize)
	if err != nil {
		return nil, nil, err
	}
	layout, err := ResolveStandaloneFZFLayout(data)
	if err != nil {
		return nil, nil, err
	}
	hdr := &FZFHeader{NVoice: layout.VoiceCount(), BStep0: layout.BStep0(), NBankSectors: layout.BankCount(), VoiceAreaStart: layout.VoiceStart()}
	return data, hdr, nil
}

// ExtractStoredNames reads the trimmed voice names from an FZF byte slice
// using the offsets in hdr. Names that fall outside the data slice are
// returned as empty strings, preserving voice index alignment.
func ExtractStoredNames(data []byte, hdr *FZFHeader) []string {
	storedNames := make([]string, hdr.NVoice)
	for i := range hdr.NVoice {
		voff := disk.VoiceSlotOffset(hdr.VoiceAreaStart, i)
		if voff+disk.VoiceNameOffset+disk.LabelSize <= len(data) {
			raw := data[voff+disk.VoiceNameOffset : voff+disk.VoiceNameOffset+disk.LabelSize]
			storedNames[i] = disk.TrimPadded(raw)
		}
	}
	return storedNames
}

// ResolveVoiceTargets returns the voice indices selected by CLI-style
// selectors along with the full list of stored voice names for the FZF.
// If allVoices is true, every voice index is returned. Otherwise each name
// in voiceNames must match a stored name (case-insensitive); when a name
// has no match, an error listing the available voice names is returned.
//
// Callers must have already validated that exactly one of voiceNames or
// allVoices is supplied. The returned error carries the "fzutil:" prefix
// and callers may wrap it.
func ResolveVoiceTargets(data []byte, hdr *FZFHeader, voiceNames []string, allVoices bool) (targets []int, storedNames []string, err error) {
	storedNames = ExtractStoredNames(data, hdr)
	if allVoices {
		targets = make([]int, 0, hdr.NVoice)
		for i := range hdr.NVoice {
			targets = append(targets, i)
		}
		return targets, storedNames, nil
	}
	for _, want := range voiceNames {
		found := false
		for i, stored := range storedNames {
			if strings.EqualFold(stored, want) {
				targets = append(targets, i)
				found = true
			}
		}
		if !found {
			return nil, storedNames, voiceNotFoundError(want, storedNames)
		}
	}
	return targets, storedNames, nil
}

// IsMultiDiskFirstHalf reports whether data looks like disk 1 of a 2-disk
// full dump split: bank 0's BankTotalWaveOffset claims more wave sectors
// than are present locally, AND at least one plausibly-named voice's wavst
// points past the local audio area. Both conditions matter because the
// BankTotalWaveOffset marker is frequently garbage in real-world dumps, so
// the voice check keeps out false positives. Mirrors the heuristic in
// fzfinfo without pulling in the renderer it doesn't need.
//
// Callers gate destructive operations on this. A bank grow, for one, must
// refuse on disk 1 of a split: BankCount is shared with disk 2, so growing
// one desyncs the pair.
func IsMultiDiskFirstHalf(data []byte) bool {
	if len(data) < disk.SectorSize+8 {
		return false
	}
	hdr, err := ParseFZFHeader(data)
	if err != nil {
		return false
	}
	voiceSectors := disk.VoiceAreaSectors(hdr.NVoice)
	voiceAreaEnd := hdr.VoiceAreaStart + voiceSectors*disk.SectorSize
	if len(data) < voiceAreaEnd {
		return false
	}
	bank := data[:disk.SectorSize]
	totalWaveMarker := int(binary.LittleEndian.Uint32(
		bank[disk.BankTotalWaveOffset : disk.BankTotalWaveOffset+4]))
	localAudioBytes := len(data) - voiceAreaEnd
	localWaveSectors := localAudioBytes / disk.SectorSize
	if totalWaveMarker <= 0 || totalWaveMarker <= localWaveSectors {
		return false
	}
	voiceArea := data[hdr.VoiceAreaStart:voiceAreaEnd]
	for i := 0; i < hdr.NVoice; i++ {
		voff := disk.VoiceSlotOffset(0, i)
		if voff+disk.VoiceHeaderUsed > len(voiceArea) {
			continue
		}
		slot := voiceArea[voff : voff+disk.VoiceHeaderUsed]
		if !disk.IsPlausibleVoiceSlot(slot) {
			continue
		}
		wavst := binary.LittleEndian.Uint32(
			slot[disk.VoiceWaveStartOffset : disk.VoiceWaveStartOffset+4])
		if int(wavst)*disk.BytesPerSample >= localAudioBytes {
			return true
		}
	}
	return false
}

// voiceNotFoundError builds a "voice not found" error that lists the
// distinct, non-empty stored voice names in sorted order.
func voiceNotFoundError(want string, stored []string) error {
	available := make([]string, 0, len(stored))
	seen := map[string]bool{}
	for _, s := range stored {
		if s != "" && !seen[s] {
			available = append(available, s)
			seen[s] = true
		}
	}
	sort.Strings(available)
	return fmt.Errorf("fzutil: voice %q not found\navailable voices: %s",
		want, strings.Join(available, ", "))
}
