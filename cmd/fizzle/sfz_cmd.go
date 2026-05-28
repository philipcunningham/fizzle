package main

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/sfzconvert"
	"github.com/philipcunningham/fizzle/pkg/sfzexport"
)

func sfzCmd() *cli.Command {
	return &cli.Command{
		Name:        "sfz",
		Usage:       "convert SFZ instruments to FZ series format",
		Description: "SFZ is an open sampler format widely supported by DAWs and sample libraries. 'sfz convert' maps each SFZ region to an FZ voice, preserving key ranges, velocity ranges, and root keys.",
		Commands: []*cli.Command{
			{
				Name:      "convert",
				Usage:     "convert an SFZ instrument or WAV directory into an FZ series full dump (.fzf)",
				ArgsUsage: "SFZ-OR-DIR OUTPUT",
				UsageText: `Convert an SFZ instrument file or a directory of WAV files into a full dump.

When given an .sfz file, each region becomes one voice with its key range,
velocity range, and root key preserved. WAV files are read automatically.

When given a directory, all .wav files in the directory are loaded in
alphabetical order and assigned to sequential keys starting at C2 (MIDI 36).
This is the zero-SFZ workflow for simple drum kits.

Unsupported SFZ features are reported as warnings but do not stop conversion.
The output can then be added to a disk image with 'fizzle disk add'.

   SFZ-OR-DIR  the .sfz instrument file or directory of WAV files to convert
   OUTPUT      the .fzf full dump file to write

   --rate         sample rate: 36000, 18000, or 9000 Hz (default: 36000)
   --fit-to-disk  step down the sample rate automatically if the output would
                  not fit on a single 1.25 MB floppy disk; --rate sets the
                  ceiling (never upsampled, may downsample to 18000 or 9000)
   --split-disks  split across 2 floppy disk images (the FZ series maximum,
                   limited by its 2 MB of sample RAM); OUTPUT becomes the
                   filename prefix and produces OUTPUT-1.img and OUTPUT-2.img
                   ready for a Gotek or floppy emulator.
                   Cannot be used with --fit-to-disk.

Example:
   fizzle sfz convert drums.sfz drums.fzf
   fizzle sfz convert --fit-to-disk jungle.sfz jungle.fzf
   fizzle sfz convert --rate 36000 --split-disks jungle.sfz JUNGLISM
   fizzle sfz convert ./my-samples/ mykit.fzf`,
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:  "rate",
						Value: fzutil.DefaultRate,
						Usage: "sample rate: 36000, 18000, or 9000",
					},
					&cli.BoolFlag{
						Name:  "fit-to-disk",
						Usage: "automatically reduce sample rate if needed to fit on a floppy disk",
					},
					&cli.BoolFlag{
						Name:  "split-disks",
						Usage: "split across 2 floppy disk images; OUTPUT is the filename prefix (produces PREFIX-1.img and PREFIX-2.img)",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 2 {
						return cli.Exit("usage: fizzle sfz convert [--rate N] [--fit-to-disk|--split-disks] SFZ-OR-DIR OUTPUT", exitUsage)
					}
					src := cmd.Args().Get(0)
					out := cmd.Args().Get(1)
					rateVal := cmd.Int("rate")
					if rateVal < 0 {
						return cli.Exit("--rate must be a positive number", exitUsage)
					}
					rate := uint32(rateVal) //nolint:gosec // validated non-negative above
					fit := cmd.Bool("fit-to-disk")
					splitDisks := cmd.Bool("split-disks")

					if splitDisks && fit {
						return cli.Exit("--split-disks and --fit-to-disk cannot be used together", exitUsage)
					}

					info, err := os.Stat(src)
					if err != nil {
						return fmt.Errorf("sfz convert: %w", err)
					}

					if splitDisks {
						if info.IsDir() {
							return cli.Exit("--split-disks is not supported with a WAV directory", exitUsage)
						}
						return sfzconvert.ConvertMultiDisk(ctx, src, out, rate)
					}

					if info.IsDir() {
						return sfzconvert.ConvertDir(ctx, src, out, rate, fit)
					}
					return sfzconvert.Convert(ctx, src, out, rate, fit)
				},
			},
			{
				Name:      "export",
				Usage:     "export a full dump as an SFZ instrument with WAV files",
				ArgsUsage: "FZF OUTPUT_DIR",
				UsageText: `Export all voices from a full dump file as an SFZ instrument.
Each voice is extracted as a WAV file and an SFZ file is generated
mapping them to their original key ranges, velocities, and synthesis
parameters.

   FZF         the .fzf full dump file to export
   OUTPUT_DIR  directory to write the .sfz and .wav files to

   --name NAME  name for the SFZ file (default: derived from FZF filename)

Example:
   fizzle sfz export drums.fzf ./my-instrument/
   fizzle sfz export --name mykit drums.fzf ./output/`,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "name", Usage: "SFZ filename (without extension)"},
				},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 2 {
						return cli.Exit("usage: fizzle sfz export [--name NAME] FZF OUTPUT_DIR", exitUsage)
					}
					fzfPath := cmd.Args().Get(0)
					outputDir := cmd.Args().Get(1)
					name := cmd.String("name")
					return sfzexport.Export(fzfPath, outputDir, name)
				},
			},
		},
	}
}
