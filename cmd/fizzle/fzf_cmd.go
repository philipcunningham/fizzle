package main

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/philipcunningham/fizzle/pkg/fzfeffects"
	"github.com/philipcunningham/fizzle/pkg/fzfinfo"
	"github.com/philipcunningham/fizzle/pkg/fzfmidi"
	"github.com/philipcunningham/fizzle/pkg/fzfoutput"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/voicebuild"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
	"github.com/philipcunningham/fizzle/pkg/voiceunpack"
)

func fzfCmd() *cli.Command {
	return &cli.Command{
		Name:        "fzf",
		Usage:       "work with FZ series full dump files (.fzf)",
		Description: "A full dump (.fzf) packs multiple voices into one file that loads onto the sampler in a single operation. Use 'fzf build' to create one from voice files, then 'disk add' to put it on a disk image.",
		Commands: []*cli.Command{
			{
				Name:      subcmdInfo,
				Usage:     "show the voice map of a full dump file",
				ArgsUsage: argsUsageFZF,
				UsageText: `Display all voices in a full dump file as a table showing each voice's
name, key range, sample rate, and duration. Root key and velocity columns
appear only when they carry useful information. Voices with sustain loops
are marked in the duration column.

This gives you a complete map of the instrument.

   FZF  the .fzf full dump file to inspect

   --json  output as JSON instead of a table

Example:
   fizzle fzf info drums.fzf
   fizzle fzf info --json drums.fzf`,
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: flagJSON, Usage: flagJSONUsage},
				},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 1 {
						return cli.Exit("usage: fizzle fzf info FZF", exitUsage)
					}
					info, err := fzfinfo.Parse(cmd.Args().Get(0))
					if err != nil {
						return err
					}
					if cmd.Bool(flagJSON) {
						return fzfinfo.RenderJSON(os.Stdout, info)
					}
					fzfinfo.Render(os.Stdout, info, nil)
					return nil
				},
			},
			{
				Name:      "build",
				Usage:     "pack individual voice files (.fzv) into a full dump (.fzf)",
				ArgsUsage: "OUTPUT VOICE [VOICE...]",
				UsageText: `Pack one or more voice files into a single full dump file (.fzf).

A full dump loads all voices in one operation on the sampler, which is much
more convenient than loading voices one at a time. Once built, use
'fizzle disk add' to copy the dump onto a disk image.

   OUTPUT  the .fzf full dump file to create (up to 64 voices)
   VOICE   one or more .fzv voice files to pack in

Example:
   fizzle fzf build drums.fzf kick.fzv snare.fzv hihat.fzv`,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() < 2 {
						return cli.Exit("usage: fizzle fzf build OUTPUT VOICE [VOICE...]", exitUsage)
					}
					args := cmd.Args().Slice()
					return voicebuild.Build(ctx, args[0], args[1:])
				},
			},
			{
				Name:      "unpack",
				Usage:     "extract individual voice files (.fzv) from a full dump (.fzf)",
				ArgsUsage: "FZF OUTPUTDIR",
				UsageText: `Extract all voices from a full dump file into individual .fzv files,
one file per voice, each named after the voice as stored in the dump.

   FZF        the .fzf full dump file to unpack (or a disk 1 .img when using --disk2)
   OUTPUTDIR  the directory to write the .fzv files into (created if it does not exist)

   --disk2    path to the disk 2 .img file for a multi-disk full dump; the first
              argument is treated as the disk 1 .img path and voices are extracted
              with audio from both disks

Example:
   fizzle fzf unpack drums.fzf ./voices/
   fizzle fzf unpack JUNGLISM-1.img --disk2 JUNGLISM-2.img ./voices/`,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "disk2",
						Usage: "disk 2 image path for multi-disk unpack",
					},
					&cli.IntFlag{
						Name:  "bank",
						Usage: "extract only voices from the given bank (1-based; 0 means all banks)",
					},
				},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 2 {
						return cli.Exit("usage: fizzle fzf unpack [--disk2 DISK2.img] [--bank N] FZF-OR-IMG OUTPUTDIR", exitUsage)
					}
					disk2 := cmd.String("disk2")
					bank := cmd.Int("bank")
					if disk2 != "" {
						return voiceunpack.UnpackMultiDisk(cmd.Args().Get(0), disk2, cmd.Args().Get(1))
					}
					if bank > 0 {
						return voiceunpack.UnpackBank(cmd.Args().Get(0), cmd.Args().Get(1), bank-1)
					}
					return voiceunpack.Unpack(cmd.Args().Get(0), cmd.Args().Get(1))
				},
			},
			{
				Name:      "midi",
				Usage:     "set the MIDI receive channel for one or more voices",
				ArgsUsage: argsUsageFZF,
				UsageText: `Set the MIDI receive channel for voices in a full dump file.

The FZ-1 responds to note-on/off events on each voice's assigned channel,
allowing independent pitch bend and expression per voice group. For example,
assign your bass voice to channel 2 and send pitch bend only on channel 2
to bend the bass without affecting the drums.

Use 'fzf info' to see voice names before running this command.

   FZF           the .fzf full dump file to modify (modified in place)

   --voice NAME  voice name to target, exactly as shown in 'fzf info'
                 (case-insensitive, repeatable for multiple voices)
   --all         target all voices (use with --channel 1 to reset)
   --channel N   MIDI receive channel to assign (1-16, required)

Example:
   fizzle fzf midi drums.fzf --voice "REESE" --channel 2
   fizzle fzf midi drums.fzf --voice "808" --voice "REESE" --channel 2
   fizzle fzf midi drums.fzf --all --channel 1`,
				Flags: []cli.Flag{
					&cli.StringSliceFlag{
						Name:  flagVoice,
						Usage: "voice name to target (exact, case-insensitive, repeatable)",
					},
					&cli.BoolFlag{
						Name:  "all",
						Usage: "target all voices",
					},
					&cli.IntFlag{
						Name:     "channel",
						Usage:    "MIDI receive channel (1-16)",
						Required: true,
					},
				},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 1 {
						return cli.Exit("usage: fizzle fzf midi [--voice NAME ...] [--all] --channel N FZF", exitUsage)
					}
					fzfPath := cmd.Args().Get(0)
					voices := cmd.StringSlice(flagVoice)
					all := cmd.Bool("all")
					ch := cmd.Int("channel")

					if ch < 1 || ch > 16 {
						return cli.Exit(fmt.Sprintf("--channel must be between 1 and 16, got %d", ch), exitUsage)
					}

					res, err := fzfmidi.Set(fzfPath, voices, all, uint8(ch)) //nolint:gosec // validated 1..16 above
					if err != nil {
						return err
					}

					fzfmidi.Render(os.Stdout, res, uint8(ch)) //nolint:gosec // validated 1..16 above
					if len(res.Updated) == 0 {
						return nil
					}
					return fzfinfo.Info(fzfPath, os.Stdout, nil)
				},
			},
			{
				Name:      "output",
				Usage:     "set the output (generator channel) for one or more voices",
				ArgsUsage: argsUsageFZF,
				UsageText: `Set the output assignment for voices in a full dump file.

The FZ-1 has 8 voice generators, each feeding an individual output jack
(1-8) on the back panel. Assigning a voice to a single output makes it
monophonic on that output: a new note cuts the previous one. Voices
sharing the same output mute each other. Assigning multiple outputs
gives limited polyphony across those outputs. 'all' enables all 8
outputs.

Use 'fzf info' to see voice names and current output assignments.

   FZF            the .fzf full dump file to modify (modified in place)

   --voice NAME   voice name to target, exactly as shown in 'fzf info'
                  (case-insensitive, repeatable for multiple voices)
   --all          target all voices
   --output VAL   output assignment: 1-8 (single), 1,3,5 (multiple),
                  or 'all' (all 8 outputs). Required.

Example:
   fizzle fzf output drums.fzf --voice "REESE" --output 2
   fizzle fzf output drums.fzf --voice "PAD" --output 1,3,5
   fizzle fzf output drums.fzf --all --output all`,
				Flags: []cli.Flag{
					&cli.StringSliceFlag{
						Name:  flagVoice,
						Usage: "voice name to target (exact, case-insensitive, repeatable)",
					},
					&cli.BoolFlag{
						Name:  "all",
						Usage: "target all voices",
					},
					&cli.StringFlag{
						Name:     "output",
						Usage:    "output assignment: 1-8, comma-separated, or 'all'",
						Required: true,
					},
				},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 1 {
						return cli.Exit("usage: fizzle fzf output [--voice NAME ...] [--all] --output VAL FZF", exitUsage)
					}
					fzfPath := cmd.Args().Get(0)
					voices := cmd.StringSlice(flagVoice)
					all := cmd.Bool("all")
					outputStr := cmd.String("output")

					gchn, err := fzfoutput.ParseOutputFlag(outputStr)
					if err != nil {
						return cli.Exit(err.Error(), exitUsage)
					}

					res, err := fzfoutput.Set(fzfPath, voices, all, gchn)
					if err != nil {
						return err
					}

					fzfoutput.Render(os.Stdout, res, outputStr)
					if len(res.Updated) == 0 {
						return nil
					}
					return fzfinfo.Info(fzfPath, os.Stdout, nil)
				},
			},
			{
				Name:      "effects",
				Usage:     "view or set the global effect parameters (bend, mod, foot, aftertouch)",
				ArgsUsage: argsUsageFZF,
				UsageText: `View or modify the global effect block in a full dump file.

The effect block controls how performance controllers are routed to the
synthesis engine. Three controllers (mod wheel, foot pedal, aftertouch)
each route to seven targets: LFO pitch/amp/filter/resonance and amp/
filter/resonance offset. Plus the global pitch bend range.

The --bend flag is in 1/8-semitone units, so 24 = 3 semitones and
48 = 6 semitones.

With no flags, the current effect parameters are displayed.

   FZF  the .fzf full dump file to inspect or modify (modified in place)

Example:
   fizzle fzf effects drums.fzf --bend 48
   fizzle fzf effects drums.fzf --mod-lfa 30 --aftertouch-dcf 20`,
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "bend", Usage: "pitch bend range in 1/8-semitone units (0-127)"},
					&cli.IntFlag{Name: "mod-lfp", Usage: "mod wheel to LFO pitch depth (0-127)"},
					&cli.IntFlag{Name: "mod-lfa", Usage: "mod wheel to LFO amp depth (0-127)"},
					&cli.IntFlag{Name: "mod-lff", Usage: "mod wheel to LFO filter depth (0-127)"},
					&cli.IntFlag{Name: "mod-lfq", Usage: "mod wheel to LFO resonance depth (0-127)"},
					&cli.IntFlag{Name: "mod-dca", Usage: "mod wheel to amp offset (0-127)"},
					&cli.IntFlag{Name: "mod-dcf", Usage: "mod wheel to filter offset (0-127)"},
					&cli.IntFlag{Name: "mod-dcq", Usage: "mod wheel to resonance offset (0-127)"},
					&cli.IntFlag{Name: "foot-lfp", Usage: "foot pedal to LFO pitch depth (0-127)"},
					&cli.IntFlag{Name: "foot-lfa", Usage: "foot pedal to LFO amp depth (0-127)"},
					&cli.IntFlag{Name: "foot-lff", Usage: "foot pedal to LFO filter depth (0-127)"},
					&cli.IntFlag{Name: "foot-lfq", Usage: "foot pedal to LFO resonance depth (0-127)"},
					&cli.IntFlag{Name: "foot-dca", Usage: "foot pedal to amp offset (volume) (0-127)"},
					&cli.IntFlag{Name: "foot-dcf", Usage: "foot pedal to filter offset (0-127)"},
					&cli.IntFlag{Name: "foot-dcq", Usage: "foot pedal to resonance offset (0-127)"},
					&cli.IntFlag{Name: "aftertouch-lfp", Usage: "aftertouch to LFO pitch depth (0-127)"},
					&cli.IntFlag{Name: "aftertouch-lfa", Usage: "aftertouch to LFO amp depth (0-127)"},
					&cli.IntFlag{Name: "aftertouch-lff", Usage: "aftertouch to LFO filter depth (0-127)"},
					&cli.IntFlag{Name: "aftertouch-lfq", Usage: "aftertouch to LFO resonance depth (0-127)"},
					&cli.IntFlag{Name: "aftertouch-dca", Usage: "aftertouch to amp offset (0-127)"},
					&cli.IntFlag{Name: "aftertouch-dcf", Usage: "aftertouch to filter offset (0-127)"},
					&cli.IntFlag{Name: "aftertouch-dcq", Usage: "aftertouch to resonance offset (0-127)"},
				},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 1 {
						return cli.Exit("usage: fizzle fzf effects FZF [flags]", exitUsage)
					}
					fzfPath := cmd.Args().Get(0)

					// effectFlagBinding pairs a CLI flag name with the
					// SetParams field it writes to. Used both to detect
					// "any flag set" and to populate the SetParams.
					type effectFlagBinding struct {
						flag  string
						field *int
					}
					p := fzfeffects.Unchanged()
					bindings := []effectFlagBinding{
						{"bend", &p.BendRange},
						{"mod-lfp", &p.ModLFP},
						{"mod-lfa", &p.ModLFA},
						{"mod-lff", &p.ModLFF},
						{"mod-lfq", &p.ModLFQ},
						{"mod-dca", &p.ModDCA},
						{"mod-dcf", &p.ModDCF},
						{"mod-dcq", &p.ModDCQ},
						{"foot-lfp", &p.FotLFP},
						{"foot-lfa", &p.FotLFA},
						{"foot-lff", &p.FotLFF},
						{"foot-lfq", &p.FotLFQ},
						{"foot-dca", &p.FotDCA},
						{"foot-dcf", &p.FotDCF},
						{"foot-dcq", &p.FotDCQ},
						{"aftertouch-lfp", &p.AftLFP},
						{"aftertouch-lfa", &p.AftLFA},
						{"aftertouch-lff", &p.AftLFF},
						{"aftertouch-lfq", &p.AftLFQ},
						{"aftertouch-dca", &p.AftDCA},
						{"aftertouch-dcf", &p.AftDCF},
						{"aftertouch-dcq", &p.AftDCQ},
					}

					anySet := false
					for _, b := range bindings {
						if cmd.IsSet(b.flag) {
							anySet = true
							*b.field = cmd.Int(b.flag)
						}
					}

					if !anySet {
						params, err := fzfeffects.Parse(fzfPath)
						if err != nil {
							return err
						}
						fzfeffects.Render(os.Stdout, params)
						return nil
					}

					res, err := fzfeffects.Set(fzfPath, p)
					if err != nil {
						return err
					}
					fzfeffects.RenderResult(os.Stdout, res)
					return nil
				},
			},
			{
				Name:      "edit",
				Usage:     "modify voice parameters in a full dump file",
				ArgsUsage: argsUsageFZF,
				UsageText: `Modify parameters of a voice inside an FZF full dump file in place.
The voice is identified by --voice (case-insensitive, as shown in 'fzf info').
Only the specified flags are changed; all other parameters are preserved.

   FZF  the .fzf full dump file to modify

Example:
   fizzle fzf edit drums.fzf --voice "PAD" --lfo-wave sine --lfo-rate 25
   fizzle fzf edit drums.fzf --voice "KICK" --cutoff 64 --resonance 7`,
				Flags: append(editFlags(), &cli.StringFlag{
					Name:  flagVoice,
					Usage: "voice name to target (exact, case-insensitive)",
				}),
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 1 {
						return cli.Exit("usage: fizzle fzf edit FZF --voice NAME [flags]", exitUsage)
					}
					voice := cmd.String(flagVoice)
					if voice == "" {
						return cli.Exit("--voice is required", exitUsage)
					}
					path := cmd.Args().Get(0)
					params, err := fzvinfo.ParseFZFVoice(path, voice)
					if err != nil {
						return err
					}
					patches, err := collectPatches(cmd, params)
					if err != nil {
						return err
					}
					if len(patches) == 0 {
						return cli.Exit("no edit flags specified", exitUsage)
					}
					return voiceedit.ApplyToFZFVoice(path, voice, patches)
				},
			},
		},
	}
}
