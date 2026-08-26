package webcore

import (
	"encoding/binary"
	"strings"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
)

func typeCode(displayName string) string {
	first, _, _ := strings.Cut(displayName, " ")
	return strings.ToLower(first)
}

func voiceParams(vp *fzvinfo.VoiceParams, voiceBytes []byte) map[string]any {
	wave := ""
	if idx := int(lfoNameByte(voiceBytes) & disk.LFOWaveformMask); idx < len(lfoWaveNames) {
		wave = lfoWaveNames[idx]
	}
	mode := vp.PlaybackMode
	if mode == "synthesized" {
		mode = "synth"
	}
	tune := 0
	sync := "off"
	if lfoNameByte(voiceBytes)&disk.LFOPhaseFlag != 0 {
		sync = "on"
	}
	if len(voiceBytes) >= disk.VoiceDCPOffset+2 {
		tune = int(int16(binary.LittleEndian.Uint16(voiceBytes[disk.VoiceDCPOffset : disk.VoiceDCPOffset+2]))) // #nosec G115 -- intentional signed reinterpretation
	}
	return map[string]any{
		fieldPlaybackMode: mode, fieldTune: disk.TuneWordToDisplay(int16(tune)),
		fieldRootKey: int(vp.KeyCentre), fieldKeyLow: int(vp.KeyLow), fieldKeyHigh: int(vp.KeyHigh),
		fieldCutoff: int(vp.FilterCutoff), fieldResonance: int(vp.FilterQ),
		fieldDcaLevelKF: disk.KFByteToDisplay(uint8(vp.DCALevelKF)),
		fieldDcaRateKF:  disk.KFByteToDisplay(uint8(vp.DCARateKF)),
		fieldDcfLevelKF: disk.KFByteToDisplay(uint8(vp.DCFLevelKF)),
		fieldDcfRateKF:  disk.KFByteToDisplay(uint8(vp.DCFRateKF)),
		fieldVelDcaKF:   int(vp.VelDCAKF), fieldVelDcfKF: int(vp.VelDCFKF),
		fieldVelDcqKF: disk.VelDCQByteToDisplay(uint8(vp.VelDCQKF)),
		fieldVelDcaRS: int(vp.VelDCARS), fieldVelDcfRS: int(vp.VelDCFRS),
		fieldLfoWave: wave, fieldLfoRate: int(vp.LFORate),
		fieldLfoDelay: disk.LFODelayWordToDisplay(vp.LFODelay),
		fieldLfoPitch: int(vp.LFODepthPitch), fieldLfoAmp: int(vp.LFODepthAmp),
		fieldLfoFilter: int(vp.LFODepthFilter), fieldLfoSync: sync,
	}
}

func cloneParams(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneParamValue(value)
	}
	return out
}

// Parameter snapshots currently contain scalar values. The collection cases
// keep the boundary ownership rule intact if a future schema field is grouped.
func cloneParamValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneParams(value)
	case []any:
		out := make([]any, len(value))
		for i := range value {
			out[i] = cloneParamValue(value[i])
		}
		return out
	case []int:
		return append([]int(nil), value...)
	case []string:
		return append([]string(nil), value...)
	default:
		return value
	}
}

func cloneVoiceDetail(in *VoiceDetail) *VoiceDetail {
	if in == nil {
		return nil
	}
	out := *in
	out.Loops = append([]LoopSnapshot(nil), in.Loops...)
	out.Dca.Rates = append([]int(nil), in.Dca.Rates...)
	out.Dca.Stops = append([]int(nil), in.Dca.Stops...)
	out.Dcf.Rates = append([]int(nil), in.Dcf.Rates...)
	out.Dcf.Stops = append([]int(nil), in.Dcf.Stops...)
	return &out
}

func cloneFiles(in []FileSnapshot) []FileSnapshot {
	out := make([]FileSnapshot, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].Params = cloneParams(in[i].Params)
		out[i].Voice = cloneVoiceDetail(in[i].Voice)
	}
	return out
}

func cloneInstrument(in *InstrumentSnapshot) *InstrumentSnapshot {
	if in == nil {
		return nil
	}
	out := *in
	out.Banks = make([]BankSnapshot, len(in.Banks))
	for i := range in.Banks {
		out.Banks[i] = in.Banks[i]
		out.Banks[i].Areas = append([]AreaSnapshot(nil), in.Banks[i].Areas...)
	}
	out.Voices = make([]InstrumentVoice, len(in.Voices))
	for i := range in.Voices {
		out.Voices[i] = in.Voices[i]
		out.Voices[i].Params = cloneParams(in.Voices[i].Params)
		out.Voices[i].Voice = cloneVoiceDetail(in.Voices[i].Voice)
	}
	if in.Effects != nil {
		effects := *in.Effects
		effects.Matrix = make([][]int, len(in.Effects.Matrix))
		for i := range in.Effects.Matrix {
			effects.Matrix[i] = append([]int(nil), in.Effects.Matrix[i]...)
		}
		out.Effects = &effects
	}
	return &out
}
