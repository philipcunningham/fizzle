// Package fzutil provides shared FZ format utilities.
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

// DefaultRate is the highest FZ sample rate.
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

// ErrSourceRateTooLow and ErrTooLong classify resampling refusals.
var (
	ErrSourceRateTooLow = errors.New("fzutil: source sample rate below minimum")
	ErrTooLong          = errors.New("fzutil: resampled length exceeds the sampler's memory")
)

// MinSampleRate prevents tiny source rates from expanding into impractical output.
const MinSampleRate = 1000

// MaxResampleOut is the sample count that fits in FZ sample memory.
const MaxResampleOut = disk.MaxSampleRAM / disk.BytesPerSample

// ResampledLen estimates output using the same guards as Resample.
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
	// Float conversion avoids integer overflow before the allocation bound applies.
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

// Resample uses linear interpolation and copies samples when rates match.
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
		// Integer positions avoid platform-dependent floating-point rounding.
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
		// int64 prevents the interpolation product from overflowing.
		a := int64(src[lo])
		b := int64(src[hi])
		v := a + (b-a)*rem/dstRate
		out[i] = int16(v) //nolint:gosec // G115: value clamped to int16 range on preceding lines
	}
	return out, nil
}

// VoiceName normalizes a path stem for the 12-character FZ display.
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

// ReadFZV reads an FZV containing at least one header sector.
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

// FZFHeader holds resolved full-dump geometry. BStep0 counts bank splits, while NVoice counts file-level voice slots.
type FZFHeader struct {
	NVoice         int
	BStep0         int
	NBankSectors   int
	VoiceAreaStart int
}

// ParseFZFHeader resolves standalone geometry by walking voice slots. See llm-wiki/topics/voice-area-sizing.md.
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
	// Every bank contributes to the walk bound.
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

// parseFZFHeaderWithVoiceCount validates geometry under DIS authority. See llm-wiki/topics/voice-area-sizing.md.
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

// VoiceCountSource identifies voice-count authority.
type VoiceCountSource uint8

// VoiceCountWalk, VoiceCountDIS, and VoiceCountMarker identify count authority.
const (
	VoiceCountWalk VoiceCountSource = iota
	VoiceCountDIS
	VoiceCountMarker
)

// voiceMarkerMagic distinguishes fizzle metadata from firmware garbage at the same offset.
var voiceMarkerMagic = [4]byte{'f', 'z', 'v', '1'}

const (
	markerCountOffset = disk.BankVoiceMarkerOffset + 4
	markerLenOffset   = disk.BankVoiceMarkerOffset + 6
	markerSumOffset   = disk.BankVoiceMarkerOffset + 12
)

// markerChecksum excludes its own marker record.
func markerChecksum(data []byte, vn int) uint32 {
	end := countBankSectors(data)*disk.SectorSize + disk.VoiceAreaSectors(vn)*disk.SectorSize
	if end > len(data) {
		end = len(data)
	}
	region := append([]byte(nil), data[:end]...)
	clear(region[disk.BankVoiceMarkerOffset : disk.BankVoiceMarkerOffset+disk.BankVoiceMarkerSize])
	return crc32.ChecksumIEEE(region)
}

// StampVoiceCountMarker writes voice-count authority into a standalone dump.
func StampVoiceCountMarker(data []byte, vn int) {
	if len(data) < disk.BankVoiceMarkerOffset+disk.BankVoiceMarkerSize || vn < 1 || vn > disk.MaxVoices {
		return
	}
	copy(data[disk.BankVoiceMarkerOffset:], voiceMarkerMagic[:])
	binary.LittleEndian.PutUint16(data[markerCountOffset:], uint16(vn))      //nolint:gosec // bounded above
	binary.LittleEndian.PutUint32(data[markerLenOffset:], uint32(len(data))) //nolint:gosec // bounded by MaxReadSize
	binary.LittleEndian.PutUint32(data[markerSumOffset:], markerChecksum(data, vn))
}

// ClearVoiceCountMarker removes only a recognized fizzle marker.
func ClearVoiceCountMarker(data []byte) {
	if len(data) < disk.BankVoiceMarkerOffset+disk.BankVoiceMarkerSize {
		return
	}
	if !bytes.Equal(data[disk.BankVoiceMarkerOffset:disk.BankVoiceMarkerOffset+4], voiceMarkerMagic[:]) {
		return
	}
	clear(data[disk.BankVoiceMarkerOffset : disk.BankVoiceMarkerOffset+disk.BankVoiceMarkerSize])
}

// MarkerVoiceCount returns a validated marker count or zero.
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

// inferVoiceCount walks plausible slots up to the split-count bound. See llm-wiki/topics/voice-area-sizing.md.
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

// countAllVoices uses every bank's split count as a bounded walk limit.
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

// countBankSectors counts consecutive named banks with nonzero split counts. Empty trailing banks don't survive reload.
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

// OverCapacity reports whether bytes exceed one FZ floppy.
func OverCapacity(sizeBytes int) bool {
	return sizeBytes > disk.UsableDataSize
}

// BankVoiceEntry combines metadata shared by bank and full dumps. BankVolume remains uninterpreted.
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

// VoiceEntry is the rendered metadata shared by bank and full dumps.
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

// BankSite identifies a bank split whose vp entry references a voice slot.
type BankSite struct {
	BankIdx  int // 0..hdr.NBankSectors-1
	SplitIdx int // 0..bstep[BankIdx]-1
}

// FindBankSitesForVoice returns referencing splits in bank order. Bank metadata uses split indices, not voice-slot indices.
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

// BankSliceAt returns a bank sector or nil when unavailable.
func BankSliceAt(data []byte, bankIdx int) []byte {
	off := bankIdx * disk.SectorSize
	if off+disk.SectorSize > len(data) {
		return nil
	}
	return data[off : off+disk.SectorSize]
}

// BankSectorShowsVelocity reports whether any bank site has a nondefault or silent velocity range.
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

// BankSectorShowsVolume reports whether any bank site has nondefault volume.
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

// ParseBankVoiceEntry combines one voice header with one referencing bank split. It rejects silent or truncated slots.
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

// ReadFZF reads bytes and resolved geometry from an FZF.
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

// ExtractStoredNames preserves slot alignment by returning empty names for unavailable slots.
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

// ResolveVoiceTargets resolves case-insensitive CLI selectors and lists stored names.
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
