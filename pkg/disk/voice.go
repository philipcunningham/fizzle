package disk

import (
	"encoding/binary"
	"strconv"
	"strings"
)

// Voice header layout offsets and limits.
const (
	// Voice header field offsets.
	VoiceNameOffset    = 0xb2
	VoiceSampOffset    = 0xb1
	VoicePackSize      = 256
	VoiceHeaderUsed    = 192
	MaxVoices          = 64
	VoicesPerSector    = 4
	VoiceNameFieldSize = 14

	// Wave and generator pointer offsets.
	VoiceWaveStartOffset = 0x00
	VoiceWaveEndOffset   = 0x04
	VoiceGenStartOffset  = 0x08
	VoiceGenEndOffset    = 0x0c

	// Loop configuration offsets.
	VoiceLoopModeOffset = 0x10
	VoiceLoopSusOffset  = 0x12
	VoiceLoopEndOffset  = 0x13
	VoiceLoopSt0Offset  = 0x14
	VoiceLoopEd0Offset  = 0x34
	// VoiceLoopXFOffset is the start of loopxf[8] (spec §2-1, cross-fade
	// loop duration). Each entry is 2 bytes little-endian with valid range
	// 0-1023; 0 disables cross-fade.
	VoiceLoopXFOffset = 0x54
	// VoiceLoopTmOffset is the start of looptm[8] (spec §2-1, multi-loop
	// duration). Each entry is 2 bytes little-endian with valid range
	// 1-1022; the duration step is 16 ms.
	VoiceLoopTmOffset = 0x64
	// LoopXFEntrySize / LoopTmEntrySize are the per-index strides of the
	// loopxf[] and looptm[] arrays.
	LoopXFEntrySize = 2
	LoopTmEntrySize = 2

	// Envelope offsets (DCA and DCF).
	//
	// Both envelopes have the same 8-stage structure: a sustain point (Sus),
	// an end point (End), and per-stage Rate + Stop level arrays. On the
	// hardware front panel these are labelled RATE 1..8 and LEVEL 1..8
	// (called "stop level" in struct voicedata). Stages 0..Sus run on
	// note-on; stages Sus+1..End run on note-off. The hardware displays
	// rates and stop levels on a 0-99 scale; the stored bytes are 0-127 for
	// rates and 0-255 for stop levels (see Rate*/Stop* conversion helpers
	// below).
	//
	// DCA controls amplitude; DCF controls filter cutoff over time.
	//
	// All 8 DCA and 8 DCF stages are storage-backed, user-editable,
	// and audible.
	//
	// The edit screen at F000:54ED in the disassembled firmware ROM
	// drives a shared parameter cluster, configured asymmetrically
	// (raw asm at F000:550F / F000:553A):
	//
	//   DCA arm: [0x0486] = 6 rows, [0x048A] = 2 scroll-top offset.
	//   DCF arm: [0x0486] = 8 rows, [0x048A] = 0 scroll-top offset.
	//
	// The cluster has 8 parameter rows (CUTOFF FREQ, RESONANCE, RATE
	// KF, LEVEL KF, STEP @, RATE @, LEVEL @, COPY FROM). DCF shows
	// all 8; DCA scrolls past the two filter-only rows (CUTOFF FREQ,
	// RESONANCE) and shows the remaining 6. STEP @ selects which
	// envelope stage (0..7) RATE @ and LEVEL @ then edit.
	//
	// Synthesis is driven by the per-tick voice service at F000:1CD8.
	// The dispatcher at F000:1DA2 computes BX = (voicestate & 7) * 2
	// and CALLs through the 8-entry table at CS:0x1DC8. Table entry
	// values:
	//
	//   state 0, 4 -> F000:1DD8  (pitch / LFO target)
	//   state 1    -> F000:1FE0  (state-machine inquire)
	//   state 2, 6 -> F000:2173  (DCF cutoff target)
	//   state 3, 7 -> F000:2039  (DCA target)
	//   state 5    -> F000:225B  (DCF resonance / DCQ target)
	//
	// The DCA-target handler at F000:2039 (states 3 and 7) indexes
	// the full 8-entry dca_rate[] and dca_stop[] via the runtime
	// voicestate block at [0x5420 + voice_idx * 0x3A]; it then writes
	// newdca[index] for the timer-IRQ DCA slew at F000:0A49 to
	// consume. The state machine has no awareness of the editor's
	// row count.
	//
	// F000:3E30 (1889 bytes) is the maths pipeline that prepares the
	// edit-screen envelope visualisation. It performs no LCD writes
	// itself; the actual plotting happens in F000:4591, which the
	// maths pipeline invokes at F000:43F2. Neither function drives
	// synthesis, so chasing audibility questions through either is a
	// dead end.
	VoiceDCPOffset     = 0x74
	VoiceDCFOffset     = 0x76
	VoiceDCQOffset     = 0x77
	VoiceDCASusOffset  = 0x78
	VoiceDCAEndOffset  = 0x79
	VoiceDCARateOffset = 0x7a
	VoiceDCAStopOffset = 0x82
	VoiceDCFSusOffset  = 0x8a
	VoiceDCFEndOffset  = 0x8b
	VoiceDCFRateOffset = 0x8c
	VoiceDCFStopOffset = 0x94

	// LFO offsets.
	//
	// The LFO modulates pitch, amplitude, filter cutoff, and resonance, each
	// with its own depth byte. lfo_name selects the waveform and carries the
	// phase-sync flag in its top bit (see LFOWaveformMask / LFOPhaseFlag).
	VoiceLFODelayOffset = 0x9c
	VoiceLFONameOffset  = 0x9e
	VoiceLFOAtckOffset  = 0x9f
	VoiceLFORateOffset  = 0xa0
	VoiceLFODCPOffset   = 0xa1
	VoiceLFODCAOffset   = 0xa2
	VoiceLFODCFOffset   = 0xa3
	VoiceLFODCQOffset   = 0xa4

	// Modulation routing offsets.
	VoiceVelDCQKFOffset = 0xa5
	VoiceDCAKFOffset    = 0xa6
	VoiceDCARSOffset    = 0xa7
	VoiceDCFKFOffset    = 0xa8
	VoiceDCFRSOffset    = 0xa9
	VoiceVelDCAKFOffset = 0xaa
	VoiceVelDCARSOffset = 0xab
	VoiceVelDCFKFOffset = 0xac
	VoiceVelDCFRSOffset = 0xad

	// Key mapping offsets.
	VoiceKeyHighOffset = 0xae
	VoiceKeyLowOffset  = 0xaf
	VoiceKeyCentOffset = 0xb0

	// Sample pointer range bounds for ForEachSamplePointer.
	//
	// LoopPointerRangeEnd marks the end of the loopst/looped address arrays
	// only (loopst[8] at 0x14..0x33, looped[8] at 0x34..0x53). The two
	// arrays that immediately follow (loopxf[8] (0x54..0x63, crossfade
	// times) and looptm[8] (0x64..0x73, loop times)) are also loop-related
	// but hold scalar values rather than sample-address pointers, so they
	// are not scanned by ForEachSamplePointer. LoopXfStart and LoopTmStart
	// are provided for callers that need to address those fields directly.
	WavePointerRangeStart = 0x00
	WavePointerRangeEnd   = 0x10
	LoopPointerRangeStart = 0x14
	LoopPointerRangeEnd   = 0x54
	LoopXfStart           = 0x54
	LoopTmStart           = 0x64

	// Loop-pointer flag-bit masks (spec §2-1):
	//   "Upper 8 bits for loopst are used for loop fine and take a number
	//    among 0 - 255."
	//   "The MSB for looped is used for loop patterns; 1 for Skip, 0 for
	//    Trace."
	// The address bits occupy the remaining 24 (loopst) or 31 (looped) bits.
	LoopStartFineShift   = 24
	LoopStartAddressMask = 0x00FFFFFF
	LoopEndSkipMask      = 0x80000000
	LoopEndAddressMask   = 0x7FFFFFFF

	// Rate byte sign-magnitude layout. Bit 7 is the sign bit (0 = rising,
	// 1 = falling). Bits 0 through 6 carry the magnitude (0 to 127).
	//
	// The FZ-10M front panel displays rate magnitude on a 0 to 99 scale and
	// does not show the sign bit. The direction is implicit from the level
	// transitions. This was verified by loading test images (SIGNBIT-A,
	// SIGNBIT-B) that differ only in their sign bits: the hardware displayed
	// identical rate values for both.
	//
	// Hardware display scaling (validated against FZ-10M):
	//   rate display  = (magnitude * 100) >> 7
	//   level display = ceil(byte * 99 / 255)
	RateSignBit = 0x80
	RateMagMask = 0x7f

	// LFO name byte layout. Bits 0 through 6 select the waveform index;
	// bit 7 is the phase sync flag.
	LFOWaveformMask = 0x7f
	LFOPhaseFlag    = 0x80
)

