package webcore

import (
	"strconv"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
)

// SchemaField declares one editable voice parameter: the UI renders a
// control from this declaration and nothing else (R14). Kind is one of
// "knob", "stepper", "note" (a stepper displayed as a note name), or
// "select". Numeric kinds carry Min and Max; select carries Options.
type SchemaField struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Group   string   `json:"group"`
	Kind    string   `json:"kind"`
	Min     int      `json:"min"`
	Max     int      `json:"max"`
	Options []string `json:"options,omitempty"`
}

// Group and kind names used by the schema.
const (
	groupSample   = "Sample"
	groupIdentity = "Identity and mapping"
	groupFilter   = "Filter"
	groupKF       = "Key follow"
	groupVelocity = "Velocity"
	groupLFO      = "LFO"

	kindKnob    = "knob"
	kindStepper = "stepper"
	kindNote    = "note"
	kindSelect  = "select"
)

// Field IDs, shared by the schema table, the patch builders, and the
// snapshot readout.
const (
	fieldPlaybackMode = "playbackMode"
	fieldSampleRate   = "sampleRate"
	fieldTune         = "tune"
	fieldRootKey      = "rootKey"
	fieldKeyLow       = "keyLow"
	fieldKeyHigh      = "keyHigh"
	fieldCutoff       = "cutoff"
	fieldResonance    = "resonance"
	fieldDcaLevelKF   = "dcaLevelKF"
	fieldDcaRateKF    = "dcaRateKF"
	fieldDcfLevelKF   = "dcfLevelKF"
	fieldDcfRateKF    = "dcfRateKF"
	fieldVelDcaKF     = "velDcaKF"
	fieldVelDcfKF     = "velDcfKF"
	fieldVelDcqKF     = "velDcqKF"
	fieldVelDcaRS     = "velDcaRS"
	fieldVelDcfRS     = "velDcfRS"
	fieldLfoWave      = "lfoWave"
	fieldLfoRate      = "lfoRate"
	fieldLfoDelay     = "lfoDelay"
	fieldLfoAttack    = "lfoAttack"
	fieldLfoPitch     = "lfoPitch"
	fieldLfoAmp       = "lfoAmp"
	fieldLfoFilter    = "lfoFilter"
	fieldLfoQ         = "lfoQ"
)

// lfoWaveNames maps the stored waveform index to the identifier the
// CLI accepts (voiceedit.WaveformIndex inverts it).
var lfoWaveNames = [...]string{
	disk.LFOSine:      "sine",
	disk.LFOSawUp:     "saw-up",
	disk.LFOSawDown:   "saw-down",
	disk.LFOTriangle:  "triangle",
	disk.LFORectangle: "rectangle",
	disk.LFORandom:    "random",
}

// sampleRateOptions lists the three rates the FZ hardware has, as the
// identifiers the setter accepts. disk.SampleRates is the canonical
// list, so a rate the hardware gains here would arrive with it.
var sampleRateOptions = func() []string {
	out := make([]string, 0, len(disk.SampleRates))
	for _, rate := range disk.SampleRates {
		out = append(out, strconv.FormatUint(uint64(rate), 10))
	}
	return out
}()

