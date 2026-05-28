// Package voiceedit provides in-place byte patching for FZV and FZF voice
// parameters. Patches are applied atomically: the file is read, modified in
// memory, and written back via fileutil.WriteAtomic.
package voiceedit

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fileutil"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
)

// Sentinel errors. Wrap with %w; callers should use errors.Is to identify
// a specific failure mode rather than matching error message substrings.
var (
	ErrNotVoiceFile     = errors.New("voiceedit: file does not appear to be a voice file")
	ErrUnsupportedPatch = errors.New("voiceedit: unsupported patch size")
	ErrFileTooSmall     = errors.New("voiceedit: file too small")
)

// Patch describes a modification to a voice header. When Bytes is non-nil it
// is written verbatim at Offset and Size/Value are ignored. Otherwise Size
// (1 or 2) bytes from Value are written as little-endian.
type Patch struct {
	Offset int
	Size   int    // 1 or 2 (ignored when Bytes is set)
	Value  uint16 // ignored when Bytes is set
	Bytes  []byte // multi-byte payload; when non-nil takes precedence over Size/Value
}

// ApplyToFZV reads the FZV file at path, applies patches to the voice header,
// and writes the result back atomically. Offsets are relative to the start of
// the voice header (byte 0 of the file). The read-modify-write sequence is
// serialised across processes via fileutil.WithFileLock so concurrent writers
// can't lose each other's edits.
func ApplyToFZV(path string, patches []Patch) error {
	return fileutil.WithFileLock(path, func() error {
		return applyToFZVLocked(path, patches)
	})
}

func applyToFZVLocked(path string, patches []Patch) error {
	data, err := fzutil.ReadBounded(path, fzutil.MaxReadSize)
	if err != nil {
		return fmt.Errorf("voiceedit: reading FZV: %w", err)
	}
	if len(data) < disk.SectorSize {
		return fmt.Errorf("%w (%d bytes, need at least %d)", ErrFileTooSmall, len(data), disk.SectorSize)
	}
	if !disk.IsPrintableName(data[disk.VoiceNameOffset : disk.VoiceNameOffset+disk.LabelSize]) {
		return fmt.Errorf("%w: %q", ErrNotVoiceFile, path)
	}
	if err := applyPatches(data, 0, patches); err != nil {
		return fmt.Errorf("voiceedit: %w", err)
	}
	if err := fileutil.WriteAtomic(path, data); err != nil {
		return fmt.Errorf("voiceedit: %w", err)
	}
	log.Info().Str("file", filepath.Base(path)).Msg("voice parameters updated")
	return nil
}

// ApplyToFZFVoice reads the FZF file at path, locates the voice by name,
// applies patches to that voice's header, and writes back atomically. The
// read-modify-write sequence is serialised across processes via
// fileutil.WithFileLock to prevent lost writes from concurrent edits.
func ApplyToFZFVoice(path string, voiceName string, patches []Patch) error {
	return fileutil.WithFileLock(path, func() error {
		return applyToFZFVoiceLocked(path, voiceName, patches)
	})
}

func applyToFZFVoiceLocked(path string, voiceName string, patches []Patch) error {
	data, err := fzutil.ReadBounded(path, fzutil.MaxReadSize)
	if err != nil {
		return fmt.Errorf("voiceedit: reading FZF: %w", err)
	}
	hdr, err := fzutil.ParseFZFHeader(data)
	if err != nil {
		return fmt.Errorf("voiceedit: %w", err)
	}
	idx, err := findVoiceIndex(data, hdr, voiceName)
	if err != nil {
		return err
	}
	voiceOffset := disk.VoiceSlotOffset(hdr.VoiceAreaStart, idx)
	if voiceOffset+disk.VoiceHeaderUsed > len(data) {
		return fmt.Errorf("voiceedit: voice %d header extends beyond file", idx)
	}
	if err := applyPatches(data, voiceOffset, patches); err != nil {
		return fmt.Errorf("voiceedit: %w", err)
	}
	if err := syncBankKeyRange(data, hdr, idx, voiceOffset, patches); err != nil {
		return fmt.Errorf("voiceedit: %w", err)
	}
	if err := fileutil.WriteAtomic(path, data); err != nil {
		return fmt.Errorf("voiceedit: %w", err)
	}
	log.Info().Str("file", filepath.Base(path)).Str("voice", voiceName).Msg("voice parameters updated")
	return nil
}