// Bank sector layout offsets and limits.
//
// Two strides apply:
//
//   - On disk, each bank occupies a full 1024-byte (SectorSize)
//     sector. fizzle uses this stride throughout.
//   - In firmware RAM, the active bank record is 0x290 bytes (656).
//     The FZ-1 ROM walks bank records at base [0x0E08] with stride
//     0x290 (raw asm at F000:6338 confirms the stride and base).
//     Two of the BRK 3 dispatch entries use it: F000:63C2 (load
//     screen) and F000:6E7C (save screen). The stride covers
//     everything up to and including the bank name (12-char name
//     plus 2-byte terminator ends at exactly 0x290) and stops short
//     of the effect block at +0x3C0.
//
// Bytes 0x290 to 0x3BF of a bank sector are outside the active-bank-
// record stride. The multi-disk firmware stamps the total wave
// sector count across both disks of a 2-disk full dump into this
// region at BankTotalWaveOffset.
//
// The effect block at BankEffectOffset = 0x3C0 (24 bytes,
// struct effectdata) round-trips to the live RAM copy at [0x5288]
// via two mirror helpers in the disassembled firmware:
//
//   - F000:C626 LOAD: copies 0x18 bytes from header+0x3C0 (sector
//     buffer) into [0x5288] via REP MOVSW at F000:C660. Called
//     during disk load from F000:B3DA and F000:B4C8.
//   - F000:C8A6 SAVE: the mirror direction; copies [0x5288] into
//     header+0x3C0 of the save staging buffer when save kind is 0
//     or 3.
//
// So writes to BankEffectOffset survive a save-then-load round-trip
// and reach the synthesis path. Boot init at F000:0860 writes 0x18
// into [0x5288] (raw asm: MOV byte ptr [0x5288], 0x18), giving the
// default bender depth of 24 * 1/8 = 3 semitones.
const (
	// BankVoiceCountOffset holds the per-bank bstep field. Spec
	// section 2-2 calls it a voice count; the disassembled firmware
	// uses it as the count of vp[] entries the bank consumes (one
	// per key split). vp[] entries may repeat (one voice covering
	// several splits), so bstep is not the count of unique voices.
	// Empirically verified across all 530 banks in the test corpus:
	// vp[bstep..63] is always zero, and ~23% of banks carry at least
	// one duplicate vp[] entry.
	//
	// Read as a 16-bit little-endian word. The load screen at
	// F000:63C2 reads it via:
	//
	//   F000:63C6  ff 36 00 0e     PUSH word [0x0E00]    ; seg
	//   F000:63CA  ff 36 fe 0d     PUSH word [0x0DFE]    ; off
	//   F000:63CE  8f 46 fa        POP  word [BP-6]      ; far ptr lo
	//   F000:63D1  8f 46 fc        POP  word [BP-4]      ; far ptr hi
	//   F000:63E5  c4 5e fa        LES BX, [BP-6]        ; chase
	//   F000:63E8  26 8b 07        MOV AX, word ptr ES:[BX]
	//   F000:63EB  40              INC AX
	//   F000:63EC  a3 88 04        MOV [0x0488], AX
	//   F000:63EF  83 3e 88 04 40  CMP word ptr [0x0488], 0x40
	//
	// The load screen reads the far pointer [0x0DFE]:[0x0E00] from
	// RAM itself (the caller pushes nothing); the producer of that
	// RAM pair is F000:6338 (the bank-record picker), which writes
	// MOV [0x0DFE], BX and MOV [0x0E00], DS at F000:6383 / F000:6387.
	//
	// The +1 then clamp-to-64 post-processing makes
	// [0x0488] = min(disk_bstep+1, 64). fizzle writes
	// bstep = voice_count (see voicebuild and studio/model for the
	// single-voice wrappers). Real Casio factory disks round-trip
	// correctly under this convention, so [0x0488] is not a literal
	// voice count for the load screen: it is a "highest index
	// inclusive" derived value used to size the CKPLAY-style
	// listing.
	//
	// Writers of this offset must follow the established convention
	// (bstep = voice_count). Any future path that adds or removes
	// voice slots must rewrite bstep accordingly; the type system
	// does not enforce this.
	BankVoiceCountOffset = 0x00
	BankNameOffset       = 0x282
	// BankTotalWaveOffset sits at exactly the firmware's in-RAM bank
	// stride boundary (0x290 = 656). The FZ-1 ROM treats bytes from
	// 0x290 to 0x3BF as padding for the active bank record, so the
	// multi-disk firmware uses this region to stamp the total wave
	// sector count across both disks of a 2-disk full dump. See
	// pkg/fzfinfo for the split detection that reads this marker.
	BankTotalWaveOffset    = 0x290
	BankMIDIRecvChanOffset = 0x142
	MaxBanks               = 8
	BankKeyHighOffset      = 0x02
	BankKeyLowOffset       = 0x42
	BankVelHighOffset      = 0x82
	BankVelLowOffset       = 0xc2
	BankKeyCentOffset      = 0x102
	BankEffectOffset       = 0x3c0
	BankAudioOutOffset     = 0x182
	// BankVolumeOffset is the start of the bvol[64] per-voice volume array
	// in the bank sector (spec section 2-2, unsigned short bvol[MAXV],
	// range 0-127). Each entry is one byte (HP 64000 short).
	BankVolumeOffset = 0x1c2
	// DefaultBankVolume is the per-voice bank volume written by voicebuild.
	// Spec §2-2 calls bvol "sound volume" (0-127). We don't fully understand
	// the field: factory Casio dumps sit in 0..27, and on a real FZ-10M a
	// disk with bvol=127 plays much quieter than one with bvol=0, yet
	// changing the per-area volume in the on-instrument bank editor has no
	// audible effect. That rules out a plain continuous attenuator at this
	// offset; the field may be consulted only at load time, or the editor
	// may target a different byte. We default to 0 because that matches
	// factory disks and produces the loudest reliable playback observed.
	DefaultBankVolume  = 0
	BankVoiceNumOffset = 0x202
	// VPEntrySize is the size of a single vp[] entry in a bank
	// sector (a 16-bit little-endian voice-slot index).
	VPEntrySize    = 2
	EffectDataSize = 24

	// Effect block field offsets (relative to BankEffectOffset). See spec
	// section 2-3 (struct effectdata). Every field is one byte. The
	// aft_lfp offset is 17, NOT 18; the original constant was off by one
	// and pointed at aft_lfa instead. Documented but unused fields (mvol,
	// suss; spec: "normally 0") are kept here so parse round-trips can
	// inspect them.
	//
	// EffectBendOffset (byte 0) is the pitch-bender depth in 1/8
	// semitone units, range 0..127. The full runtime chain in the
	// disassembled firmware (verified byte-for-byte at F000:1EE4
	// onward):
	//
	//   BH = [0x5288]                              ; the bend depth
	//   IMUL by the per-channel bend value at
	//     0x52A0 + ch*0x18 + 0
	//   IDIV by 127
	//   SAR by 3                                   ; 1/8 semitone unit
	//   ADD result to the pitch accumulator
	//
	// The matching menu editor at F000:6AEE uses SHR/SHL by 3 on
	// the byte to round-trip the 1/8-semitone UI. Boot init at
	// F000:0860 writes 0x18, giving the default depth of 3 semitones.
	EffectBendOffset   = 0x00 // bend: pitch-bender depth (1/8 semitone units, 0-127)
	EffectMVolOffset   = 0x01 // mvol: master volume (unused, spec says normally 0)
	EffectSusSOffset   = 0x02 // suss: sustain switch (unused, spec says normally 0)
	EffectModLFPOffset = 0x03 // mod wheel -> LFO pitch depth
	EffectModLFAOffset = 0x04 // mod wheel -> LFO amp depth
	EffectModLFFOffset = 0x05 // mod wheel -> LFO filter depth
	EffectModLFQOffset = 0x06 // mod wheel -> LFO resonance depth
	EffectModDCFOffset = 0x07 // mod wheel -> filter offset
	EffectModDCAOffset = 0x08 // mod wheel -> amp offset
	EffectModDCQOffset = 0x09 // mod wheel -> resonance offset
	EffectFotLFPOffset = 0x0A // foot pedal -> LFO pitch depth
	EffectFotLFAOffset = 0x0B // foot pedal -> LFO amp depth
	EffectFotLFFOffset = 0x0C // foot pedal -> LFO filter depth
	EffectFotLFQOffset = 0x0D // foot pedal -> LFO resonance depth
	EffectFotDCAOffset = 0x0E // foot pedal -> amp offset (volume)
	EffectFotDCFOffset = 0x0F // foot pedal -> filter offset
	EffectFotDCQOffset = 0x10 // foot pedal -> resonance offset
	EffectAftLFPOffset = 0x11 // aftertouch -> LFO pitch depth
	EffectAftLFAOffset = 0x12 // aftertouch -> LFO amp depth
	EffectAftLFFOffset = 0x13 // aftertouch -> LFO filter depth
	EffectAftLFQOffset = 0x14 // aftertouch -> LFO resonance depth
	EffectAftDCAOffset = 0x15 // aftertouch -> amp offset
	EffectAftDCFOffset = 0x16 // aftertouch -> filter offset
	EffectAftDCQOffset = 0x17 // aftertouch -> resonance offset

	MaxBendRange = 127
)

