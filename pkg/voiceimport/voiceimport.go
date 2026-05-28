// Package voiceimport implements the fizzle voice import command. It converts
// a mono PCM WAV file (16, 24, or 32-bit) into an FZ series voice file (.fzv).
package voiceimport

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"

	"github.com/rs/zerolog/log"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fileutil"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/internal/bitconv"
	"github.com/philipcunningham/fizzle/pkg/render"
	"github.com/philipcunningham/fizzle/pkg/wav"
)

// MaxTranspose / MinTranspose bound the transpose argument to Encode in
// semitones. The FZ-1 dcp field is a signed 16-bit value at 1/256-semitone
// resolution; ±127 is the widest range that survives that encoding, and
// matches the SFZ v1 spec.
const (
	MaxTranspose = 127
	MinTranspose = -127
)

// Import converts the WAV at wavPath to an FZV voice file at fzvPath.
// targetRate must be 36000, 18000, or 9000. The output is written atomically.
func Import(wavPath, fzvPath string, targetRate uint32) error {
	if err := disk.ValidateRate(targetRate); err != nil {
		return fmt.Errorf("voiceimport: %w", err)
	}
	idx, _ := disk.RateIndexFor(targetRate)

	f, err := fzutil.ReadWAV(wavPath)
	if err != nil {
		return fmt.Errorf("voiceimport: %w", err)
	}

	samples, err := fzutil.Resample(f, targetRate)
	if err != nil {
		return fmt.Errorf("voiceimport: %w", err)
	}

	name := fzutil.VoiceName(wavPath)
	log.Info().
		Str("wav", filepath.Base(wavPath)).
		Str("name", name).
		Uint32("rate", targetRate).
		Int("samples", len(samples)).
		Msg("importing voice")
	data := Encode(samples, idx, name, 0, scaledLoop(f, targetRate, len(samples)))
	// Honour the SMPL chunk's MIDIUnityNote so a WAV produced by `fzv extract`
	// (which embeds the source voice's root key) round-trips its cent byte
	// when re-imported. MIDIUnityNote=0 is the WAV "unset" sentinel; leave the
	// Encode default in place in that case.
	if f.MIDIUnityNote != 0 && len(data) > disk.VoiceKeyCentOffset {
		data[disk.VoiceKeyCentOffset] = f.MIDIUnityNote
	}
	log.Debug().
		Str("fzv", fzvPath).
		Str("size", fmt.Sprintf("%d bytes", len(data))).
		Msg("writing voice file")
	if fzutil.OverCapacity(len(data)) {
		log.Warn().
			Str("size", render.FormatBytes(len(data))).
			Str("limit", render.FormatBytes(disk.UsableDataSize)).
			Msg("voice data exceeds floppy disk capacity")
	}
	if err := fileutil.WriteAtomic(fzvPath, data); err != nil {
		return fmt.Errorf("voiceimport: %w", err)
	}
	return nil
}

