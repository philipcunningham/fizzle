// Package voiceedit patches FZV and FZF voice parameters atomically.
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
	"github.com/philipcunningham/fizzle/pkg/model"
	"github.com/philipcunningham/fizzle/pkg/voicepatch"
)

// ErrNotVoiceFile, ErrUnsupportedPatch, and ErrFileTooSmall classify edit refusals.
var (
	ErrNotVoiceFile     = errors.New("voiceedit: file does not appear to be a voice file")
	ErrUnsupportedPatch = voicepatch.ErrUnsupportedSize
	ErrFileTooSmall     = errors.New("voiceedit: file too small")
)

// Edit describes a voice-header mutation.
type Edit = voicepatch.Edit

// ApplyToFZVBytes applies validated edits to FZV bytes in place.
func ApplyToFZVBytes(data []byte, patches []Edit) error {
	if len(data) < disk.SectorSize {
		return fmt.Errorf("%w (%d bytes, need at least %d)", ErrFileTooSmall, len(data), disk.SectorSize)
	}
	if !disk.IsPrintableName(data[disk.VoiceNameOffset : disk.VoiceNameOffset+disk.LabelSize]) {
		return ErrNotVoiceFile
	}
	resolved, err := voicepatch.ResolveHeader(data, 0, patches)
	if err != nil {
		return fmt.Errorf("voiceedit: %w", err)
	}
	if err := model.Apply(data, resolved); err != nil {
		return fmt.Errorf("voiceedit: %w", err)
	}
	return nil
}

// ApplyToFZV applies header-relative patches under a cross-process lock.
func ApplyToFZV(path string, patches []Edit) error {
	return fileutil.WithFileLock(path, func() error {
		return applyToFZVLocked(path, patches)
	})
}

func applyToFZVLocked(path string, patches []Edit) error {
	data, err := fzutil.ReadBounded(path, fzutil.MaxReadSize)
	if err != nil {
		return fmt.Errorf("voiceedit: reading FZV: %w", err)
	}
	if err := ApplyToFZVBytes(data, patches); err != nil {
		return err
	}
	if err := fileutil.WriteAtomic(path, data); err != nil {
		return fmt.Errorf("voiceedit: %w", err)
	}
	log.Info().Str("file", filepath.Base(path)).Msg("voice parameters updated")
	return nil
}

// ApplyToFZFVoice atomically patches the named voice under a cross-process lock.
func ApplyToFZFVoice(path string, voiceName string, patches []Edit) error {
	return fileutil.WithFileLock(path, func() error {
		return applyToFZFVoiceLocked(path, voiceName, patches)
	})
}

func applyToFZFVoiceLocked(path string, voiceName string, patches []Edit) error {
	data, err := fzutil.ReadBounded(path, fzutil.MaxReadSize)
	if err != nil {
		return fmt.Errorf("voiceedit: reading FZF: %w", err)
	}
	layout, err := fzutil.ResolveStandaloneFZFLayout(data)
	if err != nil {
		return fmt.Errorf("voiceedit: %w", err)
	}
	hdr := &fzutil.FZFHeader{NVoice: layout.VoiceCount(), BStep0: layout.BStep0(), NBankSectors: layout.BankCount(), VoiceAreaStart: layout.VoiceStart()}
	idx, err := findVoiceIndex(data, hdr, voiceName)
	if err != nil {
		return err
	}
	voiceOffset := disk.VoiceSlotOffset(hdr.VoiceAreaStart, idx)
	if voiceOffset+disk.VoiceHeaderUsed > len(data) {
		return fmt.Errorf("voiceedit: voice %d header extends beyond file", idx)
	}
	resolved, err := voicepatch.ResolveFZFSlot(data, layout, idx, patches)
	if err != nil {
		return fmt.Errorf("voiceedit: %w", err)
	}
	if err := model.Apply(data, resolved); err != nil {
		return fmt.Errorf("voiceedit: %w", err)
	}
	if err := fileutil.WriteAtomic(path, data); err != nil {
		return fmt.Errorf("voiceedit: %w", err)
	}
	log.Info().Str("file", filepath.Base(path)).Str("voice", voiceName).Msg("voice parameters updated")
	return nil
}

// ApplyToFZFSlotBytes patches a slot and mirrors key ranges to every referencing bank site.
func ApplyToFZFSlotBytes(data []byte, slot int, patches []Edit) error {
	layout, err := fzutil.ResolveStandaloneFZFLayout(data)
	if err != nil {
		return fmt.Errorf("voiceedit: %w", err)
	}
	resolved, err := voicepatch.ResolveFZFSlot(data, layout, slot, patches)
	if err != nil {
		return fmt.Errorf("voiceedit: %w", err)
	}
	if err := model.Apply(data, resolved); err != nil {
		return fmt.Errorf("voiceedit: %w", err)
	}
	return nil
}

