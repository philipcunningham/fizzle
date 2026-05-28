// Package fzvinfo implements the 'fizzle fzv info' command. It reads a voice
// file and returns its parameters as structured data, with a separate renderer
// for terminal output.
package fzvinfo

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/render"
)

type lfoParams struct {
	waveform    string
	phaseSync   bool
	rate        uint8
	attack      uint8
	delay       uint16
	depthPitch  uint8
	depthAmp    uint8
	depthFilter uint8
	depthQ      uint8
}

var lfoWaveforms = map[uint8]string{
	disk.LFOSine:      "Sine",
	disk.LFOSawUp:     "Saw Up",
	disk.LFOSawDown:   "Saw Down",
	disk.LFOTriangle:  "Triangle",
	disk.LFORectangle: "Rectangle",
	disk.LFORandom:    "Random",
}

// allMatch reports whether every byte in b equals val. Used by the
// DCA/DCF "default envelope" detector to recognise the canonical
// pattern (e.g. stages 1-7 all at rate 0xC0, stops 0) without writing
// out an 8-element comparison at each call site.
func allMatch(b []byte, val byte) bool {
	for _, c := range b {
		if c != val {
			return false
		}
	}
	return true
}

// rateStr formats an envelope rate byte using the FZ-10M front-panel
// display scale (0-99), discarding the sign bit. The mapping is in
// pkg/disk.RateByteToDisplay; this is a render-side convenience.
func rateStr(b uint8) string {
	return fmt.Sprintf("%d", disk.RateByteToDisplay(b))
}

// kfDisplay converts a signed key-follow byte into its hardware display
// value (-15..+15). VoiceParams stores these fields as int8 so JSON sees
// signed values; disk.KFByteToDisplay accepts a uint8 and reinterprets via
// int8 internally, so we cast on the way in.
func kfDisplay(b int8) int {
	return disk.KFByteToDisplay(uint8(b)) //nolint:gosec // G115: intentional int8 -> uint8 reinterpretation; KFByteToDisplay treats the byte as two's-complement
}

// VoiceParams holds the parsed parameters of an FZV voice file.
type VoiceParams struct {
	Name         string  `json:"name"`
	SampleRate   uint32  `json:"sample_rate"`
	Samples      uint32  `json:"samples"`
	Duration     float64 `json:"duration"`
	PlaybackMode string  `json:"playback_mode"`
	LoopMode     uint16  `json:"-"`
	GenStart     uint32  `json:"-"`
	GenEnd       uint32  `json:"-"`
	WaveEnd      uint32  `json:"-"`
	Transpose    int16   `json:"transpose"`
	KeyLow       uint8   `json:"key_low"`
	KeyHigh      uint8   `json:"key_high"`
	KeyCentre    uint8   `json:"root_note"`
	FilterCutoff uint8   `json:"cutoff"`
	FilterQ      uint8   `json:"resonance"`

	DCADefault bool                       `json:"-"`
	DCASilent  bool                       `json:"-"`
	DCASustain uint8                      `json:"dca_sustain"`
	DCAEnd     uint8                      `json:"dca_end"`
	DCARates   [disk.EnvelopeStages]uint8 `json:"dca_rates"`
	DCAStops   [disk.EnvelopeStages]uint8 `json:"dca_stops"`

	DCFDefault bool                       `json:"-"`
	DCFSustain uint8                      `json:"dcf_sustain"`
	DCFEnd     uint8                      `json:"dcf_end"`
	DCFRates   [disk.EnvelopeStages]uint8 `json:"dcf_rates"`
	DCFStops   [disk.EnvelopeStages]uint8 `json:"dcf_stops"`

	LFOWaveform    string `json:"lfo_waveform"`
	LFOPhaseSync   bool   `json:"lfo_phase_sync"`
	LFORate        uint8  `json:"lfo_rate"`
	LFOAttack      uint8  `json:"lfo_attack"`
	LFODelay       uint16 `json:"lfo_delay"`
	LFODepthPitch  uint8  `json:"lfo_depth_pitch"`
	LFODepthAmp    uint8  `json:"lfo_depth_amp"`
	LFODepthFilter uint8  `json:"lfo_depth_filter"`
	LFODepthQ      uint8  `json:"lfo_depth_q"`

	// DCALevelKF/DCARateKF/DCFLevelKF/DCFRateKF and VelDCAKF/VelDCFKF (with
	// the sibling VelDCQKF/VelDCARS/VelDCFRS below) are signed -127..+127
	// key-follow / rate-scaling / initial-touch modulation amounts per spec
	// §2-1 (offsets 0xa6-0xa9, 0xaa, 0xac). Stored as raw signed bytes; JSON
	// renders them as signed ints so consumers see -10 rather than 246.
	DCALevelKF int8 `json:"dca_level_kf"`
	DCARateKF  int8 `json:"dca_rate_kf"`
	DCFLevelKF int8 `json:"dcf_level_kf"`
	DCFRateKF  int8 `json:"dcf_rate_kf"`
	VelDCAKF   int8 `json:"vel_dca_kf"`
	VelDCFKF   int8 `json:"vel_dcf_kf"`
	VelDCQKF   int8 `json:"vel_dcq_kf"`
	VelDCARS   int8 `json:"vel_dca_rs"`
	VelDCFRS   int8 `json:"vel_dcf_rs"`

	HasActiveLoop bool   `json:"has_loop"`
	LoopSustain   uint8  `json:"loop_sustain"`
	LoopRelease   uint8  `json:"loop_release"`
	LoopStart     uint32 `json:"loop_start"`
	LoopEnd       uint32 `json:"loop_end"`
	// LoopXF is the cross-fade duration for the active sustain loop
	// (loopxf[loop_sus], spec §2-1, range 0-1023; 0 disables cross-fade).
	LoopXF uint16 `json:"loop_xfade"`
	// LoopTm is the multi-loop time for the active sustain loop
	// (looptm[loop_sus], spec §2-1, range 1-1022; step 16 ms).
	LoopTm uint16 `json:"loop_time"`
}