// voiceSchema is the flat scalar surface of a voice: the parameters
// the CLI's fzv edit exposes, minus name, envelopes, loops, and the
// generation window, which get bespoke treatment because their bounds
// are the voice's own frame count rather than anything a static
// declaration can carry. Ranges mirror the voiceedit builders; the
// builders stay the enforcement of record.
var voiceSchema = []SchemaField{
	{ID: fieldPlaybackMode, Label: "Playback", Group: groupSample, Kind: kindSelect,
		Options: []string{"normal", "reverse", "cue", "synth"}},
	{ID: fieldSampleRate, Label: "Sample rate (Hz)", Group: groupSample, Kind: kindSelect,
		Options: sampleRateOptions},

	{ID: fieldTune, Label: "Tune (1/256 semi)", Group: groupIdentity, Kind: kindStepper, Min: -32768, Max: 32767},
	{ID: fieldRootKey, Label: "Root", Group: groupIdentity, Kind: kindNote, Min: 0, Max: 127},
	{ID: fieldKeyLow, Label: "Key low", Group: groupIdentity, Kind: kindNote, Min: 0, Max: 127},
	{ID: fieldKeyHigh, Label: "Key high", Group: groupIdentity, Kind: kindNote, Min: 0, Max: 127},

	{ID: fieldCutoff, Label: "Cutoff", Group: groupFilter, Kind: kindKnob, Min: 0, Max: 127},
	{ID: fieldResonance, Label: "Resonance", Group: groupFilter, Kind: kindKnob, Min: 0, Max: disk.MaxResonance},

	{ID: fieldDcaLevelKF, Label: "DCA level", Group: groupKF, Kind: kindStepper, Min: -15, Max: 15},
	{ID: fieldDcaRateKF, Label: "DCA rate", Group: groupKF, Kind: kindStepper, Min: -15, Max: 15},
	{ID: fieldDcfLevelKF, Label: "DCF level", Group: groupKF, Kind: kindStepper, Min: -15, Max: 15},
	{ID: fieldDcfRateKF, Label: "DCF rate", Group: groupKF, Kind: kindStepper, Min: -15, Max: 15},

	{ID: fieldVelDcaKF, Label: "To amplitude", Group: groupVelocity, Kind: kindStepper, Min: -127, Max: 127},
	{ID: fieldVelDcfKF, Label: "To filter", Group: groupVelocity, Kind: kindStepper, Min: -127, Max: 127},
	{ID: fieldVelDcqKF, Label: "To resonance", Group: groupVelocity, Kind: kindStepper, Min: -127, Max: 127},
	{ID: fieldVelDcaRS, Label: "Amp rate scale", Group: groupVelocity, Kind: kindStepper, Min: -127, Max: 127},
	{ID: fieldVelDcfRS, Label: "Filter rate scale", Group: groupVelocity, Kind: kindStepper, Min: -127, Max: 127},

	{ID: fieldLfoRate, Label: "Rate", Group: groupLFO, Kind: kindKnob, Min: 0, Max: 127},
	{ID: fieldLfoDelay, Label: "Delay", Group: groupLFO, Kind: kindStepper, Min: 0, Max: 65535},
	{ID: fieldLfoAttack, Label: "Attack", Group: groupLFO, Kind: kindKnob, Min: 0, Max: 127},
	{ID: fieldLfoPitch, Label: "Pitch depth", Group: groupLFO, Kind: kindKnob, Min: 0, Max: 127},
	{ID: fieldLfoAmp, Label: "Amp depth", Group: groupLFO, Kind: kindKnob, Min: 0, Max: 127},
	{ID: fieldLfoFilter, Label: "Filter depth", Group: groupLFO, Kind: kindKnob, Min: 0, Max: 127},
	{ID: fieldLfoQ, Label: "Resonance depth", Group: groupLFO, Kind: kindKnob, Min: 0, Max: 127},
	{ID: fieldLfoWave, Label: "Waveform", Group: groupLFO, Kind: kindSelect,
		Options: append([]string{}, lfoWaveNames[:]...)},
}

// Schema returns the editable parameter schema the UI renders from.
func Schema() []SchemaField {
	out := make([]SchemaField, len(voiceSchema))
	copy(out, voiceSchema)
	return out
}

func schemaField(id string) (SchemaField, bool) {
	for _, f := range voiceSchema {
		if f.ID == id {
			return f, true
		}
	}
	return SchemaField{}, false
}

