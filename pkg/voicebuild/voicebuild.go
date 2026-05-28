// Package voicebuild implements the fizzle voice build command. It assembles
// up to 64 individual FZV voice files into a single FZF full data dump
// compatible with the FZ series of samplers.
package voicebuild

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/rs/zerolog/log"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fileutil"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
	"github.com/philipcunningham/fizzle/pkg/render"
)

// Sentinel errors. Wrap with %w; callers should use errors.Is to identify
// a specific failure mode rather than matching error message substrings.
var (
	ErrNoVoices      = errors.New("voicebuild: no voice files provided")
	ErrTooManyVoices = errors.New("voicebuild: too many voices")
)

// defaultBankName is the 12-byte name written into a generated bank sector.
// The trailing spaces are load-bearing: the FZ label field is exactly 12 bytes,
// space-padded. Do not trim them.
const defaultBankName = "All Voices  "

// defaultEffectData is the 24-byte global effect block (struct efectdata)
// written at disk.BankEffectOffset in the bank sector. It controls how the
// sampler routes performance controllers (pitch bend, mod wheel, foot pedal,
// aftertouch) to the synthesis engine for every voice in the bank.
//
// Values confirmed against Jacob Vosmaer's reference implementation
// (https://github.com/jacobvosmaer/fz1).
//
// Byte map (zero-indexed within the 24-byte block):
//
//	[0]  bend        pitch-bend range, in 1/8-semitone units (0x18 = 24 = ±3 semitones)
//	[1]  reserved / 0
//	[2]  reserved / 0
//	[3]  mod_lfp     mod wheel to LFO pitch depth (0x0f = 15)
//	[4-13] reserved / 0   (additional controller routings the spec describes
//	                       but which fizzle does not currently expose)
//	[14] fot_dca     foot pedal to DCA (volume) depth (0x40 = 64)
//	[15-16] reserved / 0
//	[17] aft_lfp     aftertouch to LFO pitch depth (0x08 = 8)
//	[18-23] reserved / 0
//
// The `fzf effects` command (see pkg/fzfeffects/) can modify these values.
// The four named fields above are the ones verified on hardware; the
// "reserved / 0" slots correspond to additional routings in struct efectdata
// whose semantics have not been independently confirmed.
var defaultEffectData = [disk.EffectDataSize]byte{
	0x18, 0x00, 0x00, 0x0f, 0x00, 0x00, 0x00, 0x00, // bend=24 (0x00), mod_lfp=15 (0x03)
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40, 0x00, // fot_dca=64 (0x0E)
	0x00, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // aft_lfp=8 (0x11)
}

// Keygroup describes the per-voice mapping in a bank sector: which keys and
// velocities trigger the voice, what the root key is, and which voice
// generators handle it.
//
// AudioOut is the gchn bitmask: 0xff means all 8 generators (polyphonic).
// A single-bit value like 0x01 assigns one generator (monophonic: new note
// cuts the previous one). Voices sharing the same single-bit AudioOut value
// will mute each other, implementing SFZ mutegroup behaviour.
//
// The Go zero value of Keygroup is unsafe to load on real hardware:
// VelLow=0 disables note-on triggering, and AudioOut=0 routes the voice to
// no generators. Construct via NewKeygroup to get the hardware defaults.
type Keygroup struct {
	KeyLow    uint8
	KeyHigh   uint8
	VelLow    uint8
	VelHigh   uint8
	KeyCentre uint8
	MIDIChan  uint8
	AudioOut  uint8 // gchn bitmask; 0 means use default (0xff = polyphonic)
}

// NewKeygroup returns a Keygroup with the given key range and root note,
// the standard 1..127 velocity range, and polyphonic audio routing
// (AudioOut = 0xff). MIDI channel defaults to 0; callers that need
// per-voice channel routing or a monophonic mutegroup assignment should
// override the relevant fields after construction.
func NewKeygroup(keyLow, keyHigh, keyCentre uint8) Keygroup {
	return Keygroup{
		KeyLow:    keyLow,
		KeyHigh:   keyHigh,
		KeyCentre: keyCentre,
		VelLow:    disk.DefaultVelLow,
		VelHigh:   disk.DefaultVelHigh,
		AudioOut:  disk.PolyphonicAudioOut,
	}
}

// AssembleWithKeygroups builds an FZF byte slice from decoded voice data and
// an explicit keygroup mapping. len(voices) must equal len(groups).
func AssembleWithKeygroups(voices [][]byte, groups []Keygroup) ([]byte, error) {
	if len(voices) != len(groups) {
		return nil, fmt.Errorf("voicebuild: %d voices but %d keygroups", len(voices), len(groups))
	}
	return assembleWithGroups(voices, groups)
}