// ParseVoiceInFZF parses a named voice's parameters directly from FZF data
// without extracting audio. This is used for voices whose audio is on a
// different disk in a multi-disk full dump.
func ParseVoiceInFZF(fzfData []byte, voiceName string) (*VoiceParams, error) {
	hdr, err := fzutil.ParseFZFHeader(fzfData)
	if err != nil {
		return nil, fmt.Errorf("fzvinfo: %w", err)
	}
	voiceSectors := disk.VoiceAreaSectors(hdr.NVoice)
	voiceAreaEnd := hdr.VoiceAreaStart + voiceSectors*disk.SectorSize
	if len(fzfData) < voiceAreaEnd {
		return nil, fmt.Errorf("fzvinfo: FZF too small for voice area")
	}
	targets, _, err := fzutil.ResolveVoiceTargets(fzfData, hdr, []string{voiceName}, false)
	if err != nil {
		return nil, fmt.Errorf("fzvinfo: %w", err)
	}
	off := disk.VoiceSlotOffset(hdr.VoiceAreaStart, targets[0])
	if off+disk.VoiceHeaderUsed > len(fzfData) {
		return nil, fmt.Errorf("fzvinfo: voice %q header truncated", voiceName)
	}
	voiceHdr := make([]byte, disk.SectorSize)
	copy(voiceHdr, fzfData[off:off+disk.VoiceHeaderUsed])
	return parseHeader(voiceHdr, voiceName)
}

