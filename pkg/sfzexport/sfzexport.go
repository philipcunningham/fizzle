// Package sfzexport converts an FZF full dump into an SFZ instrument file
// plus individual WAV files. This is the reverse of pkg/sfzconvert and
// enables round-tripping hardware dumps into DAWs.
//
// Mapping decisions and known limitations:
//
//   - Voices are processed in voice-slot order so that bank-sector metadata
//     (key range, velocity, mutegroup) aligns with the voice-header data
//     (cutoff, resonance, envelopes, loop points) for each voice.
//   - Per-voice filenames are derived from the voice name embedded in the
//     header, with any filesystem-unsafe runes mapped to underscore.
//   - Playback modes other than NORMAL collapse on the SFZ side because SFZ
//     has no equivalent for the FZ's CUE, SYNTH, or REVERSE modes. The
//     original mode is preserved verbatim in a "// Playback:" comment so the
//     information survives a round-trip through a text-aware tool, but it is
//     not recovered by sfz convert.
//   - A velocity range of (0, 0) means "voice silenced" on the hardware. It
//     is preserved with explicit lovel/hivel opcodes so re-import does not
//     silently restore the default (1, 127) range.
//   - DCA/DCF envelopes, LFO, and modulation routing have no SFZ
//     representation. They are preserved as "//" comments above each region.
package sfzexport

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"math/bits"
	"os"
	"path/filepath"
	"strings"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fileutil"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/voiceextract"
	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
	"github.com/philipcunningham/fizzle/pkg/wav"
	"github.com/rs/zerolog/log"
)

var lfoWaveformNames = [...]string{"sine", "saw-up", "saw-down", "triangle", "rectangle", "random"}

func lfoWaveformName(idx int) string {
	if idx >= 0 && idx < len(lfoWaveformNames) {
		return lfoWaveformNames[idx]
	}
	return fmt.Sprintf("unknown(%d)", idx)
}

// playbackModeName delegates to disk.PlaybackModeName so the SFZ export's
// "// Playback:" comment uses the same canonical lowercase identifier as
// fzfinfo/fzvinfo ("normal", "normal_variant", "synthesized", ...).
// Unknown modes are surfaced as a hex literal so the value survives
// round-trips and the user can correlate with bytes on disk.
func playbackModeName(mode uint16) string {
	name := disk.PlaybackModeName(mode)
	if name == disk.PlaybackModeNameUnknown {
		return fmt.Sprintf("0x%04X", mode)
	}
	return name
}