// Key range and MIDI defaults.
const (
	FirstMIDINote      = 36
	MaxMIDIChannel     = 16
	MaxMIDINote        = 127
	DefaultVoiceName   = "VOICE"
	DefaultKeyHigh     = 96
	DefaultKeyLow      = 36
	DefaultKeyCentre   = 72
	MaxGenerators      = 8
	PolyphonicAudioOut = 0xff
	DefaultVelLow      = 1
	DefaultVelHigh     = 0x7f
)

// Playback mode constants from the FZ-1 Data Structures document.
// Stored at voicedata+0x10 (disk-resident, static). fizzle handles
// only voicedata; the runtime voicestate block at firmware RAM
// [0x5420 + voice_idx * 0x3A] also uses offset +0x10 but for a
// signed loop-status word with different sentinels (0xFFFE and
// 0xFFFF; see PlaybackModeNormalVariant for citations).
const (
	PlaybackModeNoSound     = 0x0000
	PlaybackModeNormal      = 0x01D7
	PlaybackModeReverse     = 0x101D
	PlaybackModeCue         = 0x2014
	PlaybackModeSynthesized = 0x0013

	// PlaybackModeNormalVariant is an undocumented variant of NORMAL
	// that appears only in the FZ-1 Factory Library's Clarinet.fzf,
	// exclusively in the first voice of each bank (every slot whose
	// waveStart=0). It differs from NORMAL (0x01D7) by one cleared
	// bit: bit 7 of the low byte. No other file in 85+ factory and
	// shareware FZFs carries this value.
	//
	// Spec section 2-1 treats loop as an opaque 5-value enum mapping
	// to GAA gate-array modes; the bit-level layout of the field is
	// not documented. Parsing this value lets fizzle load
	// Clarinet.fzf; encountering it emits a WARN log.
	//
	// Functionally equivalent to NORMAL on every firmware read of
	// voicedata+0x10 decoded from the disassembled firmware source.
	// The reads are enumerative (full 16-bit compare or top-nibble
	// mask), not bit-level:
	//
	//   F000:1E88  voice-service front gate:
	//                AND AX, 0xF000; CMP AX, 0x2000 (CUE family
	//                detector)
	//   F000:1EF8  separate dispatch entry called from F000:1E88:
	//                CMP word [SI+0x10], 0x13 (SYNTHESIZED pitch
	//                bias)
	//   F000:4AA1  three-way label dispatcher (F000:4A70 body):
	//                CMP word ES:[BX+0x10], 0x13 after LES BX,
	//                [0x0E04] at F000:4A9D; branches to the
	//                alternate label at F000:4ABC on mismatch
	//   F000:6175  voice param menu init at F000:615C:
	//                CMP word ES:[SI+0x10], 0x13
	//   F000:6286  voice param menu init at F000:6278:
	//                CMP word ES:[BX+0x10], 0x13
	//
	// 0x0157 and 0x01D7 share top nibble 0 (so the 0x2000 / 0x1000
	// family detectors miss) and neither equals 0x0013 (so the
	// SYNTHESIZED bias does not apply). Append CS:IP and branch
	// behaviour here if a bit-level read of voicedata+0x10 surfaces
	// elsewhere in the ROM.
	//
	// Note on voicestate vs voicedata: voicedata is the disk-resident
	// static struct this enum covers. The runtime voicestate block
	// (RAM, firmware base 0x5420, stride 0x3A per voice) stores a
	// signed loop-status word at +0x10 with sentinels 0xFFFE
	// (boundary just crossed) and 0xFFFF (waiting for chip to reach
	// loopst[idx]); these are voicestate values, not voicedata, and
	// do not belong in this enum. The voice-service handler at
	// F000:1E88 pins both reads: voicedata at [SI+0x10] (front gate)
	// and voicestate at [DI+0x10] (loop tracking: CMP word
	// [DI+0x10], -2 at F000:1F4E).
	PlaybackModeNormalVariant = 0x0157
)