// Parse reads the FZV file at path and returns its parameters as structured data.
func Parse(path string) (*VoiceParams, error) {
	data, err := fzutil.ReadBounded(path, fzutil.MaxReadSize)
	if err != nil {
		return nil, fmt.Errorf("fzvinfo: reading %q: %w", path, err)
	}
	// Use the structural plausibility check (active playback mode, sane wave
	// pointers, valid envelope stages) rather than the strict
	// IsPlausibleVoiceHeader. Real-world FZF dumps sometimes contain voices
	// whose 12-byte name field is zeroed or otherwise non-printable. Once
	// extracted via `fzf unpack`, these are still valid voices and should
	// parse here even though they fail the strict name check. The
	// IsPlausibleVoiceSlot bytes-level checks reject non-voice data (FZF
	// bank sectors, text files, etc.) just as effectively.
	if len(data) < disk.SectorSize {
		return nil, fmt.Errorf("fzvinfo: file too small to be a voice file")
	}
	if !disk.IsPlausibleVoiceSlot(data[:disk.VoiceHeaderUsed]) {
		return nil, fmt.Errorf("fzvinfo: %q does not look like a voice file", path)
	}
	return parseHeader(data[:disk.SectorSize], path)
}

func parseHeader(hdr []byte, source string) (*VoiceParams, error) {
	_ = source // retained for future error messages that need a path
	nameBytes := hdr[disk.VoiceNameOffset : disk.VoiceNameOffset+disk.LabelSize]
	var name string
	if disk.IsPrintableName(nameBytes) {
		name = disk.TrimPadded(nameBytes)
	}
	if name == "" {
		name = "(unnamed)"
	}

	sampIdx := hdr[disk.VoiceSampOffset]
	var rate uint32
	if int(sampIdx) < disk.NumSampleRates() {
		rate = disk.SampleRate(sampIdx)
	}

	waveStart := binary.LittleEndian.Uint32(hdr[disk.VoiceWaveStartOffset : disk.VoiceWaveStartOffset+4])
	waveEnd := binary.LittleEndian.Uint32(hdr[disk.VoiceWaveEndOffset : disk.VoiceWaveEndOffset+4])
	genStart := binary.LittleEndian.Uint32(hdr[disk.VoiceGenStartOffset : disk.VoiceGenStartOffset+4])
	genEnd := binary.LittleEndian.Uint32(hdr[disk.VoiceGenEndOffset : disk.VoiceGenEndOffset+4])
	loopMode := binary.LittleEndian.Uint16(hdr[disk.VoiceLoopModeOffset : disk.VoiceLoopModeOffset+2])
	dcp := binary.LittleEndian.Uint16(hdr[disk.VoiceDCPOffset : disk.VoiceDCPOffset+2])
	dcf := hdr[disk.VoiceDCFOffset]
	dcq := hdr[disk.VoiceDCQOffset]

	hwid := hdr[disk.VoiceKeyHighOffset]
	lwid := hdr[disk.VoiceKeyLowOffset]
	cent := hdr[disk.VoiceKeyCentOffset]

	lfo := parseLFO(hdr)

	// Audio length is waveEnd-waveStart samples. For standalone FZV files
	// waveStart is 0 and waveEnd is the full sample count, but for voices
	// parsed via ParseVoiceInFZF the wave pointers are cumulative addresses
	// in the FZF audio area, so reporting waveEnd alone inflates both the
	// duration (by waveStart/rate) and the sample count (by waveStart).
	var samples uint32
	if waveEnd >= waveStart {
		samples = waveEnd - waveStart
	}
	var duration float64
	if rate > 0 {
		duration = float64(samples) / float64(rate)
	}

	// Spec §2-1: wavst, genst, gened, waved are all word-addressed pointers
	// satisfying wavst <= genst <= gened <= waved. For standalone FZVs
	// waveStart is 0 and this is a no-op; for voices borrowed from an FZF
	// audio area via ParseVoiceInFZF, the raw pointers are cumulative and
	// must be localised to 0..N for display so the "Gen range" line shows
	// voice-local sample addresses, not multi-thousand FZF-area offsets.
	// This is the sibling of the Samples/Duration localisation above (F7).
	if waveEnd >= waveStart {
		waveEnd -= waveStart
	} else {
		waveEnd = 0
	}
	if genStart >= waveStart {
		genStart -= waveStart
	} else {
		genStart = 0
	}
	if genEnd >= waveStart {
		genEnd -= waveStart
	} else {
		genEnd = 0
	}

	// Delegate to disk.PlaybackModeName so fzvinfo, fzfinfo, and sfzexport
	// agree on the canonical lowercase identifier (spec: "no_sound",
	// "normal", "normal_variant", "reverse", "cue", "synthesized"). For
	// truly unrecognised modes, fall back to a hex literal that surfaces
	// the raw value rather than swallowing it.
	modeName := disk.PlaybackModeName(loopMode)
	if modeName == disk.PlaybackModeNameUnknown {
		modeName = fmt.Sprintf("unknown (0x%04x)", loopMode)
	}

	transpose := int16(dcp) / disk.SemitoneDCPScale //nolint:gosec // G115: intentional uint16-to-int16 reinterpretation for signed DCP value

	dcaSus, dcaEnd, dcaRates, dcaStops := parseEnvelope(hdr, disk.VoiceDCASusOffset, disk.VoiceDCAEndOffset, disk.VoiceDCARateOffset, disk.VoiceDCAStopOffset)
	dcfSus, dcfEnd, dcfRates, dcfStops := parseEnvelope(hdr, disk.VoiceDCFSusOffset, disk.VoiceDCFEndOffset, disk.VoiceDCFRateOffset, disk.VoiceDCFStopOffset)

	dcaDefault := dcaSus == 0 && dcaEnd == disk.HoldIndefinitely &&
		hdr[disk.VoiceDCARateOffset] == disk.EnvelopeMaxRate &&
		allMatch(hdr[disk.VoiceDCARateOffset+1:disk.VoiceDCARateOffset+disk.EnvelopeStages], disk.EnvelopeIdleRate) &&
		hdr[disk.VoiceDCAStopOffset] == disk.EnvelopeFullLevel &&
		allMatch(hdr[disk.VoiceDCAStopOffset+1:disk.VoiceDCAStopOffset+disk.EnvelopeStages], 0)
	silentEnvelope := dcaSus == 0 && dcaEnd == 0

	dcfDefault := dcfSus == 0 && dcfEnd == disk.HoldIndefinitely &&
		hdr[disk.VoiceDCFRateOffset] == disk.EnvelopeMaxRate &&
		allMatch(hdr[disk.VoiceDCFRateOffset+1:disk.VoiceDCFRateOffset+disk.EnvelopeStages], 0) &&
		dcf == disk.DCFMaxOffset &&
		allMatch(hdr[disk.VoiceDCFStopOffset:disk.VoiceDCFStopOffset+disk.EnvelopeStages], disk.EnvelopeFullLevel)

	loopInfo := parseLoop(hdr, loopMode)

	return &VoiceParams{
		Name:         name,
		SampleRate:   rate,
		Samples:      samples,
		Duration:     duration,
		PlaybackMode: modeName,
		LoopMode:     loopMode,
		GenStart:     genStart,
		GenEnd:       genEnd,
		WaveEnd:      waveEnd,
		Transpose:    transpose,
		KeyLow:       lwid,
		KeyHigh:      hwid,
		KeyCentre:    cent,
		FilterCutoff: dcf,
		FilterQ:      dcq,

		DCADefault: dcaDefault,
		DCASilent:  silentEnvelope,
		DCASustain: dcaSus,
		DCAEnd:     dcaEnd,
		DCARates:   dcaRates,
		DCAStops:   dcaStops,

		DCFDefault: dcfDefault,
		DCFSustain: dcfSus,
		DCFEnd:     dcfEnd,
		DCFRates:   dcfRates,
		DCFStops:   dcfStops,

		LFOWaveform:    lfo.waveform,
		LFOPhaseSync:   lfo.phaseSync,
		LFORate:        lfo.rate,
		LFOAttack:      lfo.attack,
		LFODelay:       lfo.delay,
		LFODepthPitch:  lfo.depthPitch,
		LFODepthAmp:    lfo.depthAmp,
		LFODepthFilter: lfo.depthFilter,
		LFODepthQ:      lfo.depthQ,

		DCALevelKF: int8(hdr[disk.VoiceDCAKFOffset]),    //nolint:gosec // G115: intentional two's complement reinterpretation
		DCARateKF:  int8(hdr[disk.VoiceDCARSOffset]),    //nolint:gosec // G115: intentional two's complement reinterpretation
		DCFLevelKF: int8(hdr[disk.VoiceDCFKFOffset]),    //nolint:gosec // G115: intentional two's complement reinterpretation
		DCFRateKF:  int8(hdr[disk.VoiceDCFRSOffset]),    //nolint:gosec // G115: intentional two's complement reinterpretation
		VelDCAKF:   int8(hdr[disk.VoiceVelDCAKFOffset]), //nolint:gosec // G115: intentional two's complement reinterpretation
		VelDCFKF:   int8(hdr[disk.VoiceVelDCFKFOffset]), //nolint:gosec // G115: intentional two's complement reinterpretation
		VelDCQKF:   int8(hdr[disk.VoiceVelDCQKFOffset]), //nolint:gosec // G115: intentional two's complement reinterpretation
		VelDCARS:   int8(hdr[disk.VoiceVelDCARSOffset]), //nolint:gosec // G115: intentional two's complement reinterpretation
		VelDCFRS:   int8(hdr[disk.VoiceVelDCFRSOffset]), //nolint:gosec // G115: intentional two's complement reinterpretation

		HasActiveLoop: loopInfo.hasLoop,
		LoopSustain:   loopInfo.sustain,
		LoopRelease:   loopInfo.release,
		LoopStart:     loopInfo.start,
		LoopEnd:       loopInfo.end,
		LoopXF:        loopInfo.xfade,
		LoopTm:        loopInfo.time,
	}, nil
}
func parseEnvelope(hdr []byte, susOff, endOff, rateOff, stopOff int) (sustain, end uint8, rates, stops [disk.EnvelopeStages]uint8) {
	sustain = hdr[susOff]
	end = hdr[endOff]
	for i := range disk.EnvelopeStages {
		rates[i] = hdr[rateOff+i]
		stops[i] = hdr[stopOff+i]
	}
	return
}