// syncBankKeyRange mirrors any key-range patches (key-low / key-high / cent)
// from the voice header into every bank site that references the voice slot.
//
// Spec context: the voice-header fields at 0xae/0xaf/0xb0 (`hwid`/`lwid`/`cent`,
// §2-1) are read only when the FZ-1 loads a voice standalone via the per-voice
// disk path. When the FZF is loaded as a bank (the normal case), the firmware
// reads the per-split arrays in the bank sector (§2-2: `hwid[64]` @ 0x02,
// `lwid[64]` @ 0x42, `cent[64]` @ 0x102), and the spec explicitly notes that
// these "can be set independently from those for voice data". A `fizzle fzf
// edit --key-low/--key-high/--root` that touched only the voice header would
// be a silent no-op on hardware playback. This mirrors the multi-bank
// fan-out pattern used by fzfmidi.Set / fzfoutput.Set.
//
// Non-key-range patches (filter, LFO, envelope, name, tune, playback mode)
// have no bank counterpart and are skipped.
func syncBankKeyRange(data []byte, hdr *fzutil.FZFHeader, idx int, voiceOffset int, patches []Patch) error {
	bankOffsetFor := func(voiceHdrOffset int) (int, bool) {
		switch voiceHdrOffset {
		case disk.VoiceKeyHighOffset:
			return disk.BankKeyHighOffset, true
		case disk.VoiceKeyLowOffset:
			return disk.BankKeyLowOffset, true
		case disk.VoiceKeyCentOffset:
			return disk.BankKeyCentOffset, true
		}
		return 0, false
	}

	var sites []fzutil.BankSite
	sitesLoaded := false
	for _, p := range patches {
		bankOff, ok := bankOffsetFor(p.Offset)
		if !ok {
			continue
		}
		// Read the post-write byte directly from the voice-header slot;
		// robust to whatever applyPatches actually wrote (Value vs Bytes).
		srcOff := voiceOffset + p.Offset
		if srcOff >= len(data) {
			return fmt.Errorf("key-range source byte at %d beyond data", srcOff)
		}
		newByte := data[srcOff]
		if !sitesLoaded {
			sites = fzutil.FindBankSitesForVoice(data, hdr, idx)
			sitesLoaded = true
		}
		for _, site := range sites {
			off := site.BankIdx*disk.SectorSize + bankOff + site.SplitIdx
			if off >= len(data) {
				return fmt.Errorf("bank site write at %d beyond data", off)
			}
			data[off] = newByte
		}
	}
	return nil
}

func applyPatches(data []byte, base int, patches []Patch) error {
	for _, p := range patches {
		size := p.Size
		if p.Bytes != nil {
			size = len(p.Bytes)
		}
		if p.Offset < 0 || p.Offset+size > disk.VoiceHeaderUsed {
			return fmt.Errorf("voiceedit: patch offset %d (size %d) out of voice header range", p.Offset, size)
		}
		off := base + p.Offset
		if off+size > len(data) {
			return fmt.Errorf("voiceedit: patch at %d extends beyond data", off)
		}
		if p.Bytes != nil {
			copy(data[off:off+size], p.Bytes)
			continue
		}
		switch p.Size {
		case 1:
			data[off] = byte(p.Value) //nolint:gosec // G115: value validated before patch creation
		case 2:
			binary.LittleEndian.PutUint16(data[off:], p.Value)
		default:
			return fmt.Errorf("%w: %d", ErrUnsupportedPatch, p.Size)
		}
	}
	return nil
}
func findVoiceIndex(data []byte, hdr *fzutil.FZFHeader, name string) (int, error) {
	target := strings.ToUpper(strings.TrimSpace(name))
	for i := range hdr.NVoice {
		off := disk.VoiceSlotOffset(hdr.VoiceAreaStart, i)
		if off+disk.VoiceNameOffset+disk.LabelSize > len(data) {
			break
		}
		raw := data[off+disk.VoiceNameOffset : off+disk.VoiceNameOffset+disk.LabelSize]
		stored := strings.ToUpper(disk.TrimPadded(raw))
		if stored == target {
			return i, nil
		}
	}
	return -1, fmt.Errorf("voiceedit: voice %q not found", name)
}