// Canonical lowercase identifiers for the documented playback modes. Used
// as JSON values so consumers can filter on stable string keys. NoSound is
// the spec-defined placeholder for an undefined slot (see the loop-mode
// table in docs/casio-fz1-data-structures.md).
const (
	PlaybackModeNameNoSound       = "no_sound"
	PlaybackModeNameNormal        = "normal"
	PlaybackModeNameNormalVariant = "normal_variant"
	PlaybackModeNameReverse       = "reverse"
	PlaybackModeNameCue           = "cue"
	PlaybackModeNameSynthesized   = "synthesized"
	PlaybackModeNameUnknown       = "unknown"
)

// PlaybackModeName returns the canonical identifier for a playback mode
// value. Unrecognised values map to "unknown".
func PlaybackModeName(mode uint16) string {
	switch mode {
	case PlaybackModeNoSound:
		return PlaybackModeNameNoSound
	case PlaybackModeNormal:
		return PlaybackModeNameNormal
	case PlaybackModeNormalVariant:
		return PlaybackModeNameNormalVariant
	case PlaybackModeReverse:
		return PlaybackModeNameReverse
	case PlaybackModeCue:
		return PlaybackModeNameCue
	case PlaybackModeSynthesized:
		return PlaybackModeNameSynthesized
	default:
		return PlaybackModeNameUnknown
	}
}