func parseLFO(hdr []byte) lfoParams {
	lfoName := hdr[disk.VoiceLFONameOffset]
	return lfoParams{
		waveform:    lfoWaveforms[lfoName&disk.LFOWaveformMask],
		phaseSync:   lfoName&disk.LFOPhaseFlag != 0,
		rate:        hdr[disk.VoiceLFORateOffset],
		attack:      hdr[disk.VoiceLFOAtckOffset],
		delay:       binary.LittleEndian.Uint16(hdr[disk.VoiceLFODelayOffset : disk.VoiceLFODelayOffset+2]),
		depthPitch:  hdr[disk.VoiceLFODCPOffset],
		depthAmp:    hdr[disk.VoiceLFODCAOffset],
		depthFilter: hdr[disk.VoiceLFODCFOffset],
		depthQ:      hdr[disk.VoiceLFODCQOffset],
	}
}

type loopInfo struct {
	hasLoop    bool
	sustain    uint8
	release    uint8
	start, end uint32
	xfade      uint16
	time       uint16
}

// parseLoop reads the loop configuration from the voice header. loop_sus
// (0..7) selects the active sustain-loop pair; 8 means no sustain loop.
// loop_end (0..7) selects the release-loop pair; 8 means "execute all 8
// loops". The reported loop_start/loop_end addresses come from the pair
// selected by loop_sus, and the reported xfade/time come from the matching
// loopxf[]/looptm[] entries.
func parseLoop(hdr []byte, loopMode uint16) loopInfo {
	info := loopInfo{
		sustain: hdr[disk.VoiceLoopSusOffset],
		release: hdr[disk.VoiceLoopEndOffset],
	}
	if loopMode == disk.PlaybackModeNoSound {
		return info
	}
	if info.sustain < disk.NoSustainLoop {
		stOff := disk.VoiceLoopSt0Offset + int(info.sustain)*4
		edOff := disk.VoiceLoopEd0Offset + int(info.sustain)*4
		// Mask the spec's flag bits (loop-fine in upper 8 of loopst,
		// skip in MSB of looped) so we report just the sample addresses.
		rawSt := binary.LittleEndian.Uint32(hdr[stOff : stOff+4])
		rawEd := binary.LittleEndian.Uint32(hdr[edOff : edOff+4])
		info.start = disk.LoopStartAddress(rawSt)
		info.end = disk.LoopEndAddress(rawEd)
		xfOff := disk.VoiceLoopXFOffset + int(info.sustain)*disk.LoopXFEntrySize
		tmOff := disk.VoiceLoopTmOffset + int(info.sustain)*disk.LoopTmEntrySize
		info.xfade = binary.LittleEndian.Uint16(hdr[xfOff : xfOff+disk.LoopXFEntrySize])
		info.time = binary.LittleEndian.Uint16(hdr[tmOff : tmOff+disk.LoopTmEntrySize])
		info.hasLoop = info.start < info.end
	}
	return info
}