// BuildLFOPatches creates patches for LFO parameters. Pass Unchanged for any
// parameter to leave it unmodified. origLFOName is the current value of the
// lfo_name byte (spec offset 0x9E): bits 0-6 hold the waveform index, bit 7
// is the phase-sync flag. It is used to preserve the phase-sync flag when
// only the waveform index changes; see disk.LFOWaveformMask / LFOPhaseFlag.
func BuildLFOPatches(wave, rate, delay, attack, pitch, amp, filter, q int, origLFOName uint8) ([]Patch, error) {
	var patches []Patch
	if wave != Unchanged {
		if err := ValidateWaveform(wave); err != nil {
			return nil, err
		}
		// Preserve bit 7 (phase-sync) of the original lfo_name byte;
		// writing a clean byte(wave) would silently clear it.
		val := uint8(wave)&disk.LFOWaveformMask | (origLFOName & disk.LFOPhaseFlag) //nolint:gosec // wave is validated above (0..5)
		patches = append(patches, Patch{Offset: disk.VoiceLFONameOffset, Size: 1, Value: uint16(val)})
	}
	if delay != Unchanged {
		if delay > disk.MaxLFODelay {
			return nil, fmt.Errorf("voiceedit: lfo-delay must be 0 to %d, got %d", disk.MaxLFODelay, delay)
		}
		patches = append(patches, Patch{Offset: disk.VoiceLFODelayOffset, Size: 2, Value: bitconv.NarrowU16(delay)})
	}
	type lfoParam struct {
		name   string
		val    int
		offset int
	}
	params := []lfoParam{
		{"lfo-rate", rate, disk.VoiceLFORateOffset},
		{"lfo-attack", attack, disk.VoiceLFOAtckOffset},
		{"lfo-pitch", pitch, disk.VoiceLFODCPOffset},
		{"lfo-amp", amp, disk.VoiceLFODCAOffset},
		{"lfo-filter", filter, disk.VoiceLFODCFOffset},
		{"lfo-q", q, disk.VoiceLFODCQOffset},
	}
	for _, p := range params {
		if p.val != Unchanged {
			if err := ValidateByte(p.name, p.val, 0, 127); err != nil {
				return nil, err
			}
			patches = append(patches, Patch{Offset: p.offset, Size: 1, Value: bitconv.NarrowU16(p.val)})
		}
	}
	return patches, nil
}

// BuildModulationPatches creates patches for modulation routing parameters.
// KF parameters (dcaKF, dcaRS, dcfKF, dcfRS) use the hardware display scale
// (-15 to +15). All five velocity-modulation parameters (velDCAKF, velDCFKF,
// velDCQKF, velDCARS, velDCFRS) are signed -127 to +127 per spec §2-1 and
// are stored as two's-complement bytes. Pass Unchanged for any parameter to
// leave it unmodified.
func BuildModulationPatches(dcaKF, dcaRS, dcfKF, dcfRS, velDCAKF, velDCFKF, velDCQKF, velDCARS, velDCFRS int) ([]Patch, error) {
	type kfParam struct {
		name   string
		val    int
		offset int
	}
	kfParams := []kfParam{
		{"dca-level-kf", dcaKF, disk.VoiceDCAKFOffset},
		{"dca-rate-kf", dcaRS, disk.VoiceDCARSOffset},
		{"dcf-level-kf", dcfKF, disk.VoiceDCFKFOffset},
		{"dcf-rate-kf", dcfRS, disk.VoiceDCFRSOffset},
	}
	var patches []Patch
	for _, p := range kfParams {
		if p.val != Unchanged {
			if err := ValidateByte(p.name, p.val, disk.MinKFDisplay, disk.MaxKFDisplay); err != nil {
				return nil, err
			}
			patches = append(patches, Patch{Offset: p.offset, Size: 1, Value: uint16(disk.KFDisplayToByte(p.val))})
		}
	}
	type signedParam struct {
		name   string
		val    int
		offset int
	}
	signedParams := []signedParam{
		{"vel-dca-kf", velDCAKF, disk.VoiceVelDCAKFOffset},
		{"vel-dcf-kf", velDCFKF, disk.VoiceVelDCFKFOffset},
		{"vel-dcq-kf", velDCQKF, disk.VoiceVelDCQKFOffset},
		{"vel-dca-rs", velDCARS, disk.VoiceVelDCARSOffset},
		{"vel-dcf-rs", velDCFRS, disk.VoiceVelDCFRSOffset},
	}
	for _, p := range signedParams {
		if p.val != Unchanged {
			if err := ValidateByte(p.name, p.val, -127, 127); err != nil {
				return nil, err
			}
			patches = append(patches, Patch{Offset: p.offset, Size: 1, Value: uint16(uint8(int8(p.val)))}) //nolint:gosec // G115: intentional two's complement conversion; value validated above
		}
	}
	return patches, nil
}