// LoopStartAddress returns the sample address bits of a loopst[] value,
// masking out the loop-fine byte the spec reserves in the upper 8 bits.
// Real-world FZ-1 voices usually leave loop-fine = 0, but third-party files
// may carry non-zero values that must not be interpreted as part of the
// address.
func LoopStartAddress(loopst uint32) uint32 {
	return loopst & LoopStartAddressMask
}

// LoopFineBits returns the loop-fine byte stored in the upper 8 bits of a
// loopst[] value (spec §2-1: "Upper 8 bits for loopst are used for loop
// fine and take a number among 0 - 255").
func LoopFineBits(loopst uint32) uint8 {
	return uint8(loopst >> LoopStartFineShift)
}

// LoopEndAddress returns the sample address bits of a looped[] value,
// masking out the MSB the spec reserves for the loop-pattern (skip) flag.
func LoopEndAddress(looped uint32) uint32 {
	return looped & LoopEndAddressMask
}

// LoopSkipFlag reports whether the looped[] value has the skip-flag bit
// set (spec §2-1: "The MSB for looped is used for loop patterns; 1 for
// Skip, 0 for Trace").
func LoopSkipFlag(looped uint32) bool {
	return looped&LoopEndSkipMask != 0
}

// Envelope and loop defaults used when importing or building voices.
const (
	EnvelopeStages = 8
	NoSustainLoop  = 8
	// NoReleaseLoop is the loop_end value that prevents release looping.
	// The spec says values 0-7 assign the end of loops 1-8; value 8 means
	// "execute all 8 loops" which, combined with loop_sus=8 and all loop
	// pairs pointing to gened, causes playback to reach the end and stop.
	// Using loop_end=0 would activate loop 0 during release, causing a
	// zero-length loop click if loopst[0]==looped[0].
	NoReleaseLoop         = 8
	HoldIndefinitely      = 7
	EnvelopeMaxRate       = 127
	EnvelopeFullLevel     = 255
	DCFMaxOffset          = 127
	EnvelopeIdleRate      = 0xC0 // falling at magnitude 64; used for post-sustain stages
	VelSensitivityDefault = 80
	LoopTimeDefault       = 100
	GenEndGuard           = 8
	DisplayMax            = 99
	MaxEnvelopeStep       = 7
	MaxResonance          = 127
	MaxKFDisplay          = 15
	MinKFDisplay          = -15
	MaxLFODelay           = 65535
	// LFOAtckDefault is the in-spec default for the lfo_atck byte at offset
	// 0x9f. The spec (§2-1) defines the valid range as 1-127 with smaller =
	// slower rise and larger = faster. 127 means the LFO reaches full level
	// immediately, which is transparent when LFO depths are zero (the
	// default) and matches factory dumps that use a non-zero attack
	// (CASIO074/099/118/142 in the shareware corpus).
	LFOAtckDefault = 127
)