// RenderJSON writes the voice parameters as indented JSON to w.
func RenderJSON(w io.Writer, p *VoiceParams) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(p)
}

// Render writes a human-readable parameter summary to w.
func Render(w io.Writer, p *VoiceParams) {
	render.Printf(w, "Voice:       %s\n", p.Name)
	render.Printf(w, "Sample rate: %d Hz\n", p.SampleRate)
	render.Printf(w, "Samples:     %d\n", p.Samples)
	render.Printf(w, "Duration:    %.3f s\n", p.Duration)
	if p.LoopMode != disk.PlaybackModeNormal {
		render.Printf(w, "Loop mode:   %s\n", p.PlaybackMode)
	}
	if p.GenStart > 0 || (p.WaveEnd > 0 && p.GenEnd+disk.GenEndGuard < p.WaveEnd) {
		render.Printf(w, "Gen range:   %d to %d samples\n", p.GenStart, p.GenEnd)
	}
	if p.Transpose != 0 {
		render.Printf(w, "Transpose:   %+d semitones\n", p.Transpose)
	}

	render.Printf(w, "Key window:  %s to %s   Root: %s\n",
		render.NoteName(p.KeyLow), render.NoteName(p.KeyHigh), render.NoteName(p.KeyCentre))

	if p.FilterCutoff != disk.DCFMaxOffset || p.FilterQ != 0 {
		render.Printf(w, "Filter:      cutoff=%d  resonance=%d\n", p.FilterCutoff, p.FilterQ)
	}

	if !p.DCADefault {
		if p.DCASilent {
			render.Printf(w, "\nWarning: dca_sus=0 and dca_end=0. This voice will be silent on hardware.\n")
		}
		render.Printf(w, "\nEnvelope (DCA):\n")
		render.Printf(w, "  Sustain: %d   End: %d\n", p.DCASustain, p.DCAEnd)
		renderEnvelope(w, p.DCARates, p.DCAStops)
	}

	if !p.DCFDefault {
		render.Printf(w, "\nEnvelope (DCF):\n")
		render.Printf(w, "  Sustain: %d   End: %d\n", p.DCFSustain, p.DCFEnd)
		renderEnvelope(w, p.DCFRates, p.DCFStops)
	}

	if p.LFODepthPitch > 0 || p.LFODepthAmp > 0 || p.LFODepthFilter > 0 || p.LFODepthQ > 0 {
		render.Printf(w, "\nLFO:\n")
		render.Printf(w, "  Waveform: %s", p.LFOWaveform)
		if p.LFOPhaseSync {
			render.Printf(w, " (phase sync)")
		}
		render.Println(w)
		render.Printf(w, "  Rate: %d   Attack: %d   Delay: %d\n",
			p.LFORate, p.LFOAttack, p.LFODelay)
		render.Printf(w, "  Depth: pitch=%d  amp=%d  filter=%d  q=%d\n", p.LFODepthPitch, p.LFODepthAmp, p.LFODepthFilter, p.LFODepthQ)
	}

	hasModRouting := kfDisplay(p.DCALevelKF) != 0 || kfDisplay(p.DCARateKF) != 0 ||
		kfDisplay(p.DCFLevelKF) != 0 || kfDisplay(p.DCFRateKF) != 0 ||
		int(p.VelDCAKF) != disk.VelSensitivityDefault || p.VelDCFKF != 0 ||
		p.VelDCQKF != 0 || p.VelDCARS != 0 || p.VelDCFRS != 0
	if hasModRouting {
		render.Printf(w, "\nModulation:\n")
		render.Printf(w, "  DCA: level KF=%+d  rate KF=%+d  vel sensitivity=%+d\n",
			kfDisplay(p.DCALevelKF), kfDisplay(p.DCARateKF), p.VelDCAKF)
		render.Printf(w, "  DCF: level KF=%+d  rate KF=%+d  vel sensitivity=%+d\n",
			kfDisplay(p.DCFLevelKF), kfDisplay(p.DCFRateKF), p.VelDCFKF)
		if p.VelDCQKF != 0 || p.VelDCARS != 0 || p.VelDCFRS != 0 {
			render.Printf(w, "  Vel: dcq KF=%+d  dca RS=%+d  dcf RS=%+d\n",
				p.VelDCQKF, p.VelDCARS, p.VelDCFRS)
		}
	}

	if p.HasActiveLoop {
		render.Printf(w, "\nLoop:\n")
		susStr := "none"
		if p.LoopSustain < disk.NoSustainLoop {
			susStr = fmt.Sprintf("%d", p.LoopSustain+1)
		}
		// loop_end values 0..7 pick a release-loop pair; 8 means
		// "execute all 8 loops" (the spec's release-fade semantics).
		var relStr string
		if p.LoopRelease < disk.NoReleaseLoop {
			relStr = fmt.Sprintf("loop %d", p.LoopRelease+1)
		} else {
			relStr = "all loops"
		}
		render.Printf(w, "  Sustain on: loop %s   Release: %s\n", susStr, relStr)
		if p.LoopMode != disk.PlaybackModeSynthesized && p.SampleRate > 0 {
			loopNum := int(p.LoopSustain) + 1
			durMs := float64(p.LoopEnd-p.LoopStart) / float64(p.SampleRate) * 1000
			render.Printf(w, "  Loop %d: %d to %d samples (%.0f ms)\n", loopNum, p.LoopStart, p.LoopEnd, durMs)
		}
		if p.LoopXF > 0 || p.LoopTm > 0 {
			render.Printf(w, "  Cross-fade: %d   Time: %d\n", p.LoopXF, p.LoopTm)
		}
	}
}