// ErrTooManyDisks is returned when an instrument's audio is too large to fit
// across the two floppy disks the hardware supports.
type ErrTooManyDisks struct {
	TotalAudioBytes int
	CapacityBytes   int
}

// ErrSampleRAMExceeded is returned when the total audio across all disks
// exceeds the hardware's 2 MB sample RAM. The sampler reports "no memory
// space" when loading a full dump that exceeds this limit.
type ErrSampleRAMExceeded struct {
	TotalAudioBytes int
}

// Error returns a message describing the over-capacity instrument.
func (e *ErrTooManyDisks) Error() string {
	over := e.TotalAudioBytes - e.CapacityBytes
	return fmt.Sprintf("instrument audio is %s but 2 disks hold %s (%s over the limit)",
		render.FormatBytes(e.TotalAudioBytes),
		render.FormatBytes(e.CapacityBytes),
		render.FormatBytes(over),
	)
}

// Error returns a message describing the over-RAM instrument.
func (e *ErrSampleRAMExceeded) Error() string {
	over := e.TotalAudioBytes - disk.MaxSampleRAM
	return fmt.Sprintf("instrument audio is %s but the sampler has %s of sample RAM (%s over the limit)",
		render.FormatBytes(e.TotalAudioBytes),
		render.FormatBytes(disk.MaxSampleRAM),
		render.FormatBytes(over),
	)
}

// MultiDiskResult holds the data for a 2-disk full dump.
// Disks[0] is a complete FZF (bank + voice area + partial audio).
// Disks[1] is pure audio continuation (no bank or voice headers).
// The DIS tail values are the same for both disks.
type MultiDiskResult struct {
	Disks      [][]byte // always len 2: [disk1FZF, disk2Audio]
	BankCount  int
	VoiceCount int
	WaveCount  int
}

// AssembleMultiDisk splits a full dump across 2 floppy disks.
// The FZ series has 2 MB of sample RAM, so two disks is the hardware maximum.
//
// The hardware loads disk 1 first (bank sector, voice headers for ALL voices,
// and as much audio as fits), then loads disk 2 as pure audio continuation.
// Disk 2 has no bank sector or voice headers; the sampler appends its audio
// into RAM immediately after disk 1's audio. Voice header pointers on disk 1
// use absolute word addresses that span both disks.
//
// The DIS tail values (bn, vn, wn) are identical on both disks and reflect
// the total instrument size.
func AssembleMultiDisk(voices [][]byte, groups []Keygroup) (MultiDiskResult, error) {
	if len(voices) != len(groups) {
		return MultiDiskResult{}, fmt.Errorf("voicebuild: %d voices but %d keygroups", len(voices), len(groups))
	}
	if len(voices) > disk.MaxVoices {
		return MultiDiskResult{}, fmt.Errorf("%w (%d, max %d)", ErrTooManyVoices, len(voices), disk.MaxVoices)
	}

	fzf, err := assembleWithGroups(voices, groups)
	if err != nil {
		return MultiDiskResult{}, err
	}

	// The maximum FZF data that fits on one disk, accounting for the DIS
	// sector that diskadd allocates alongside the data.
	maxDisk1 := disk.UsableDataSize - disk.SectorSize
	maxDisk1 = (maxDisk1 / disk.SectorSize) * disk.SectorSize

	if len(fzf) <= maxDisk1 {
		return MultiDiskResult{}, fmt.Errorf("voicebuild: all voices fit on one disk; use AssembleWithKeygroups instead")
	}

	// Disk 2 also needs a DIS sector, so its max data capacity is the same.
	maxDisk2 := maxDisk1
	overflow := len(fzf) - maxDisk1
	if overflow > maxDisk2 {
		return MultiDiskResult{}, &ErrTooManyDisks{
			TotalAudioBytes: len(fzf),
			CapacityBytes:   maxDisk1 + maxDisk2,
		}
	}

	// Check total audio against the hardware's 2 MB sample RAM limit.
	n := len(voices)
	audioBlocks, err := buildAudioBlocks(voices)
	if err != nil {
		return MultiDiskResult{}, err
	}
	totalAudioBytes := 0
	totalWaveSectors := 0
	for _, b := range audioBlocks {
		totalAudioBytes += len(b)
		totalWaveSectors += len(b) / disk.SectorSize
	}
	if totalAudioBytes > disk.MaxSampleRAM {
		return MultiDiskResult{}, &ErrSampleRAMExceeded{TotalAudioBytes: totalAudioBytes}
	}

	// Stamp the total wave sector count into the bank sector so that
	// diskadd writes the correct DIS wn value for disk 1. This tells the
	// sampler that more audio is coming on disk 2.
	binary.LittleEndian.PutUint32(fzf[disk.BankTotalWaveOffset:], bitconv.NarrowU32(totalWaveSectors))

	disk1Data := fzf[:maxDisk1]
	disk2Data := fzf[maxDisk1:]

	log.Info().
		Int("total_voices", n).
		Int("disks", 2).
		Msg("multi-disk split")

	log.Info().
		Int("disk", 1).
		Str("size", render.FormatBytes(len(disk1Data))).
		Msg("assembled disk")

	log.Info().
		Int("disk", 2).
		Str("size", render.FormatBytes(len(disk2Data))).
		Msg("assembled disk")

	return MultiDiskResult{
		Disks:      [][]byte{disk1Data, disk2Data},
		BankCount:  1,
		VoiceCount: n,
		WaveCount:  totalWaveSectors,
	}, nil
}