// BuildFilterPatches creates patches for filter cutoff and resonance.
// Both use the hardware display scale: cutoff 0 to 127, resonance 0 to 127.
// The resonance byte is stored directly (the full byte is used by the hardware,
// not just the upper nibble as the spec suggests). Pass Unchanged to leave
// a parameter unmodified.
func BuildFilterPatches(cutoff, resonance int) ([]Patch, error) {
	var patches []Patch
	if cutoff != Unchanged {
		if err := ValidateByte("cutoff", cutoff, 0, 127); err != nil {
			return nil, err
		}
		patches = append(patches, Patch{Offset: disk.VoiceDCFOffset, Size: 1, Value: bitconv.NarrowU16(cutoff)})
	}
	if resonance != Unchanged {
		if err := ValidateByte("resonance", resonance, 0, disk.MaxResonance); err != nil {
			return nil, err
		}
		patches = append(patches, Patch{Offset: disk.VoiceDCQOffset, Size: 1, Value: bitconv.NarrowU16(resonance)})
	}
	return patches, nil
}

// BuildNamePatch creates a patch for the voice name (max 12 characters). The
// 12-byte padded name is followed by two zero bytes required by the FZ voice
// header layout.
// BuildNamePatch stores the voice name verbatim: mixed case preserved.
// The FZ-1 hardware supports mixed-case names (factory disks such as
// "All Voices" demonstrate this); upper-casing on commit surprises
// users when they Tab through an unchanged field and the displayed
// value mutates. Voice-name lookups elsewhere (findVoiceIndex) match
// case-insensitively, so existing case-insensitive flows still work.
func BuildNamePatch(name string) ([]Patch, error) {
	if len(name) > disk.LabelSize {
		return nil, fmt.Errorf("voiceedit: name %q exceeds %d characters", name, disk.LabelSize)
	}
	padded := disk.PadLabel(name)
	payload := make([]byte, disk.VoiceNameFieldSize)
	copy(payload, padded[:])
	return []Patch{{Offset: disk.VoiceNameOffset, Bytes: payload}}, nil
}

// ValidateByte checks that val is within the given range.
func ValidateByte(name string, val, lo, hi int) error {
	if val < lo || val > hi {
		return fmt.Errorf("voiceedit: %s must be %d to %d, got %d", name, lo, hi, val)
	}
	return nil
}

// ValidateWaveform checks that val is a valid LFO waveform index.
func ValidateWaveform(val int) error {
	if val < 0 || val > disk.LFORandom {
		return fmt.Errorf("voiceedit: waveform must be 0 to %d (sine, saw-up, saw-down, triangle, rectangle, random), got %d", disk.LFORandom, val)
	}
	return nil
}

var waveformNames = map[string]int{
	"sine":      disk.LFOSine,
	"saw-up":    disk.LFOSawUp,
	"saw-down":  disk.LFOSawDown,
	"triangle":  disk.LFOTriangle,
	"rectangle": disk.LFORectangle,
	"random":    disk.LFORandom,
}

// WaveformIndex returns the index for the named LFO waveform and whether it was found.
func WaveformIndex(name string) (int, bool) {
	name = strings.ToLower(name)
	idx, ok := waveformNames[name]
	return idx, ok
}

const (
	// Unchanged is the sentinel value for parameters that should not be
	// modified. It must be outside all valid parameter ranges.
	Unchanged = -1000
)

// BuildTunePatch creates a patch for the voice tuning (DCP field). The value
// is in 1/256-semitone units and stored as a uint16 (two's complement).
func BuildTunePatch(tune int) ([]Patch, error) {
	if tune < -32768 || tune > 32767 {
		return nil, fmt.Errorf("voiceedit: tune must be -32768 to 32767, got %d", tune)
	}
	return []Patch{{Offset: disk.VoiceDCPOffset, Size: 2, Value: uint16(int16(tune))}}, nil //nolint:gosec // validated above
}

// BuildKeyRangePatch creates patches for the key range (key-low, key-high,
// root). Each value is a MIDI note number (0 to 127). Pass Unchanged for
// any parameter to leave it unmodified.
func BuildKeyRangePatch(keyLow, keyHigh, root int) ([]Patch, error) {
	type keyParam struct {
		name   string
		val    int
		offset int
	}
	params := []keyParam{
		{"key-low", keyLow, disk.VoiceKeyLowOffset},
		{"key-high", keyHigh, disk.VoiceKeyHighOffset},
		{"root", root, disk.VoiceKeyCentOffset},
	}
	var patches []Patch
	for _, p := range params {
		if p.val != Unchanged {
			if err := ValidateByte(p.name, p.val, 0, disk.MaxMIDINote); err != nil {
				return nil, err
			}
			patches = append(patches, Patch{Offset: p.offset, Size: 1, Value: bitconv.NarrowU16(p.val)})
		}
	}
	return patches, nil
}