// voiceHeader mirrors struct voicedata from the FZ-1 Data Structures document.
// Fields are in declaration order so binary.Write produces the correct layout.
// All fields are little-endian as per the V50 (8086-compatible) CPU.
//
// In the FZ-1 spec: long=4 bytes, int=2 bytes, short=1 byte.
// Total size = 0xC0 = 192 bytes.
type voiceHeader struct {
	WaveStart uint32 // 0x00 wavst
	WaveEnd   uint32 // 0x04 waved
	GenStart  uint32 // 0x08 genst
	GenEnd    uint32 // 0x0c gened

	PlaybackMode uint16 // 0x10 loop: playback mode constant (0x01D7 = NORMAL, 0x101D = REVERSE, etc.)
	LoopSus      uint8  // 0x12 loop_sus (short=1)
	LoopEnd      uint8  // 0x13 loop_end (short=1)

	LoopSt [disk.MaxGenerators]uint32 // 0x14 loopst[8] (long[8]=32)
	LoopEd [disk.MaxGenerators]uint32 // 0x34 looped[8] (long[8]=32)
	LoopXf [disk.MaxGenerators]uint16 // 0x54 loopxf[8] (int[8]=16)
	LoopTm [disk.MaxGenerators]uint16 // 0x64 looptm[8] (uint[8]=16)

	DCP uint16 // 0x74 dcp (int=2)
	DCF uint8  // 0x76 dcf (short=1)
	DCQ uint8  // 0x77 dcq (short=1)

	DCASus  uint8                      // 0x78 dca_sus (short=1)
	DCAEnd  uint8                      // 0x79 dca_end (short=1)
	DCARate [disk.EnvelopeStages]uint8 // 0x7a dca_rate[8] (short[8]=8)
	DCAStop [disk.EnvelopeStages]uint8 // 0x82 dca_stop[8] (ushort[8]=8)

	DCFSus  uint8                      // 0x8a dcf_sus (short=1)
	DCFEnd  uint8                      // 0x8b dcf_end (short=1)
	DCFRate [disk.EnvelopeStages]uint8 // 0x8c dcf_rate[8] (short[8]=8)
	DCFStop [disk.EnvelopeStages]uint8 // 0x94 dcf_stop[8] (ushort[8]=8)

	LFODelay uint16 // 0x9c lfo_delay (unsigned int=2)
	LFOName  uint8  // 0x9e lfo_name (unsigned short=1)
	LFOAtck  uint8  // 0x9f lfo_atck (unsigned short=1)
	LFORate  uint8  // 0xa0 lfo_rate (short=1)
	LFODCP   uint8  // 0xa1 lfo_dcp (short=1)
	LFODCA   uint8  // 0xa2 lfo_dca (short=1)
	LFODCF   uint8  // 0xa3 lfo_dcf (short=1)
	LFODCQ   uint8  // 0xa4 lfo_dcq (short=1)

	VelDCQKF uint8 // 0xa5 vel_dcq_kf (short=1)
	DCAKF    uint8 // 0xa6 dca_kf (short=1)
	DCARS    uint8 // 0xa7 dca_rs (short=1)
	DCFKF    uint8 // 0xa8 dcf_kf (short=1)
	DCFRS    uint8 // 0xa9 dcf_rs (short=1)
	VelDCAKF uint8 // 0xaa vel_dca_kf (short=1)
	VelDCARS uint8 // 0xab vel_dca_rs (short=1)
	VelDCFKF uint8 // 0xac vel_dcf_kf (short=1)
	VelDCFRS uint8 // 0xad vel_dcf_rs (short=1)

	HWID uint8                         // 0xae hwid (unsigned short=1)
	LWID uint8                         // 0xaf lwid (unsigned short=1)
	Cent uint8                         // 0xb0 cent (unsigned short=1)
	Samp uint8                         // 0xb1 samp (unsigned short=1)
	Name [disk.VoiceNameFieldSize]byte // 0xb2 name (last 2 bytes must be zero)
}

// scaledLoop derives FZV loop points from a parsed WAV's SMPL chunk,
// scaling sample indices to the target rate when the WAV is resampled.
// Returns NoLoop() when the WAV carries no usable loop points.
func scaledLoop(f *wav.File, targetRate uint32, nSamples int) LoopParams {
	if f.LoopStart < 0 || f.LoopEnd <= f.LoopStart || f.SampleRate == 0 {
		return NoLoop()
	}
	ratio := float64(targetRate) / float64(f.SampleRate)
	ls := int(math.Round(float64(f.LoopStart) * ratio))
	le := int(math.Round(float64(f.LoopEnd) * ratio))
	if le > nSamples {
		le = nSamples
	}
	if ls >= le {
		return NoLoop()
	}
	return LoopParams{LoopStart: ls, LoopEnd: le}
}

// LoopParams carries loop point information for a voice.
// LoopStart and LoopEnd are sample indices in the target sample rate.
// If LoopStart < 0, no loop is applied and the voice plays as a one-shot.
type LoopParams struct {
	LoopStart int
	LoopEnd   int
}

// NoLoop returns a LoopParams meaning no loop.
func NoLoop() LoopParams { return LoopParams{LoopStart: -1, LoopEnd: -1} }