// RateByteToDisplay converts a sign-magnitude rate byte to the hardware's
// display value (0 to 99). The sign bit (envelope direction) is ignored;
// the FZ-10M front panel shows magnitude only.
//
// The formula (mag * 100) >> 7 was validated against FZ-10M hardware using
// test disk images with magnitudes 0, 1, 32, 63, 64, 96, 126, 127.
func RateByteToDisplay(b uint8) int {
	mag := int(b & RateMagMask)
	return (mag * 100) >> 7
}

// RateDisplayToByte converts a display magnitude (0 to 99) to a rate byte.
// The sign bit is not set; callers that need a specific direction should
// combine the result with RateSignBit. It returns the smallest magnitude
// byte that maps back to the given display value via RateByteToDisplay.
func RateDisplayToByte(display int) uint8 {
	if display <= 0 {
		return 0
	}
	return uint8((display*128 + 99) / 100) //nolint:gosec // value is bounded by validation
}

// StopByteToDisplay converts a stop level byte (0 to 255) to the hardware's
// display value (0 to 99).
//
// The formula ceil(byte * 99 / 255) was validated against FZ-10M hardware
// using test disk images with byte values 0, 25, 50, 75, 100, 150, 200, 255
// and cross-checked against the Brass factory preset (66, 56, 218, 255).
func StopByteToDisplay(b uint8) int {
	n := int(b)
	if n == 0 {
		return 0
	}
	return (n*DisplayMax + EnvelopeFullLevel - 1) / EnvelopeFullLevel
}

// StopDisplayToByte converts a display value (0 to 99) to the stop level
// byte (0 to 255) stored in the voice header. It returns the smallest byte
// value that maps back to the given display value via StopByteToDisplay.
func StopDisplayToByte(display int) uint8 {
	if display <= 0 {
		return 0
	}
	if display >= DisplayMax {
		return EnvelopeFullLevel
	}
	return uint8(EnvelopeFullLevel*(display-1)/DisplayMax + 1) //nolint:gosec // value is bounded by validation
}

// KFByteToDisplay converts a key follow byte to the hardware's display value
// (-15 to +15). The byte is interpreted as two's complement (int8) and
// divided by 8, clamped to MinKFDisplay.
//
// Validated against FZ-10M hardware using calibration disk images with byte
// values 0, 1, 4, 8, 15, 64, 127, 128.
func KFByteToDisplay(b uint8) int {
	v := int(int8(b)) / 8 //nolint:gosec // intentional two's complement reinterpretation
	if v < MinKFDisplay {
		return MinKFDisplay
	}
	return v
}

