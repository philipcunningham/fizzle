package main

import (
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
)

// editFlags returns the shared CLI flags for voice parameter editing,
// used by both `fzv edit` and `fzf edit`.
func editFlags() []cli.Flag {
	base := []cli.Flag{
		&cli.StringFlag{Name: "lfo-wave", Usage: "LFO waveform: sine, saw-up, saw-down, triangle, rectangle, random"},
		&cli.IntFlag{Name: "lfo-rate", Usage: "LFO rate (0-127)"},
		&cli.IntFlag{Name: "lfo-delay", Usage: "LFO delay (0-65535)"},
		&cli.IntFlag{Name: "lfo-attack", Usage: "LFO attack rate (0-127)"},
		&cli.IntFlag{Name: "lfo-pitch", Usage: "LFO pitch depth (0-127)"},
		&cli.IntFlag{Name: "lfo-amp", Usage: "LFO amplitude depth (0-127)"},
		&cli.IntFlag{Name: "lfo-filter", Usage: "LFO filter depth (0-127)"},
		&cli.IntFlag{Name: "lfo-q", Usage: "LFO resonance depth (0-127)"},
		&cli.IntFlag{Name: "cutoff", Usage: "filter cutoff offset (0-127)"},
		&cli.IntFlag{Name: "resonance", Usage: "filter resonance (0-127)"},
		&cli.IntFlag{Name: "dca-level-kf", Usage: "DCA level KF (-15 to +15)"},
		&cli.IntFlag{Name: "dca-rate-kf", Usage: "DCA rate KF (-15 to +15)"},
		&cli.IntFlag{Name: "dcf-level-kf", Usage: "DCF level KF (-15 to +15)"},
		&cli.IntFlag{Name: "dcf-rate-kf", Usage: "DCF rate KF (-15 to +15)"},
		&cli.IntFlag{Name: "vel-dca-kf", Usage: "velocity to amplitude (-127 to +127)"},
		&cli.IntFlag{Name: "vel-dcf-kf", Usage: "velocity to filter (-127 to +127)"},
		&cli.IntFlag{Name: "vel-dcq-kf", Usage: "initial-touch DCQ follow (-127 to +127)"},
		&cli.IntFlag{Name: "vel-dca-rs", Usage: "initial-touch amp rate scale (-127 to +127)"},
		&cli.IntFlag{Name: "vel-dcf-rs", Usage: "initial-touch DCF rate scale (-127 to +127)"},
		&cli.StringFlag{Name: "name", Usage: "voice name (max 12 characters)"},
		&cli.IntFlag{Name: "tune", Usage: "voice tuning in DCP units (1/256 semitone)"},
		&cli.IntFlag{Name: "key-low", Usage: "lowest MIDI note (0-127)"},
		&cli.IntFlag{Name: "key-high", Usage: "highest MIDI note (0-127)"},
		&cli.IntFlag{Name: "root", Usage: "root key MIDI note (0-127)"},
		&cli.StringFlag{Name: "playback-mode", Usage: "playback mode: normal, reverse, cue, synth"},
	}
	dca := envelopeFlags("dca")
	dcf := envelopeFlags("dcf")
	flags := make([]cli.Flag, 0, len(base)+len(dca)+len(dcf))
	flags = append(flags, base...)
	flags = append(flags, dca...)
	flags = append(flags, dcf...)
	return flags
}

func envelopeFlags(prefix string) []cli.Flag {
	flags := []cli.Flag{
		&cli.IntFlag{Name: prefix + "-sustain", Usage: prefix + " envelope sustain point (0-7)"},
		&cli.IntFlag{Name: prefix + "-end", Usage: prefix + " envelope end point (0-7)"},
	}
	for i := 1; i <= disk.EnvelopeStages; i++ {
		flags = append(flags,
			&cli.IntFlag{Name: fmt.Sprintf("%s-rate-%d", prefix, i), Usage: fmt.Sprintf("%s rate for stage %d (0 to 99)", prefix, i)},
			&cli.IntFlag{Name: fmt.Sprintf("%s-stop-%d", prefix, i), Usage: fmt.Sprintf("%s level for stage %d (0 to 99)", prefix, i)},
		)
	}
	return flags
}

func intIfSet(cmd *cli.Command, name string) int {
	if cmd.IsSet(name) {
		return cmd.Int(name)
	}
	return voiceedit.Unchanged
}