// Encode builds a complete FZV byte slice from PCM samples, a rate index
// (0=36kHz, 1=18kHz, 2=9kHz), a voice name, a transpose value in semitones,
// and optional loop points. Use NoLoop() for one-shot samples.
//
// Default values produce a one-shot voice with a full-open amplitude
// envelope and MIDI key range C2-C7 centred on C5. When loop.LoopStart >= 0,
// the voice is configured for loop_sustain: it plays to the loop start,
// loops between LoopStart and LoopEnd while the key is held, then releases
// after note-off.
func Encode(samples []int16, rateIdx uint8, name string, transpose int, loop LoopParams) []byte {
	// Defensive clamp: dcp is a signed 16-bit field at 1/256-semitone resolution.
	// ±127 semitones is the widest range that fits, matching the SFZ spec.
	// Callers (e.g. pkg/sfz) should already clamp and warn; this guards against
	// future callers passing through unchecked values that would silently wrap.
	if transpose < MinTranspose {
		transpose = MinTranspose
	}
	if transpose > MaxTranspose {
		transpose = MaxTranspose
	}

	n := bitconv.LenU32(samples)
	genEnd := n
	if n > disk.GenEndGuard {
		genEnd = n - disk.GenEndGuard
	}

	hasLoop := loop.LoopStart >= 0 && loop.LoopEnd > loop.LoopStart

	// loop_sus and loop_end:
	// one-shot: loop_sus=8 (no sustain loop), loop_end=8 (no release loop)
	// looped:   loop_sus=0 (sustain on loop 1), loop_end=7 (hold indefinitely)
	loopSus := uint8(disk.NoSustainLoop)
	loopEndPt := uint8(disk.NoReleaseLoop)
	if hasLoop {
		loopSus = 0
		loopEndPt = disk.HoldIndefinitely
	}

	hdr := voiceHeader{
		WaveStart:    0,
		WaveEnd:      n,
		GenStart:     0,
		GenEnd:       genEnd,
		PlaybackMode: disk.PlaybackModeNormal, // NORMAL playback mode (see FZ-1 Data Structures doc, struct voicedata.loop)

		LoopSus:  loopSus,
		LoopEnd:  loopEndPt,
		DCP:      bitconv.NarrowU16(transpose * disk.SemitoneDCPScale),
		LoopTm:   [disk.MaxGenerators]uint16{disk.LoopTimeDefault, disk.LoopTimeDefault, disk.LoopTimeDefault, disk.LoopTimeDefault, disk.LoopTimeDefault, disk.LoopTimeDefault, disk.LoopTimeDefault, disk.LoopTimeDefault},
		DCASus:   0,
		DCAEnd:   disk.HoldIndefinitely,
		DCARate:  [disk.EnvelopeStages]uint8{disk.EnvelopeMaxRate, disk.EnvelopeIdleRate, disk.EnvelopeIdleRate, disk.EnvelopeIdleRate, disk.EnvelopeIdleRate, disk.EnvelopeIdleRate, disk.EnvelopeIdleRate, disk.EnvelopeIdleRate},
		DCAStop:  [disk.EnvelopeStages]uint8{disk.EnvelopeFullLevel, 0, 0, 0, 0, 0, 0, 0},
		DCF:      disk.DCFMaxOffset,
		DCFSus:   0,
		DCFEnd:   disk.HoldIndefinitely,
		DCFRate:  [disk.EnvelopeStages]uint8{disk.EnvelopeMaxRate, 0, 0, 0, 0, 0, 0, 0},
		DCFStop:  [disk.EnvelopeStages]uint8{disk.EnvelopeFullLevel, disk.EnvelopeFullLevel, disk.EnvelopeFullLevel, disk.EnvelopeFullLevel, disk.EnvelopeFullLevel, disk.EnvelopeFullLevel, disk.EnvelopeFullLevel, disk.EnvelopeFullLevel},
		LFOAtck:  disk.LFOAtckDefault,
		VelDCAKF: disk.VelSensitivityDefault,
		HWID:     disk.DefaultKeyHigh,
		LWID:     disk.DefaultKeyLow,
		Cent:     disk.DefaultKeyCentre,
		Samp:     rateIdx,
	}

	if hasLoop {
		// Loop 1 set to provided points; remaining loops at genEnd.
		hdr.LoopSt[0] = bitconv.NarrowU32(loop.LoopStart)
		hdr.LoopEd[0] = bitconv.NarrowU32(loop.LoopEnd)
		for i := 1; i < disk.MaxGenerators; i++ {
			hdr.LoopSt[i] = genEnd
			hdr.LoopEd[i] = genEnd
		}
	} else {
		for i := range hdr.LoopSt {
			hdr.LoopSt[i] = genEnd
			hdr.LoopEd[i] = genEnd
		}
	}
	paddedName := disk.PadLabel(name)
	copy(hdr.Name[:disk.LabelSize], paddedName[:])

	var hdrBuf bytes.Buffer
	if err := binary.Write(&hdrBuf, binary.LittleEndian, hdr); err != nil {
		// voiceHeader contains only uint8/uint16/uint32 and fixed-size arrays
		// of these, so binary.Write cannot fail at runtime. Panicking here
		// turns a future struct-change regression into an immediate, loud
		// failure rather than silent voice-file corruption.
		panic(fmt.Errorf("voiceimport: binary.Write on voiceHeader: %w", err))
	}

	// Pad header to exactly SectorSize bytes.
	hdrBytes := hdrBuf.Bytes()
	if len(hdrBytes) < disk.SectorSize {
		hdrBytes = append(hdrBytes, make([]byte, disk.SectorSize-len(hdrBytes))...)
	}

	// Pad audio to a sector boundary.
	audioSize := len(samples) * disk.BytesPerSample
	audio := make([]byte, disk.PadToSector(audioSize))
	for i, s := range samples {
		bitconv.WriteInt16LE(audio[i*disk.BytesPerSample:], s)
	}

	return append(hdrBytes, audio...)
}