// KFDisplayToByte converts a hardware display value (-15 to +15) to the key
// follow byte stored in the voice header. The display value is multiplied by
// 8 and stored as a two's complement byte.
func KFDisplayToByte(display int) uint8 {
	return uint8(int8(display * 8)) //nolint:gosec // intentional two's complement conversion; display is bounded by validation
}

// FormatAudioOut renders a gchn bitmask byte as the human-readable string
// shown by the FZ-1 front panel and the `fzf info` Out column.
//
//	0xff       -> "all"   (all 8 generators)
//	0x00       -> "none"  (no generator assigned; usually a corrupt file)
//	single bit -> "N"     (e.g. 0x04 -> "3")
//	multi bit  -> "N,M,…" sorted ascending (e.g. 0x05 -> "1,3")
func FormatAudioOut(gchn uint8) string {
	if gchn == PolyphonicAudioOut {
		return "all"
	}
	if gchn == 0 {
		return "none"
	}
	outputs := make([]string, 0, MaxGenerators)
	for i := range MaxGenerators {
		if gchn&(1<<i) != 0 {
			outputs = append(outputs, strconv.Itoa(i+1))
		}
	}
	return strings.Join(outputs, ",")
}

// LFO waveform constants from the FZ-1 Data Structures document.
const (
	LFOSine      = 0
	LFOSawUp     = 1
	LFOSawDown   = 2
	LFOTriangle  = 3
	LFORectangle = 4
	LFORandom    = 5
)

// I/O limits.
const (
	BytesPerSample     = 2
	SemitoneDCPScale   = 256
	SemitonesPerOctave = 12
)

// IsPlausibleVoiceHeader returns true when data looks like a valid FZV voice
// header. It checks that the file is at least one sector, that the 12-byte
// name at VoiceNameOffset is printable ASCII, and that the sample rate index
// byte at VoiceSampOffset is in range (0, 1, or 2). The rate index check
// prevents arbitrary text files from being misidentified as voices.
//
// This is the strict heuristic used by file-type disambiguation: it
// distinguishes FZF bank sectors (no printable name at offset 0xb2) from
// mis-named FZV voice files (printable name + valid rate). For loading FZV
// files that are KNOWN by the caller to be voices but may have blank/garbage
// name bytes (e.g. output of `fzf unpack` on real-world dumps), callers
// should use IsPlausibleVoiceSlot on the first VoiceHeaderUsed bytes instead.
func IsPlausibleVoiceHeader(data []byte) bool {
	if len(data) < SectorSize {
		return false
	}
	if !IsPrintableName(data[VoiceNameOffset : VoiceNameOffset+LabelSize]) {
		return false
	}
	return int(data[VoiceSampOffset]) < len(SampleRates)
}

// IsPlausibleVoiceSlot reports whether a 192-byte voice slot inside an FZF
// voice area carries a real voice header. It checks the playback mode is one
// of the known active modes (NoSound is *not* accepted here; callers handle
// that explicitly), the sample rate index is in range, wave pointers are
// non-decreasing, and the envelope sustain stages are in [0, EnvelopeStages).
//
// Unlike IsPlausibleVoiceHeader (which validates a whole 1024-byte FZV
// header sector), this only sees the 192 packed bytes the FZF voice area
// stores per slot, and intentionally does NOT require the name field to be
// printable: factory dumps frequently zero out names while still carrying
// real voice data, and downstream code substitutes "VOICE N" for empty
// names. Distinguishing "valid voice with blank name" from "audio bytes
// that happen to fall here" relies on the rate-index and wave-pointer
// checks instead.
func IsPlausibleVoiceSlot(slot []byte) bool {
	if len(slot) < VoiceHeaderUsed {
		return false
	}
	mode := binary.LittleEndian.Uint16(slot[VoiceLoopModeOffset:])
	switch mode {
	case PlaybackModeNormal, PlaybackModeReverse, PlaybackModeCue, PlaybackModeSynthesized, PlaybackModeNormalVariant:
		// active modes are plausible (PlaybackModeNormalVariant is an
		// undocumented but structurally valid Clarinet.fzf-specific value)
	default:
		return false
	}
	if int(slot[VoiceSampOffset]) >= len(SampleRates) {
		return false
	}
	wavst := binary.LittleEndian.Uint32(slot[VoiceWaveStartOffset:])
	waved := binary.LittleEndian.Uint32(slot[VoiceWaveEndOffset:])
	if wavst > waved {
		return false
	}
	if int(slot[VoiceDCASusOffset]) >= EnvelopeStages || int(slot[VoiceDCAEndOffset]) >= EnvelopeStages {
		return false
	}
	if int(slot[VoiceDCFSusOffset]) >= EnvelopeStages || int(slot[VoiceDCFEndOffset]) >= EnvelopeStages {
		return false
	}
	return true
}