// Export reads the FZF at fzfPath, extracts each voice as a WAV file, and
// writes an SFZ instrument file mapping them to their original key ranges.
// name controls the SFZ filename; if empty, it is derived from fzfPath.
// outputDir is created if it does not exist.
func Export(fzfPath, outputDir, name string) error {
	if name == "" {
		base := filepath.Base(fzfPath)
		name = strings.TrimSuffix(base, filepath.Ext(base))
	}

	data, hdr, err := fzutil.ReadFZF(fzfPath)
	if err != nil {
		return fmt.Errorf("sfzexport: %w", err)
	}

	storedNames := fzutil.ExtractStoredNames(data, hdr)

	voices, slotIndices, err := voiceunpack.UnpackData(fzfPath)
	if err != nil {
		return fmt.Errorf("sfzexport: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("sfzexport: %w", err)
	}

	// Mute-group and shared-voice warnings walk every (bank, split) site
	// across all bank sectors; bank 0 alone misses voices that live only
	// in banks 1-7 (real-hardware multi-bank dumps like TECHNO.img).
	gchnMap := buildMuteGroupMap(data, hdr, storedNames)
	warnSharedVoicePointers(data, hdr, storedNames)

	// Voice filenames must be unique on the output filesystem, even when
	// two voices share a header name. Tracked separately from voice
	// display names so the SFZ comments still show the original name.
	seenWavName := map[string]int{}

	var sfzBuf strings.Builder
	for emitIdx, fzvData := range voices {
		// slotIdx is the voice's position in the voice area (the value that
		// bank vp[] arrays point at). emitIdx is the index into the
		// compacted FZV slice, which skips NoSound placeholders, so the two
		// diverge whenever the dump has any leading or interior NoSound
		// slots. Bank metadata (key range, output, velocity, bvol) is
		// indexed by the bank's key-split position, looked up below.
		slotIdx := slotIndices[emitIdx]
		voiceName := ""
		if slotIdx < len(storedNames) {
			voiceName = storedNames[slotIdx]
		}
		if voiceName == "" {
			voiceName = fmt.Sprintf("VOICE_%d", slotIdx+1)
		}

		sampleRate, samples, err := voiceextract.Decode(fzvData)
		if err != nil {
			// One voice with a corrupt header (e.g. waveEnd > available
			// audio) should not abort the whole export. Surface the
			// corruption via a WARN, skip the voice, and keep going so
			// the user still gets the well-formed voices.
			log.Warn().
				Int("voice", slotIdx+1).
				Str("name", voiceName).
				Err(err).
				Msg("skipping voice with undecodable audio")
			continue
		}
		_ = emitIdx // emit position retained for future diagnostics; bank lookups use slotIdx via FindBankSitesForVoice.

		hdrBytes := fzvData[:disk.SectorSize]

		dcp := int16(binary.LittleEndian.Uint16(hdrBytes[disk.VoiceDCPOffset:])) //nolint:gosec // G115: intentional two's complement reinterpretation of DCP field
		dcf := hdrBytes[disk.VoiceDCFOffset]
		dcq := hdrBytes[disk.VoiceDCQOffset]
		loopSus := hdrBytes[disk.VoiceLoopSusOffset]
		loopEndByte := hdrBytes[disk.VoiceLoopEndOffset]
		// loop_sus (0..7) selects which of the eight loopst/looped pairs
		// is the active sustain loop. Reading [0] unconditionally would
		// export the wrong loop_start / loop_end for any voice whose
		// sustain pair was not the first. When loop_sus == 8 there is no
		// sustain loop; the indexed read is suppressed by the hasLoop
		// guard below.
		var loopSt0, loopEd0 uint32
		// loopxf[loop_sus] (0..1023; 0 disables cross-fade) and
		// looptm[loop_sus] (1..1022; 16ms step) per spec §2-1 have no
		// SFZ opcode equivalent. Captured here so the comment block
		// below can surface them as informational metadata, matching
		// how DCA/DCF/LFO are preserved across a round-trip.
		var loopXF, loopTm uint16
		if loopSus < disk.NoSustainLoop {
			stOff := disk.VoiceLoopSt0Offset + int(loopSus)*4
			edOff := disk.VoiceLoopEd0Offset + int(loopSus)*4
			// Mask off the loop-fine byte (upper 8 of loopst) and the
			// skip-flag bit (MSB of looped) per spec §2-1 so the SFZ
			// loop_start / loop_end opcodes carry only the sample
			// addresses.
			rawSt := binary.LittleEndian.Uint32(hdrBytes[stOff:])
			rawEd := binary.LittleEndian.Uint32(hdrBytes[edOff:])
			loopSt0 = disk.LoopStartAddress(rawSt)
			loopEd0 = disk.LoopEndAddress(rawEd)
			xfOff := disk.VoiceLoopXFOffset + int(loopSus)*disk.LoopXFEntrySize
			tmOff := disk.VoiceLoopTmOffset + int(loopSus)*disk.LoopTmEntrySize
			loopXF = binary.LittleEndian.Uint16(hdrBytes[xfOff:])
			loopTm = binary.LittleEndian.Uint16(hdrBytes[tmOff:])
		}
		playbackMode := binary.LittleEndian.Uint16(hdrBytes[disk.VoiceLoopModeOffset:])

		// LFO phase-sync flag (MSB of lfo_name) has no SFZ representation;
		// warn that it is being dropped so users debugging round-trip
		// issues can locate the source data.
		if hdrBytes[disk.VoiceLFONameOffset]&disk.LFOPhaseFlag != 0 {
			log.Warn().
				Int("voice", slotIdx+1).
				Str("name", voiceName).
				Str("field", "lfo_name.phase_sync").
				Msg("SFZ has no equivalent for LFO phase-sync flag; dropping")
		}

		// Release-loop pair (loop_end < NoReleaseLoop) selects a release
		// loop on the hardware. SFZ regions only carry a single loop, so
		// the release loop is lost on export.
		if loopEndByte < disk.NoReleaseLoop {
			log.Warn().
				Int("voice", slotIdx+1).
				Str("name", voiceName).
				Str("field", "loop_end").
				Uint8("value", loopEndByte).
				Msg("release loop is not preserved on SFZ export")
		}

		// Reverse playback has no SFZ equivalent; the WAV is exported as
		// forward audio. Compare the raw uint16 value so this warning stays
		// independent of any name-table refactor in progress.
		if playbackMode == disk.PlaybackModeReverse {
			log.Warn().
				Int("voice", slotIdx+1).
				Str("name", voiceName).
				Str("field", "loop_mode").
				Msg("reverse playback exported as forward audio; SFZ has no reverse equivalent")
		}

		dcaSus := hdrBytes[disk.VoiceDCASusOffset]
		dcaEnd := hdrBytes[disk.VoiceDCAEndOffset]
		var dcaRates [disk.EnvelopeStages]byte
		copy(dcaRates[:], hdrBytes[disk.VoiceDCARateOffset:disk.VoiceDCARateOffset+disk.EnvelopeStages])
		var dcaStops [disk.EnvelopeStages]byte
		copy(dcaStops[:], hdrBytes[disk.VoiceDCAStopOffset:disk.VoiceDCAStopOffset+disk.EnvelopeStages])

		dcfSus := hdrBytes[disk.VoiceDCFSusOffset]
		dcfEnd := hdrBytes[disk.VoiceDCFEndOffset]
		var dcfRates [disk.EnvelopeStages]byte
		copy(dcfRates[:], hdrBytes[disk.VoiceDCFRateOffset:disk.VoiceDCFRateOffset+disk.EnvelopeStages])
		var dcfStops [disk.EnvelopeStages]byte
		copy(dcfStops[:], hdrBytes[disk.VoiceDCFStopOffset:disk.VoiceDCFStopOffset+disk.EnvelopeStages])

		lfoWave := int(hdrBytes[disk.VoiceLFONameOffset] & disk.LFOWaveformMask)
		lfoRate := hdrBytes[disk.VoiceLFORateOffset]
		// lfo_delay is a 2-byte little-endian field per spec §2-1
		// (range 0..65535); a 1-byte read silently truncates values >255.
		lfoDelay := binary.LittleEndian.Uint16(hdrBytes[disk.VoiceLFODelayOffset : disk.VoiceLFODelayOffset+2])
		lfoAtck := hdrBytes[disk.VoiceLFOAtckOffset]
		lfoPitch := hdrBytes[disk.VoiceLFODCPOffset]
		lfoAmp := hdrBytes[disk.VoiceLFODCAOffset]
		lfoFilter := hdrBytes[disk.VoiceLFODCFOffset]
		lfoQ := hdrBytes[disk.VoiceLFODCQOffset]

		dcaLevelKF := disk.KFByteToDisplay(hdrBytes[disk.VoiceDCAKFOffset])
		dcaRateKF := disk.KFByteToDisplay(hdrBytes[disk.VoiceDCARSOffset])
		dcfLevelKF := disk.KFByteToDisplay(hdrBytes[disk.VoiceDCFKFOffset])
		dcfRateKF := disk.KFByteToDisplay(hdrBytes[disk.VoiceDCFRSOffset])
		velDCAKF := disk.KFByteToDisplay(hdrBytes[disk.VoiceVelDCAKFOffset])
		velDCFKF := disk.KFByteToDisplay(hdrBytes[disk.VoiceVelDCFKFOffset])
		velDCQKF := int8(hdrBytes[disk.VoiceVelDCQKFOffset]) //nolint:gosec // G115: intentional two's complement reinterpretation
		velDCARS := int8(hdrBytes[disk.VoiceVelDCARSOffset]) //nolint:gosec // G115: intentional two's complement reinterpretation
		velDCFRS := int8(hdrBytes[disk.VoiceVelDCFRSOffset]) //nolint:gosec // G115: intentional two's complement reinterpretation

		hasLoop := loopSus < disk.NoSustainLoop && loopSt0 < loopEd0

		loopStart := -1
		loopEnd := -1
		if hasLoop {
			loopStart = int(loopSt0)
			loopEnd = int(loopEd0)
		}

		// Preserve the voice's root note (byte 0xB0) as the WAV SMPL
		// chunk's MIDIUnityNote so a round-trip FZV -> WAV -> FZV does
		// not lose this metadata. Without this the writer falls back to
		// the hardcoded middle C default.
		voiceRoot := hdrBytes[disk.VoiceKeyCentOffset]

		wavFile := &wav.File{
			SampleRate:    sampleRate,
			Samples:       samples,
			LoopStart:     loopStart,
			LoopEnd:       loopEnd,
			MIDIUnityNote: voiceRoot,
		}

		// Build a filesystem-safe, unique WAV filename. The counter is
		// kept on the original (pre-suffix) stem so a name shared by N
		// voices produces N distinct suffixes ("X", "X-1", "X-2", ...).
		// Incrementing on the already-suffixed stem would let voices 2
		// and 3 collide on "X-1" because the bare "X" counter never
		// advances past 1.
		stem := sanitizeFilename(voiceName, fmt.Sprintf("VOICE_%d", slotIdx+1))
		count := seenWavName[stem]
		seenWavName[stem]++
		if count > 0 {
			stem = fmt.Sprintf("%s-%d", stem, count)
		}
		wavName := stem + ".wav"
		wavPath := filepath.Join(outputDir, wavName)
		var wavBuf bytes.Buffer
		if err := wav.Write(&wavBuf, wavFile); err != nil {
			return fmt.Errorf("sfzexport: writing WAV for voice %d: %w", slotIdx+1, err)
		}
		if err := fileutil.WriteAtomic(wavPath, wavBuf.Bytes()); err != nil {
			return fmt.Errorf("sfzexport: %w", err)
		}

		// Bank metadata: route through the voice's first BankSite so
		// multi-bank dumps surface the bank that actually owns the voice
		// (spec §2-2, vp[]). Single-bank dumps yield the trivial site
		// {0, slotIdx}; multi-bank dumps may resolve to any (bank, split)
		// pair. If a voice has no bank site (orphan header) we fall back
		// to bank-0/slot defaults so the SFZ region is still emitted.
		var (
			keyLow, keyHigh, keyCent uint8
			velLow, velHigh          uint8
			gchn                     uint8
		)
		velLow = disk.DefaultVelLow
		velHigh = disk.DefaultVelHigh
		gchn = disk.PolyphonicAudioOut
		if sites := fzutil.FindBankSitesForVoice(data, hdr, slotIdx); len(sites) > 0 {
			site := sites[0]
			bank := fzutil.BankSliceAt(data, site.BankIdx)
			if bank != nil {
				keyLow = bank[disk.BankKeyLowOffset+site.SplitIdx]
				keyHigh = bank[disk.BankKeyHighOffset+site.SplitIdx]
				keyCent = bank[disk.BankKeyCentOffset+site.SplitIdx]
				velLow = bank[disk.BankVelLowOffset+site.SplitIdx]
				velHigh = bank[disk.BankVelHighOffset+site.SplitIdx]
				gchn = bank[disk.BankAudioOutOffset+site.SplitIdx]
			}
		} else {
			log.Warn().
				Int("voice", slotIdx+1).
				Str("name", voiceName).
				Msg("voice has no bank reference; emitting region with default metadata")
		}

		semitones, cents := dcpToTransposeAndTune(dcp)

		fmt.Fprintf(&sfzBuf, "// Voice %d: %s\n", slotIdx+1, voiceName)
		fmt.Fprintf(&sfzBuf, "// DCA: sustain=%d end=%d rates=[%s] stops=[%s]\n",
			dcaSus, dcaEnd, formatByteArray(dcaRates[:]), formatByteArray(dcaStops[:]))
		fmt.Fprintf(&sfzBuf, "// DCF: sustain=%d end=%d rates=[%s] stops=[%s]\n",
			dcfSus, dcfEnd, formatByteArray(dcfRates[:]), formatByteArray(dcfStops[:]))
		fmt.Fprintf(&sfzBuf, "// LFO: wave=%s rate=%d delay=%d attack=%d pitch=%d amp=%d filter=%d q=%d\n",
			lfoWaveformName(lfoWave), lfoRate, lfoDelay, lfoAtck, lfoPitch, lfoAmp, lfoFilter, lfoQ)
		fmt.Fprintf(&sfzBuf, "// Modulation: dca_level_kf=%d dca_rate_kf=%d dcf_level_kf=%d dcf_rate_kf=%d vel_dca_kf=%d vel_dcf_kf=%d vel_dcq_kf=%d vel_dca_rs=%d vel_dcf_rs=%d\n",
			dcaLevelKF, dcaRateKF, dcfLevelKF, dcfRateKF, velDCAKF, velDCFKF, velDCQKF, velDCARS, velDCFRS)
		// Emit loopxf/looptm only when a sustain loop is selected and at
		// least one of the two is non-zero; otherwise the line is noise
		// (spec §2-1: loopxf=0 disables cross-fade, looptm is unused
		// without a multi-loop). SFZ has no opcode for either, so the
		// values are preserved as an informational comment alongside
		// DCA/DCF/LFO/Modulation/Playback.
		if loopSus < disk.NoSustainLoop && (loopXF > 0 || loopTm > 0) {
			fmt.Fprintf(&sfzBuf, "// Loop: xfade=%d time=%d\n", loopXF, loopTm)
		}
		fmt.Fprintf(&sfzBuf, "// Playback: %s\n", playbackModeName(playbackMode))
		sfzBuf.WriteString("\n<region>\n")
		fmt.Fprintf(&sfzBuf, "sample=%s\n", wavName)
		fmt.Fprintf(&sfzBuf, "lokey=%d hikey=%d pitch_keycenter=%d\n", keyLow, keyHigh, keyCent)

		if semitones != 0 {
			fmt.Fprintf(&sfzBuf, "transpose=%d\n", semitones)
		}
		if cents != 0 {
			fmt.Fprintf(&sfzBuf, "tune=%d\n", cents)
		}
		if dcf != disk.DCFMaxOffset {
			fmt.Fprintf(&sfzBuf, "cutoff=%d\n", dcf)
		}
		if dcq != 0 {
			fmt.Fprintf(&sfzBuf, "resonance=%d\n", dcq)
		}

		if mg, ok := gchnMap[gchn]; ok {
			fmt.Fprintf(&sfzBuf, "mutegroup=%d\n", mg)
		}

		// Velocity: emit explicit lovel/hivel for anything other than the
		// fizzle default (1, 127). This preserves (0, 0) silencing and any
		// other non-default range across re-import.
		if velLow != disk.DefaultVelLow || velHigh != disk.DefaultVelHigh {
			fmt.Fprintf(&sfzBuf, "lovel=%d hivel=%d\n", velLow, velHigh)
		}

		if !hasLoop {
			sfzBuf.WriteString("loop_mode=one_shot\n")
		} else {
			fmt.Fprintf(&sfzBuf, "loop_start=%d loop_end=%d\n", loopSt0, loopEd0)
		}

		sfzBuf.WriteString("\n")
	}

	sfzPath := filepath.Join(outputDir, name+".sfz")
	if err := fileutil.WriteAtomic(sfzPath, []byte(sfzBuf.String())); err != nil {
		return fmt.Errorf("sfzexport: %w", err)
	}

	return nil
}

// dcpToTransposeAndTune splits the FZ DCP pitch field (1/256-semitone units)
// into integer-semitone transpose and fine-tune cents. Cents that round to
// exactly +/-100 carry into the semitones bucket so the SFZ tune opcode
// stays within its documented -100..+100 range. The semitones result is
// clamped to the SFZ transpose range (-127..+127); a corrupt DCP byte can
// otherwise emit out-of-spec values that downstream SFZ parsers reject.
func dcpToTransposeAndTune(dcp int16) (semitones, cents int) {
	semitones = int(dcp) / disk.SemitoneDCPScale
	remainder := int(dcp) % disk.SemitoneDCPScale
	cents = int(math.Round(float64(remainder) * 100.0 / float64(disk.SemitoneDCPScale)))
	switch cents {
	case 100:
		semitones++
		cents = 0
	case -100:
		semitones--
		cents = 0
	}
	if semitones > disk.MaxMIDINote || semitones < -disk.MaxMIDINote {
		log.Warn().
			Int16("dcp", dcp).
			Int("semitones", semitones).
			Msg("transpose exceeds SFZ -127..+127 range; clamping")
		if semitones > disk.MaxMIDINote {
			semitones = disk.MaxMIDINote
		} else {
			semitones = -disk.MaxMIDINote
		}
	}
	return semitones, cents
}

// sanitizeFilename maps a voice name to a filesystem-safe stem. Any rune
// outside [A-Za-z0-9 _-] is replaced with underscore, leading and trailing
// whitespace is stripped, and internal whitespace runs are collapsed to a
// single space (so the on-disk filename matches what the SFZ parser
// reconstructs after splitting on whitespace). An empty result falls back
// to the supplied default. This also prevents a corrupt or adversarial FZF
// whose voice header contains "../" or other path metacharacters from
// writing outside outputDir.
func sanitizeFilename(name, fallback string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z',
			r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == ' ', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if out == "" {
		return fallback
	}
	return out
}

// buildMuteGroupMap walks every (bank, split) site across all bank sectors
// and collects the distinct single-bit gchn values. Multi-bank dumps may
// reference a voice from several banks with different gchn values; the
// SFZ mutegroup is per region, so each unique gchn gets its own group ID
// and the export loop picks the gchn that corresponds to the *site* used
// for that region (sfzexport falls back to the first BankSite when
// emitting key/vel/gchn).
func buildMuteGroupMap(data []byte, hdr *fzutil.FZFHeader, names []string) map[uint8]int {
	seen := map[uint8]int{}
	nextGroup := 1
	for v := range hdr.NVoice {
		sites := fzutil.FindBankSitesForVoice(data, hdr, v)
		if len(sites) == 0 {
			continue
		}
		site := sites[0]
		bank := fzutil.BankSliceAt(data, site.BankIdx)
		if bank == nil {
			continue
		}
		gchn := bank[disk.BankAudioOutOffset+site.SplitIdx]
		if gchn == disk.PolyphonicAudioOut || gchn == 0 {
			continue
		}
		if bits.OnesCount8(gchn) != 1 {
			// Multi-bit gchn assigns the voice to several generators at
			// once. SFZ mutegroup is a single integer, so the multi-bit
			// grouping cannot be reproduced and the voice falls back to
			// polyphonic on round-trip.
			name := ""
			if v < len(names) {
				name = names[v]
			}
			log.Warn().
				Int("voice", v+1).
				Str("name", name).
				Str("field", "gchn").
				Uint8("value", gchn).
				Msg("multi-generator gchn cannot be expressed as an SFZ mutegroup; voice exported as polyphonic")
			continue
		}
		if _, ok := seen[gchn]; !ok {
			seen[gchn] = nextGroup
			nextGroup++
		}
	}
	return seen
}

// warnSharedVoicePointers warns when a voice slot is referenced by more
// than one (bank, split) site across the full set of bank sectors. On the
// hardware this is how a single voice can occupy several key splits, both
// within one bank and across banks; on SFZ export only the first site is
// rendered (one region per slot), so the additional sharing is lost. A
// re-import via sfzconvert produces a single-bank dump with identity
// vp[]=i, never restoring the original spread.
func warnSharedVoicePointers(data []byte, hdr *fzutil.FZFHeader, names []string) {
	for v := range hdr.NVoice {
		sites := fzutil.FindBankSitesForVoice(data, hdr, v)
		if len(sites) <= 1 {
			continue
		}
		name := ""
		if v < len(names) {
			name = names[v]
		}
		extra := make([]string, 0, len(sites))
		for _, s := range sites {
			extra = append(extra, fmt.Sprintf("bank=%d split=%d", s.BankIdx, s.SplitIdx))
		}
		log.Warn().
			Int("voice", v+1).
			Str("name", name).
			Int("sites", len(sites)).
			Strs("locations", extra).
			Str("field", "vp").
			Msg("voice slot is referenced from multiple bank sites; key-split sharing is lost on SFZ export")
	}
}

func formatByteArray(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("%d", v)
	}
	return strings.Join(parts, ",")
}