// Build assembles the voice files at voicePaths into a full dump written to
// outputPath. The output is written atomically. The context is checked
// between voices so callers can cancel a long build with Ctrl+C.
func Build(ctx context.Context, outputPath string, voicePaths []string) error {
	if len(voicePaths) == 0 {
		return ErrNoVoices
	}
	if len(voicePaths) > disk.MaxVoices {
		return fmt.Errorf("%w (%d, max %d)", ErrTooManyVoices, len(voicePaths), disk.MaxVoices)
	}

	voices := make([][]byte, len(voicePaths))
	for i, p := range voicePaths {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("voicebuild: %w", err)
		}
		log.Debug().
			Str("file", p).
			Str("progress", fmt.Sprintf("%d/%d", i+1, len(voicePaths))).
			Msg("reading voice")
		data, err := fzutil.ReadFZV(p)
		if err != nil {
			return fmt.Errorf("voicebuild: reading %q: %w", p, err)
		}
		voices[i] = data
	}
	log.Info().
		Int("voices", len(voicePaths)).
		Str("output", filepath.Base(outputPath)).
		Msg("building full dump")

	out, err := assemble(voices)
	if err != nil {
		return err
	}
	if fzutil.OverCapacity(len(out)) {
		log.Warn().
			Str("size", render.FormatBytes(len(out))).
			Str("limit", render.FormatBytes(disk.UsableDataSize)).
			Msg("voice data exceeds floppy disk capacity")
	}
	if err := fileutil.WriteAtomic(outputPath, out); err != nil {
		return fmt.Errorf("voicebuild: %w", err)
	}
	log.Info().
		Str("size", render.FormatBytes(len(out))).
		Msg("full dump written")
	return nil
}

// assemble builds the FZF byte slice using a default chromatic keygroup mapping.
func assemble(voices [][]byte) ([]byte, error) {
	if len(voices) > disk.MaxVoices {
		return nil, fmt.Errorf("%w (%d, max %d)", ErrTooManyVoices, len(voices), disk.MaxVoices)
	}
	groups := make([]Keygroup, len(voices))
	for i := range voices {
		note := uint8(disk.FirstMIDINote + i)
		groups[i] = NewKeygroup(note, note, note)
	}
	return assembleWithGroups(voices, groups)
}

// buildAudioBlocks extracts the audio from each voice, pads each block to a
// sector boundary, and returns the padded blocks in order.
func buildAudioBlocks(voices [][]byte) ([][]byte, error) {
	blocks := make([][]byte, len(voices))
	for i, v := range voices {
		if len(v) < disk.SectorSize {
			return nil, fmt.Errorf("voicebuild: voice %d too small (%d bytes, need %d)", i, len(v), disk.SectorSize)
		}
		audio := v[disk.SectorSize:]
		block := make([]byte, disk.PadToSector(len(audio)))
		copy(block, audio)
		blocks[i] = block
	}
	return blocks, nil
}