// numberPatches builds the voiceedit patches for a clamped numeric
// field value. voiceBytes provides current values where a builder
// needs them.
func numberPatches(id string, n int, voiceBytes []byte) ([]voiceedit.Patch, error) {
	const u = voiceedit.Unchanged
	switch id {
	case fieldTune:
		return voiceedit.BuildTunePatch(n)
	case fieldRootKey:
		return voiceedit.BuildKeyRangePatch(u, u, n)
	case fieldKeyLow:
		return voiceedit.BuildKeyRangePatch(n, u, u)
	case fieldKeyHigh:
		return voiceedit.BuildKeyRangePatch(u, n, u)
	case fieldCutoff:
		return voiceedit.BuildFilterPatches(n, u)
	case fieldResonance:
		return voiceedit.BuildFilterPatches(u, n)
	case fieldDcaLevelKF:
		return voiceedit.BuildModulationPatches(n, u, u, u, u, u, u, u, u)
	case fieldDcaRateKF:
		return voiceedit.BuildModulationPatches(u, n, u, u, u, u, u, u, u)
	case fieldDcfLevelKF:
		return voiceedit.BuildModulationPatches(u, u, n, u, u, u, u, u, u)
	case fieldDcfRateKF:
		return voiceedit.BuildModulationPatches(u, u, u, n, u, u, u, u, u)
	case fieldVelDcaKF:
		return voiceedit.BuildModulationPatches(u, u, u, u, n, u, u, u, u)
	case fieldVelDcfKF:
		return voiceedit.BuildModulationPatches(u, u, u, u, u, n, u, u, u)
	case fieldVelDcqKF:
		return voiceedit.BuildModulationPatches(u, u, u, u, u, u, n, u, u)
	case fieldVelDcaRS:
		return voiceedit.BuildModulationPatches(u, u, u, u, u, u, u, n, u)
	case fieldVelDcfRS:
		return voiceedit.BuildModulationPatches(u, u, u, u, u, u, u, u, n)
	case fieldLfoRate:
		return voiceedit.BuildLFOPatches(u, n, u, u, u, u, u, u, lfoNameByte(voiceBytes))
	case fieldLfoDelay:
		return voiceedit.BuildLFOPatches(u, u, n, u, u, u, u, u, lfoNameByte(voiceBytes))
	case fieldLfoAttack:
		return voiceedit.BuildLFOPatches(u, u, u, n, u, u, u, u, lfoNameByte(voiceBytes))
	case fieldLfoPitch:
		return voiceedit.BuildLFOPatches(u, u, u, u, n, u, u, u, lfoNameByte(voiceBytes))
	case fieldLfoAmp:
		return voiceedit.BuildLFOPatches(u, u, u, u, u, n, u, u, lfoNameByte(voiceBytes))
	case fieldLfoFilter:
		return voiceedit.BuildLFOPatches(u, u, u, u, u, u, n, u, lfoNameByte(voiceBytes))
	case fieldLfoQ:
		return voiceedit.BuildLFOPatches(u, u, u, u, u, u, u, n, lfoNameByte(voiceBytes))
	default:
		return nil, errf("invalid-field", "%q is not a numeric field", id)
	}
}

// optionPatches builds patches for a select field. voiceBytes provides
// the current lfo_name byte so its phase-sync flag survives.
func optionPatches(id, option string, voiceBytes []byte) ([]voiceedit.Patch, error) {
	const u = voiceedit.Unchanged
	switch id {
	case fieldPlaybackMode:
		return voiceedit.BuildPlaybackModePatch(option)
	case fieldSampleRate:
		return sampleRatePatches(option)
	case fieldLfoWave:
		idx, ok := voiceedit.WaveformIndex(option)
		if !ok {
			return nil, errf(codeInvalidValue, "unknown LFO waveform %q", option)
		}
		return voiceedit.BuildLFOPatches(idx, u, u, u, u, u, u, u, lfoNameByte(voiceBytes))
	default:
		return nil, errf("invalid-field", "%q is not a select field", id)
	}
}

// sampleRatePatches writes the rate index byte for one of the three
// rates the hardware has. The byte is an index into disk.SampleRates
// and nothing else in the header moves with it: the samples keep their
// bytes and their count, and only the speed they are read back at
// changes. voiceedit has no builder of its own here (the CLI's fzv edit
// does not expose the rate), so the one byte patch is written directly
// against disk's own offset, the way the studio TUI's Sample row does.
// A rate the hardware has no index for is refused rather than rounded.
func sampleRatePatches(option string) ([]voiceedit.Patch, error) {
	hz, err := strconv.ParseUint(option, 10, 32)
	if err != nil {
		return nil, errf(codeInvalidValue, "sample rate %q is not a number of hertz", option)
	}
	idx, ok := disk.RateIndexFor(uint32(hz))
	if !ok {
		return nil, errf(codeInvalidValue, "%v", disk.ValidateRate(uint32(hz)))
	}
	return []voiceedit.Patch{{Offset: disk.VoiceSampOffset, Size: 1, Value: uint16(idx)}}, nil
}

func lfoNameByte(voiceBytes []byte) uint8 {
	if len(voiceBytes) > disk.VoiceLFONameOffset {
		return voiceBytes[disk.VoiceLFONameOffset]
	}
	return 0
}