// IsActiveOrEmptyVoiceSlot reports whether a voice slot is either a
// plausible active voice or an explicit PlaybackModeNoSound placeholder.
// The voice-area extent walk uses this to span legitimate empty slots
// (the spec allows them; see CASIO139.FZF in the FZ-1 shareware library,
// which has four NoSound slots before the first real voice) without
// terminating early. It returns false for garbage bytes, which signals
// the end of the real voice area.
func IsActiveOrEmptyVoiceSlot(slot []byte) bool {
	if len(slot) < VoiceHeaderUsed {
		return false
	}
	mode := binary.LittleEndian.Uint16(slot[VoiceLoopModeOffset:])
	if mode == PlaybackModeNoSound {
		return true
	}
	return IsPlausibleVoiceSlot(slot)
}

// SamplePointerKind classifies the four-byte fields that
// ForEachSamplePointer iterates over. Wave pointers are plain 32-bit sample
// addresses with no reserved bits. Loop-start and loop-end pointers reserve
// flag bits in addition to the address (spec §2-1: the upper 8 bits of
// loopst encode loop-fine, the MSB of looped encodes the skip flag). Wave
// vs loop distinction matters when rewriting addresses for round-trips:
// adjusting the raw 32-bit value would corrupt the reserved bits.
type SamplePointerKind int

const (
	// WavePointer designates the four wave-area pointer fields
	// (wavst, waved, genst, gened) at offsets 0x00-0x0f.
	WavePointer SamplePointerKind = iota
	// LoopStartPointer designates a loopst[i] field (0x14-0x33). The
	// upper 8 bits carry the loop-fine value; the lower 24 bits carry
	// the sample address.
	LoopStartPointer
	// LoopEndPointer designates a looped[i] field (0x34-0x53). The MSB
	// carries the loop-pattern (skip) flag; the lower 31 bits carry the
	// sample address.
	LoopEndPointer
)

// ForEachSamplePointer calls fn for each 4-byte sample pointer field in a
// voice header: wave pointers (0x00 to 0x0f) and loop pointers (0x14 to 0x53).
// fn receives a mutable slice of the 4-byte field plus a kind tag that tells
// the callback which reserved bits, if any, the field protects. Wave-pointer
// fields are plain addresses; LoopStartPointer fields carry the loop-fine
// byte in their upper 8 bits; LoopEndPointer fields carry the skip flag in
// their MSB. Callers that read/modify only the address bits must mask via
// LoopStartAddress/LoopEndAddress and reassemble before writing back.
func ForEachSamplePointer(voice []byte, fn func(field []byte, kind SamplePointerKind)) {
	if len(voice) < LoopPointerRangeEnd {
		return
	}
	for i := WavePointerRangeStart; i < WavePointerRangeEnd; i += 4 {
		fn(voice[i:i+4], WavePointer)
	}
	// loopst[8] occupies the first half of the loop-pointer range
	// (LoopPointerRangeStart..VoiceLoopEd0Offset); looped[8] occupies the
	// second half (VoiceLoopEd0Offset..LoopPointerRangeEnd).
	for i := LoopPointerRangeStart; i < VoiceLoopEd0Offset; i += 4 {
		fn(voice[i:i+4], LoopStartPointer)
	}
	for i := VoiceLoopEd0Offset; i < LoopPointerRangeEnd; i += 4 {
		fn(voice[i:i+4], LoopEndPointer)
	}
}

// VoiceAreaSectors returns the number of sectors needed to store nvoice voice headers.
func VoiceAreaSectors(nvoice int) int {
	return (nvoice + VoicesPerSector - 1) / VoicesPerSector
}

// VoiceSlotOffset returns the byte offset of voice voiceIndex within a bank starting at bankStart.
func VoiceSlotOffset(bankStart int, voiceIndex int) int {
	return bankStart + (voiceIndex/VoicesPerSector)*SectorSize + (voiceIndex%VoicesPerSector)*VoicePackSize
}

// BankVPLookup returns the voice-slot index that bank bankIdx's vp[]
// maps to area areaIdx, and whether the lookup is in bounds. The vp[]
// table is the canonical mapping from a bank's per-Area key-split
// slots to the voice-area slot indices (spec §2-2). Linear cumulative
// indexing (sum of prior banks' bstep) is NOT equivalent: vp[] entries
// may repeat across banks, banks may reorder voices, and a multi-bank
// dump may have voices that no bank's vp[] references.
//
// data is the full FZF byte slice. Returns (0, false) when bankIdx or
// areaIdx points outside the readable bytes.
func BankVPLookup(data []byte, bankIdx, areaIdx int) (int, bool) {
	off := bankIdx*SectorSize + BankVoiceNumOffset + VPEntrySize*areaIdx
	if off+VPEntrySize > len(data) {
		return 0, false
	}
	return int(binary.LittleEndian.Uint16(data[off : off+VPEntrySize])), true
}