// buildBankSector constructs a 1024-byte bank sector for n voices using the
// provided keygroup mappings. The bank name and effect data use package defaults.
//
// Returns an error if any keygroup has an out-of-range or inconsistent key
// mapping: KeyLow > KeyHigh, KeyCentre outside [KeyLow, KeyHigh], or any of
// the three exceeding disk.MaxMIDINote. Writing such values would produce a
// bank the hardware silently mis-maps, so we fail early with a descriptive
// error rather than emit an incoherent dump.
//
// voiceNames is optional metadata for error messages; pass nil to omit names.
func buildBankSector(n int, groups []Keygroup, voiceNames []string) ([]byte, error) {
	bank := make([]byte, disk.SectorSize)
	binary.LittleEndian.PutUint16(bank[disk.BankVoiceCountOffset:disk.BankVoiceCountOffset+2], bitconv.NarrowU16(n))
	for i, g := range groups {
		if err := validateKeygroup(i, g, voiceNames); err != nil {
			return nil, err
		}
		bank[disk.BankKeyHighOffset+i] = g.KeyHigh       //nolint:gosec // G602: offset + i bounded by MaxVoices (64), within SectorSize (1024)
		bank[disk.BankKeyLowOffset+i] = g.KeyLow         //nolint:gosec // G602
		bank[disk.BankVelHighOffset+i] = g.VelHigh       //nolint:gosec // G602
		bank[disk.BankVelLowOffset+i] = g.VelLow         //nolint:gosec // G602
		bank[disk.BankKeyCentOffset+i] = g.KeyCentre     //nolint:gosec // G602
		bank[disk.BankMIDIRecvChanOffset+i] = g.MIDIChan //nolint:gosec // G602
		audioOut := g.AudioOut
		if audioOut == 0 {
			audioOut = disk.PolyphonicAudioOut
		}
		bank[disk.BankAudioOutOffset+i] = audioOut //nolint:gosec // G602
		// bvol: per-voice mix volume (spec §2-2). Factory Casio dumps use
		// 0..~27 and we default to 0; field semantics are not fully
		// understood; see the DefaultBankVolume comment in pkg/disk.
		bank[disk.BankVolumeOffset+i] = disk.DefaultBankVolume //nolint:gosec // G602
		binary.LittleEndian.PutUint16(bank[disk.BankVoiceNumOffset+2*i:], uint16(i))
	}
	copy(bank[disk.BankNameOffset:], defaultBankName)
	copy(bank[disk.BankEffectOffset:], defaultEffectData[:])
	return bank, nil
}

// extractVoiceNames pulls the display name from each voice header so
// bank-validation errors can reference the offending voice by name. Voices
// shorter than the name offset are skipped (empty string).
func extractVoiceNames(voices [][]byte) []string {
	names := make([]string, len(voices))
	for i, v := range voices {
		if len(v) < disk.VoiceNameOffset+disk.LabelSize {
			continue
		}
		names[i] = disk.TrimPadded(v[disk.VoiceNameOffset : disk.VoiceNameOffset+disk.LabelSize])
	}
	return names
}

// validateKeygroup checks that a Keygroup has a coherent key mapping before
// it is written to a bank sector. See buildBankSector for the rationale.
//
// Range overflow (any field > MaxMIDINote) and KeyLow > KeyHigh are hard
// errors: the hardware silently mis-maps such banks. KeyCentre outside
// [KeyLow, KeyHigh] is only a warning, because real SFZ corpora (e.g.
// JUNGLISM) legitimately use pitch_keycenter to transpose a sample beyond
// its key range and the hardware DCP handles it fine.
func validateKeygroup(idx int, g Keygroup, voiceNames []string) error {
	label := fmt.Sprintf("voice %d", idx+1)
	if idx < len(voiceNames) && voiceNames[idx] != "" {
		label = fmt.Sprintf("voice %d (%s)", idx+1, voiceNames[idx])
	}
	if g.KeyLow > disk.MaxMIDINote {
		return fmt.Errorf("voicebuild: %s: KeyLow=%d exceeds MaxMIDINote=%d", label, g.KeyLow, disk.MaxMIDINote)
	}
	if g.KeyHigh > disk.MaxMIDINote {
		return fmt.Errorf("voicebuild: %s: KeyHigh=%d exceeds MaxMIDINote=%d", label, g.KeyHigh, disk.MaxMIDINote)
	}
	if g.KeyCentre > disk.MaxMIDINote {
		return fmt.Errorf("voicebuild: %s: KeyCentre=%d exceeds MaxMIDINote=%d", label, g.KeyCentre, disk.MaxMIDINote)
	}
	if g.KeyLow > g.KeyHigh {
		return fmt.Errorf("voicebuild: %s: KeyLow=%d > KeyHigh=%d", label, g.KeyLow, g.KeyHigh)
	}
	if g.KeyCentre < g.KeyLow || g.KeyCentre > g.KeyHigh {
		log.Warn().
			Str("voice", label).
			Uint8("KeyLow", g.KeyLow).
			Uint8("KeyHigh", g.KeyHigh).
			Uint8("KeyCentre", g.KeyCentre).
			Msg("KeyCentre outside [KeyLow, KeyHigh]; sample will be transposed")
	}
	return nil
}