var playbackModes = map[string]uint16{
	"normal":  disk.PlaybackModeNormal,
	"reverse": disk.PlaybackModeReverse,
	"cue":     disk.PlaybackModeCue,
	"synth":   disk.PlaybackModeSynthesized,
}

// BuildPlaybackModePatch creates a patch for the voice playback mode. The mode
// name is matched case-insensitively. Valid modes: Normal, Reverse, Cue, Synth.
func BuildPlaybackModePatch(mode string) ([]Patch, error) {
	val, ok := playbackModes[strings.ToLower(mode)]
	if !ok {
		return nil, fmt.Errorf("voiceedit: unknown playback mode %q (use: normal, reverse, cue, synth)", mode)
	}
	return []Patch{{Offset: disk.VoiceLoopModeOffset, Size: 2, Value: val}}, nil
}

// BuildDCAPatches creates patches for DCA envelope parameters. Pass Unchanged
// for sustain/end to leave unchanged. Pass Unchanged for individual rate/level
// array elements to leave them unchanged. Rates and levels use the hardware
// display scale (0 to 99). origRates carries the original rate bytes so the
// sign bit (envelope direction) is preserved when only the magnitude changes.
func BuildDCAPatches(sustain, end int, rates, stops [disk.EnvelopeStages]int, origRates [disk.EnvelopeStages]uint8) ([]Patch, error) {
	return buildEnvelopePatches("dca", sustain, end, rates, stops, origRates,
		disk.VoiceDCASusOffset, disk.VoiceDCAEndOffset,
		disk.VoiceDCARateOffset, disk.VoiceDCAStopOffset)
}

// BuildDCFPatches creates patches for DCF envelope parameters. Pass
// Unchanged for sustain/end to leave unchanged. Pass Unchanged for individual
// rate/level array elements to leave them unchanged. Rates and levels use
// the hardware display scale (0 to 99). origRates carries the original rate
// bytes so the sign bit (envelope direction) is preserved.
func BuildDCFPatches(sustain, end int, rates, stops [disk.EnvelopeStages]int, origRates [disk.EnvelopeStages]uint8) ([]Patch, error) {
	return buildEnvelopePatches("dcf", sustain, end, rates, stops, origRates,
		disk.VoiceDCFSusOffset, disk.VoiceDCFEndOffset,
		disk.VoiceDCFRateOffset, disk.VoiceDCFStopOffset)
}

func buildEnvelopePatches(prefix string, sustain, end int, rates, stops [disk.EnvelopeStages]int, origRates [disk.EnvelopeStages]uint8, susOff, endOff, rateOff, stopOff int) ([]Patch, error) {
	var patches []Patch
	if sustain != Unchanged {
		if err := ValidateByte(prefix+"-sustain", sustain, 0, 7); err != nil {
			return nil, err
		}
		patches = append(patches, Patch{Offset: susOff, Size: 1, Value: bitconv.NarrowU16(sustain)})
	}
	if end != Unchanged {
		if err := ValidateByte(prefix+"-end", end, 0, 7); err != nil {
			return nil, err
		}
		patches = append(patches, Patch{Offset: endOff, Size: 1, Value: bitconv.NarrowU16(end)})
	}
	for i := range disk.EnvelopeStages {
		if rates[i] != Unchanged {
			if rates[i] < 0 || rates[i] > disk.DisplayMax {
				return nil, fmt.Errorf("voiceedit: %s-rate-%d must be 0 to %d, got %d", prefix, i+1, disk.DisplayMax, rates[i])
			}
			b := disk.RateDisplayToByte(rates[i])
			if origRates[i]&disk.RateSignBit != 0 {
				b |= disk.RateSignBit
			}
			patches = append(patches, Patch{Offset: rateOff + i, Size: 1, Value: uint16(b)})
		}
		if stops[i] != Unchanged {
			if stops[i] < 0 || stops[i] > disk.DisplayMax {
				return nil, fmt.Errorf("voiceedit: %s-level-%d must be 0 to %d, got %d", prefix, i+1, disk.DisplayMax, stops[i])
			}
			patches = append(patches, Patch{Offset: stopOff + i, Size: 1, Value: uint16(disk.StopDisplayToByte(stops[i]))})
		}
	}
	return patches, nil
}