func renderEnvelope(w io.Writer, rates, stops [disk.EnvelopeStages]uint8) {
	t := table.NewWriter()
	t.SetOutputMirror(w)
	t.SetStyle(table.StyleLight)
	t.Style().Format.Header = text.FormatDefault
	t.Style().Options.DrawBorder = false
	t.Style().Options.SeparateHeader = false
	t.Style().Options.SeparateColumns = false

	rateRow := make(table.Row, 1, 1+disk.EnvelopeStages)
	rateRow[0] = "  Rates:"
	stopRow := make(table.Row, 1, 1+disk.EnvelopeStages)
	stopRow[0] = "  Stops:"
	for i := range disk.EnvelopeStages {
		rateRow = append(rateRow, rateStr(rates[i]))
		stopRow = append(stopRow, disk.StopByteToDisplay(stops[i]))
	}
	t.AppendRow(rateRow)
	t.AppendRow(stopRow)
	t.Render()
}

// ParseFZFVoice locates a voice by name in an FZF file and returns its
// parameters. The name match is case-insensitive and ignores trailing spaces.
func ParseFZFVoice(fzfPath, voiceName string) (*VoiceParams, error) {
	data, err := fzutil.ReadBounded(fzfPath, fzutil.MaxReadSize)
	if err != nil {
		return nil, fmt.Errorf("fzvinfo: reading %q: %w", fzfPath, err)
	}
	p, err := ParseVoiceInFZF(data, voiceName)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// Info reads the FZV file at path and writes a parameter summary to w.
func Info(path string, w io.Writer) error {
	params, err := Parse(path)
	if err != nil {
		return err
	}
	Render(w, params)
	return nil
}