// buildVoiceArea packs voice headers into a ceil(n/4)-sector area with
// sample pointer fields offset by priorSamples, the cumulative sample count
// of all audio preceding these voices in the combined wave area.
//
// The per-voice key range bytes (hwid/lwid/cent at offsets 0xae/0xaf/0xb0,
// spec §2-1) are overwritten from the matching Keygroup so that voice-header
// playback metadata stays in sync with the bank's split-mapping arrays
// (spec §2-2). Without this, voices imported via voiceimport.Encode would
// keep their DefaultKeyHigh/Low/Centre values in the FZF, and voiceunpack /
// fzv info would later display those stale defaults instead of the keygroup
// the caller specified. This is the voicebuild counterpart to the F11
// sfzconvert.regionToFZVFromFile cent fix (commit 699341d), broadened to
// cover all three bytes and all callers (fzf build, sfz convert).
func buildVoiceArea(voices [][]byte, audioBlocks [][]byte, groups []Keygroup, priorSamples int) []byte {
	n := len(voices)
	voiceSectors := disk.VoiceAreaSectors(n)
	area := make([]byte, voiceSectors*disk.SectorSize)
	audioOffset := priorSamples
	for i, v := range voices {
		hdr := make([]byte, disk.SectorSize)
		copy(hdr, v[:disk.SectorSize])
		fixSampleOffsets(hdr, audioOffset)
		hdr[disk.VoiceKeyHighOffset] = groups[i].KeyHigh
		hdr[disk.VoiceKeyLowOffset] = groups[i].KeyLow
		hdr[disk.VoiceKeyCentOffset] = groups[i].KeyCentre
		dest := disk.VoiceSlotOffset(0, i)
		copy(area[dest:dest+disk.VoiceHeaderUsed], hdr[:disk.VoiceHeaderUsed])
		audioOffset += len(audioBlocks[i]) / disk.BytesPerSample // bytes to samples (16-bit mono)
	}
	return area
}

// assembleWithGroups builds the FZF byte slice from decoded voice data and
// explicit keygroup mappings.
func assembleWithGroups(voices [][]byte, groups []Keygroup) ([]byte, error) {
	if len(voices) > disk.MaxVoices {
		return nil, fmt.Errorf("%w (%d, max %d)", ErrTooManyVoices, len(voices), disk.MaxVoices)
	}
	n := len(voices)

	audioBlocks, err := buildAudioBlocks(voices)
	if err != nil {
		return nil, err
	}
	bank, err := buildBankSector(n, groups, extractVoiceNames(voices))
	if err != nil {
		return nil, err
	}
	voiceArea := buildVoiceArea(voices, audioBlocks, groups, 0)

	// Pre-size the output to the exact final length so the buffer never grows.
	total := len(bank) + len(voiceArea)
	for _, block := range audioBlocks {
		total += len(block)
	}
	out := make([]byte, 0, total)
	out = append(out, bank...)
	out = append(out, voiceArea...)
	for _, block := range audioBlocks {
		out = append(out, block...)
	}
	return out, nil
}

// fixSampleOffsets adjusts the sample pointer fields in a voice header so they
// are relative to the combined wave data area rather than the individual file.
// offsetSamples is the number of samples preceding this voice in the combined
// wave area.
//
// Loop-pointer fields reserve flag bits the address adjustment must not
// disturb (spec §2-1: loopst[i] upper 8 bits = loop-fine, looped[i] MSB =
// skip flag). For those fields we mask out the address, add the offset, and
// OR the preserved flag bits back in before writing. Real-world FZ-1 voices
// produced by the sampler usually leave these flag bits zero, but third-
// party files (or files produced by an FZ-1 with loop-fine adjustments)
// carry non-zero values that previously got corrupted on round-trip.
func fixSampleOffsets(voice []byte, offsetSamples int) {
	off := bitconv.NarrowU32(offsetSamples)
	disk.ForEachSamplePointer(voice, func(field []byte, kind disk.SamplePointerKind) {
		v := binary.LittleEndian.Uint32(field)
		switch kind {
		case disk.WavePointer:
			binary.LittleEndian.PutUint32(field, v+off)
		case disk.LoopStartPointer:
			addr := disk.LoopStartAddress(v) + off
			fine := v & ^uint32(disk.LoopStartAddressMask)
			binary.LittleEndian.PutUint32(field, (addr&disk.LoopStartAddressMask)|fine)
		case disk.LoopEndPointer:
			addr := disk.LoopEndAddress(v) + off
			skip := v & disk.LoopEndSkipMask
			binary.LittleEndian.PutUint32(field, (addr&disk.LoopEndAddressMask)|skip)
		}
	})
}