// resolvePatches exposes the header resolver to package tests.
func resolvePatches(data []byte, base int, patches []Edit) ([]model.Patch, error) {
	return voicepatch.ResolveHeader(data, base, patches)
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

// BuildLFOPatches preserves the phase bit beside the waveform. Delay and its derived attack belong to BuildLFODelayPatches; the panel can't edit resonance depth.
func BuildLFOPatches(wave, rate, pitch, amp, filter int, origLFOName uint8) ([]Edit, error) {
	var patches []Edit
	if wave != Unchanged {
		if err := ValidateWaveform(wave); err != nil {
			return nil, err
		}
		// Preserve phase sync in the shared waveform byte.
		val := uint8(wave)&disk.LFOWaveformMask | (origLFOName & disk.LFOPhaseFlag) //nolint:gosec // wave is validated above (0..5)
		patches = append(patches, Edit{Offset: disk.VoiceLFONameOffset, Size: 1, Value: uint16(val)})
	}
	type lfoParam struct {
		name   string
		val    int
		offset int
	}
	params := []lfoParam{
		{"lfo-rate", rate, disk.VoiceLFORateOffset},
		{"lfo-pitch", pitch, disk.VoiceLFODCPOffset},
		{"lfo-amp", amp, disk.VoiceLFODCAOffset},
		{"lfo-filter", filter, disk.VoiceLFODCFOffset},
	}
	for _, p := range params {
		if p.val != Unchanged {
			if err := ValidateByte(p.name, p.val, 0, 127); err != nil {
				return nil, err
			}
			patches = append(patches, Edit{Offset: p.offset, Size: 1, Value: bitconv.NarrowU16(p.val)})
		}
	}
	return patches, nil
}

// BuildModulationPatches uses panel scales and preserves fields set to Unchanged. See llm-wiki/topics/display-scales.md.
func BuildModulationPatches(dcaKF, dcaRS, dcfKF, dcfRS, velDCAKF, velDCFKF, velDCQKF, velDCARS, velDCFRS int) ([]Edit, error) {
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
	var patches []Edit
	for _, p := range kfParams {
		if p.val != Unchanged {
			if err := ValidateByte(p.name, p.val, disk.MinKFDisplay, disk.MaxKFDisplay); err != nil {
				return nil, err
			}
			patches = append(patches, Edit{Offset: p.offset, Size: 1, Value: uint16(disk.KFDisplayToByte(p.val))})
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
			lo := -127
			if p.name == "vel-dcq-kf" {
				lo = 0
			}
			if err := ValidateByte(p.name, p.val, lo, 127); err != nil {
				return nil, err
			}
			patches = append(patches, Edit{Offset: p.offset, Size: 1, Value: uint16(uint8(int8(p.val)))}) //nolint:gosec // G115: intentional two's complement conversion; value validated above
		}
	}
	return patches, nil
}

// BuildLFODelayPatches writes the delay and derived attack controlled by one panel row.
func BuildLFODelayPatches(display int) ([]Edit, error) {
	if display < 0 || display > disk.MaxLFODelayDisplay {
		return nil, fmt.Errorf("voiceedit: lfo-delay must be 0 to %d, got %d", disk.MaxLFODelayDisplay, display)
	}
	return []Edit{
		{Offset: disk.VoiceLFODelayOffset, Size: 2, Value: disk.LFODelayDisplayToWord(display)},
		{Offset: disk.VoiceLFOAtckOffset, Size: 1, Value: uint16(disk.LFOAttackForDelay(display))},
	}, nil
}

// BuildLFOSyncPatch changes phase sync without changing the waveform.
func BuildLFOSyncPatch(option string, origLFOName uint8) ([]Edit, error) {
	val := origLFOName & disk.LFOWaveformMask
	switch option {
	case "on":
		val |= disk.LFOPhaseFlag
	case "off":
	default:
		return nil, fmt.Errorf("voiceedit: lfo-sync must be on or off, got %q", option)
	}
	return []Edit{{Offset: disk.VoiceLFONameOffset, Size: 1, Value: uint16(val)}}, nil
}

// BuildFilterPatches uses the panel's full-byte cutoff and resonance scales.
func BuildFilterPatches(cutoff, resonance int) ([]Edit, error) {
	var patches []Edit
	if cutoff != Unchanged {
		if err := ValidateByte("cutoff", cutoff, 0, 127); err != nil {
			return nil, err
		}
		patches = append(patches, Edit{Offset: disk.VoiceDCFOffset, Size: 1, Value: bitconv.NarrowU16(cutoff)})
	}
	if resonance != Unchanged {
		if err := ValidateByte("resonance", resonance, 0, disk.MaxResonance); err != nil {
			return nil, err
		}
		patches = append(patches, Edit{Offset: disk.VoiceDCQOffset, Size: 1, Value: bitconv.NarrowU16(resonance)})
	}
	return patches, nil
}

// BuildNamePatch preserves case and writes the padded 14-byte name field.
func BuildNamePatch(name string) ([]Edit, error) {
	if len(name) > disk.LabelSize {
		return nil, fmt.Errorf("voiceedit: name %q exceeds %d characters", name, disk.LabelSize)
	}
	padded := disk.PadLabel(name)
	payload := make([]byte, disk.VoiceNameFieldSize)
	copy(payload, padded[:])
	return []Edit{{Offset: disk.VoiceNameOffset, Bytes: payload}}, nil
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

// BuildLoopPatch creates patches setting loop index's start and end
// sample addresses. The spec reserves flag bits inside both cells (the
// loop-fine byte in the upper 8 bits of loopst, the skip flag in the
// MSB of looped); origSt and origEd carry the current cell values so
// those bits survive the write. start must be below end, and end must
// fit the 24-bit loopst address space so the pair stays addressable.
func BuildLoopPatch(index int, start, end uint32, origSt, origEd uint32) ([]Edit, error) {
	if index < 0 || index >= disk.MaxGenerators {
		return nil, fmt.Errorf("voiceedit: loop index must be 0 to %d, got %d", disk.MaxGenerators-1, index)
	}
	if start >= end {
		return nil, fmt.Errorf("voiceedit: loop start %d must be below end %d", start, end)
	}
	if end > disk.LoopStartAddressMask {
		return nil, fmt.Errorf("voiceedit: loop end %d exceeds the address space (%d)", end, disk.LoopStartAddressMask)
	}
	newSt := (origSt &^ uint32(disk.LoopStartAddressMask)) | start
	newEd := (origEd &^ uint32(disk.LoopEndAddressMask)) | end
	stBytes := make([]byte, 4)
	edBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(stBytes, newSt)
	binary.LittleEndian.PutUint32(edBytes, newEd)
	return []Edit{
		{Offset: disk.VoiceLoopSt0Offset + index*4, Bytes: stBytes},
		{Offset: disk.VoiceLoopEd0Offset + index*4, Bytes: edBytes},
	}, nil
}

// BuildLoopAttrPatch creates patches for loop index's cross-fade and
// multi-loop time attributes (loopxf and looptm, spec §2-1). Both are
// 16-bit little-endian entries; xf ranges 0 to disk.MaxLoopXF (0
// disables the cross-fade) and tm ranges 0 to disk.MaxLoopTm (fresh
// voices carry 0, so 0 passes even though the spec's lower bound is 1).
func BuildLoopAttrPatch(index, xf, tm int) ([]Edit, error) {
	if index < 0 || index >= disk.MaxGenerators {
		return nil, fmt.Errorf("voiceedit: loop index must be 0 to %d, got %d", disk.MaxGenerators-1, index)
	}
	if xf < 0 || xf > disk.MaxLoopXF {
		return nil, fmt.Errorf("voiceedit: loop cross-fade must be 0 to %d, got %d", disk.MaxLoopXF, xf)
	}
	if tm < 0 || tm > disk.MaxLoopTm {
		return nil, fmt.Errorf("voiceedit: loop time must be 0 to %d, got %d", disk.MaxLoopTm, tm)
	}
	return []Edit{
		{Offset: disk.VoiceLoopXFOffset + index*disk.LoopXFEntrySize, Size: 2, Value: bitconv.NarrowU16(xf)},
		{Offset: disk.VoiceLoopTmOffset + index*disk.LoopTmEntrySize, Size: 2, Value: bitconv.NarrowU16(tm)},
	}, nil
}

// BuildLoopSelectPatch creates patches for the sustain and release
// loop designations (loop_sus, loop_end). Valid values are 0 to 7 for
// a loop index, or disk.NoSustainLoop (8) for none.
func BuildLoopSelectPatch(sustain, release int) ([]Edit, error) {
	if err := ValidateByte("loop-sustain", sustain, 0, disk.NoSustainLoop); err != nil {
		return nil, err
	}
	if err := ValidateByte("loop-release", release, 0, disk.NoSustainLoop); err != nil {
		return nil, err
	}
	return []Edit{
		{Offset: disk.VoiceLoopSusOffset, Size: 1, Value: bitconv.NarrowU16(sustain)},
		{Offset: disk.VoiceLoopEndOffset, Size: 1, Value: bitconv.NarrowU16(release)},
	}, nil
}

// BuildTunePatch creates a patch for the voice tuning (DCP field). The value
// is in 1/256-semitone units and stored as a uint16 (two's complement).
func BuildTunePatch(tune int) ([]Edit, error) {
	if tune < -32768 || tune > 32767 {
		return nil, fmt.Errorf("voiceedit: tune must be -32768 to 32767, got %d", tune)
	}
	return []Edit{{Offset: disk.VoiceDCPOffset, Size: 2, Value: uint16(int16(tune))}}, nil //nolint:gosec // validated above
}

// BuildKeyRangePatch creates patches for the key range (key-low, key-high,
// root). Each value is a MIDI note number (0 to 127). Pass Unchanged for
// any parameter to leave it unmodified.
func BuildKeyRangePatch(keyLow, keyHigh, root int) ([]Edit, error) {
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
	var patches []Edit
	for _, p := range params {
		if p.val != Unchanged {
			if err := ValidateByte(p.name, p.val, 0, disk.MaxMIDINote); err != nil {
				return nil, err
			}
			patches = append(patches, Edit{Offset: p.offset, Size: 1, Value: bitconv.NarrowU16(p.val)})
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
func BuildPlaybackModePatch(mode string) ([]Edit, error) {
	val, ok := playbackModes[strings.ToLower(mode)]
	if !ok {
		return nil, fmt.Errorf("voiceedit: unknown playback mode %q (use: normal, reverse, cue, synth)", mode)
	}
	return []Edit{{Offset: disk.VoiceLoopModeOffset, Size: 2, Value: val}}, nil
}

// BuildDCAPatches creates patches for DCA envelope parameters. Pass Unchanged
// for sustain/end, or for an individual rate/level element, to leave it alone.
// Rates and levels use the hardware display scale (0 to 99). origRates carries
// the original rate bytes so the sign bit (envelope direction) survives a
// magnitude-only change.
func BuildDCAPatches(sustain, end int, rates, stops [disk.EnvelopeStages]int, origRates [disk.EnvelopeStages]uint8) ([]Edit, error) {
	return buildEnvelopePatches("dca", sustain, end, rates, stops, origRates,
		disk.VoiceDCASusOffset, disk.VoiceDCAEndOffset,
		disk.VoiceDCARateOffset, disk.VoiceDCAStopOffset)
}

// BuildDCFPatches creates patches for DCF envelope parameters, under the same
// conventions as BuildDCAPatches: Unchanged skips a field or element, rates
// and levels use the 0 to 99 display scale, and origRates preserves the
// envelope-direction sign bit.
func BuildDCFPatches(sustain, end int, rates, stops [disk.EnvelopeStages]int, origRates [disk.EnvelopeStages]uint8) ([]Edit, error) {
	return buildEnvelopePatches("dcf", sustain, end, rates, stops, origRates,
		disk.VoiceDCFSusOffset, disk.VoiceDCFEndOffset,
		disk.VoiceDCFRateOffset, disk.VoiceDCFStopOffset)
}

func buildEnvelopePatches(prefix string, sustain, end int, rates, stops [disk.EnvelopeStages]int, origRates [disk.EnvelopeStages]uint8, susOff, endOff, rateOff, stopOff int) ([]Edit, error) {
	var patches []Edit
	if sustain != Unchanged {
		if err := ValidateByte(prefix+"-sustain", sustain, 0, 7); err != nil {
			return nil, err
		}
		patches = append(patches, Edit{Offset: susOff, Size: 1, Value: bitconv.NarrowU16(sustain)})
	}
	if end != Unchanged {
		if err := ValidateByte(prefix+"-end", end, 0, 7); err != nil {
			return nil, err
		}
		patches = append(patches, Edit{Offset: endOff, Size: 1, Value: bitconv.NarrowU16(end)})
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
			patches = append(patches, Edit{Offset: rateOff + i, Size: 1, Value: uint16(b)})
		}
		if stops[i] != Unchanged {
			if stops[i] < 0 || stops[i] > disk.DisplayMax {
				return nil, fmt.Errorf("voiceedit: %s-level-%d must be 0 to %d, got %d", prefix, i+1, disk.DisplayMax, stops[i])
			}
			patches = append(patches, Edit{Offset: stopOff + i, Size: 1, Value: uint16(disk.StopDisplayToByte(stops[i]))})
		}
	}
	return patches, nil
}