func collectPatches(cmd *cli.Command, params *fzvinfo.VoiceParams) ([]voiceedit.Patch, error) {
	var all []voiceedit.Patch

	lfo, err := collectLFOPatches(cmd, params)
	if err != nil {
		return nil, err
	}
	all = append(all, lfo...)

	filter, err := collectFilterPatches(cmd)
	if err != nil {
		return nil, err
	}
	all = append(all, filter...)

	mod, err := collectModulationPatches(cmd)
	if err != nil {
		return nil, err
	}
	all = append(all, mod...)

	dca, err := collectEnvelopePatches(cmd, "dca", params.DCARates, voiceedit.BuildDCAPatches)
	if err != nil {
		return nil, err
	}
	all = append(all, dca...)

	dcf, err := collectEnvelopePatches(cmd, "dcf", params.DCFRates, voiceedit.BuildDCFPatches)
	if err != nil {
		return nil, err
	}
	all = append(all, dcf...)

	meta, err := collectMetaPatches(cmd)
	if err != nil {
		return nil, err
	}
	all = append(all, meta...)

	return all, nil
}

func collectLFOPatches(cmd *cli.Command, params *fzvinfo.VoiceParams) ([]voiceedit.Patch, error) {
	wave := voiceedit.Unchanged
	if cmd.IsSet("lfo-wave") {
		name := cmd.String("lfo-wave")
		idx, ok := voiceedit.WaveformIndex(name)
		if !ok {
			return nil, cli.Exit(fmt.Sprintf("unknown waveform %q (use: sine, saw-up, saw-down, triangle, rectangle, random)", name), exitUsage)
		}
		wave = idx
	}

	// Reconstruct the raw lfo_name byte so BuildLFOPatches can preserve the
	// phase-sync flag (bit 7) when only the waveform index changes. Only
	// bit 7 matters for preservation; the waveform-index bits are overwritten.
	var origLFOName uint8
	if params.LFOPhaseSync {
		origLFOName |= disk.LFOPhaseFlag
	}

	return voiceedit.BuildLFOPatches(
		wave,
		intIfSet(cmd, "lfo-rate"),
		intIfSet(cmd, "lfo-delay"),
		intIfSet(cmd, "lfo-attack"),
		intIfSet(cmd, "lfo-pitch"),
		intIfSet(cmd, "lfo-amp"),
		intIfSet(cmd, "lfo-filter"),
		intIfSet(cmd, "lfo-q"),
		origLFOName,
	)
}

func collectFilterPatches(cmd *cli.Command) ([]voiceedit.Patch, error) {
	return voiceedit.BuildFilterPatches(
		intIfSet(cmd, "cutoff"),
		intIfSet(cmd, "resonance"),
	)
}

func collectModulationPatches(cmd *cli.Command) ([]voiceedit.Patch, error) {
	return voiceedit.BuildModulationPatches(
		intIfSet(cmd, "dca-level-kf"),
		intIfSet(cmd, "dca-rate-kf"),
		intIfSet(cmd, "dcf-level-kf"),
		intIfSet(cmd, "dcf-rate-kf"),
		intIfSet(cmd, "vel-dca-kf"),
		intIfSet(cmd, "vel-dcf-kf"),
		intIfSet(cmd, "vel-dcq-kf"),
		intIfSet(cmd, "vel-dca-rs"),
		intIfSet(cmd, "vel-dcf-rs"),
	)
}

func collectMetaPatches(cmd *cli.Command) ([]voiceedit.Patch, error) {
	var all []voiceedit.Patch

	if cmd.IsSet("name") {
		namePatches, err := voiceedit.BuildNamePatch(cmd.String("name"))
		if err != nil {
			return nil, err
		}
		all = append(all, namePatches...)
	}

	if cmd.IsSet("tune") {
		tunePatches, err := voiceedit.BuildTunePatch(cmd.Int("tune"))
		if err != nil {
			return nil, err
		}
		all = append(all, tunePatches...)
	}

	keyRangePatches, err := voiceedit.BuildKeyRangePatch(
		intIfSet(cmd, "key-low"),
		intIfSet(cmd, "key-high"),
		intIfSet(cmd, "root"),
	)
	if err != nil {
		return nil, err
	}
	all = append(all, keyRangePatches...)

	if cmd.IsSet("playback-mode") {
		pmPatches, err := voiceedit.BuildPlaybackModePatch(cmd.String("playback-mode"))
		if err != nil {
			return nil, err
		}
		all = append(all, pmPatches...)
	}

	return all, nil
}

type envelopeBuildFunc func(sustain, end int, rates, stops [disk.EnvelopeStages]int, origRates [disk.EnvelopeStages]uint8) ([]voiceedit.Patch, error)

func collectEnvelopePatches(cmd *cli.Command, prefix string, origRates [disk.EnvelopeStages]uint8, buildFn envelopeBuildFunc) ([]voiceedit.Patch, error) {
	sustain := intIfSet(cmd, prefix+"-sustain")
	end := intIfSet(cmd, prefix+"-end")

	var rates, stops [disk.EnvelopeStages]int
	for i := range disk.EnvelopeStages {
		rates[i] = intIfSet(cmd, fmt.Sprintf("%s-rate-%d", prefix, i+1))
		stops[i] = intIfSet(cmd, fmt.Sprintf("%s-stop-%d", prefix, i+1))
	}

	return buildFn(sustain, end, rates, stops, origRates)
}
