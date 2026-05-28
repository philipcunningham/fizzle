package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"github.com/philipcunningham/fizzle/pkg/audioplayer"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
	"github.com/philipcunningham/fizzle/pkg/fzvinfo"
	"github.com/philipcunningham/fizzle/pkg/voiceedit"
	"github.com/philipcunningham/fizzle/pkg/voiceextract"
	"github.com/philipcunningham/fizzle/pkg/voiceimport"
)

func fzvCmd() *cli.Command {
	return &cli.Command{
		Name:        "fzv",
		Usage:       "work with FZ series voice files (.fzv)",
		Description: "A voice file (.fzv) holds a single sample and its playback parameters. Use 'fzv import' to convert a WAV into a voice file, then 'disk add' to put it on a disk image, or 'fzf build' to pack multiple voices together first.",
		Commands: []*cli.Command{
			{
				Name:      subcmdInfo,
				Usage:     "show details about a voice file",
				ArgsUsage: argsUsageFZV,
				UsageText: `Display the parameters stored in a voice file: sample rate, length,
duration, key range, root key, envelope settings, and loop configuration.

   FZV  the .fzv voice file to inspect

   --json  output as JSON instead of formatted text

Example:
   fizzle fzv info kick.fzv
   fizzle fzv info --json kick.fzv`,
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: flagJSON, Usage: flagJSONUsage},
				},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 1 {
						return cli.Exit("usage: fizzle fzv info FZV", exitUsage)
					}
					params, err := fzvinfo.Parse(cmd.Args().Get(0))
					if err != nil {
						return err
					}
					if cmd.Bool(flagJSON) {
						return fzvinfo.RenderJSON(os.Stdout, params)
					}
					fzvinfo.Render(os.Stdout, params)
					return nil
				},
			},
			{
				Name:      "import",
				Usage:     "convert a WAV file into an FZ voice file (.fzv)",
				ArgsUsage: "WAV FZV",
				UsageText: `Convert a 16-bit mono WAV file into an FZ voice file that can be loaded
onto the sampler. The WAV is resampled to the target rate if needed.

The sampler supports three sample rates. Higher rates use more memory but
sound better. 36000 Hz is the highest quality and the default.

   WAV    the WAV file to convert (16, 24, or 32-bit mono PCM)
   FZV    the .fzv voice file to write. Use 'fizzle disk add' to put it on a disk

   --rate  sample rate to encode at: 36000, 18000, or 9000 Hz (default: 36000)

Example:
   fizzle fzv import kick.wav kick.fzv
   fizzle fzv import --rate 18000 kick.wav kick.fzv`,
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:  "rate",
						Value: fzutil.DefaultRate,
						Usage: "target sample rate: 36000, 18000, or 9000",
					},
				},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 2 {
						return cli.Exit("usage: fizzle fzv import [--rate 36000|18000|9000] WAV FZV", exitUsage)
					}
					rate := cmd.Int("rate")
					if rate < 0 {
						return cli.Exit("--rate must be a positive number", exitUsage)
					}
					return voiceimport.Import(
						cmd.Args().Get(0),
						cmd.Args().Get(1),
						uint32(rate), //nolint:gosec // validated non-negative above
					)
				},
			},
			{
				Name:      "extract",
				Usage:     "extract audio from a voice file as a WAV file",
				ArgsUsage: "FZV WAV",
				UsageText: `Extract the PCM audio from an FZ voice file and write it as a standard
16-bit mono WAV file that any audio software can open.

   FZV  the .fzv voice file to read (from the sampler or a disk image)
   WAV  the .wav file to write on your computer

Example:
   fizzle fzv extract kick.fzv kick.wav`,
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 2 {
						return cli.Exit("usage: fizzle fzv extract FZV WAV", exitUsage)
					}
					return voiceextract.Extract(cmd.Args().Get(0), cmd.Args().Get(1))
				},
			},
			{
				Name:      "play",
				Usage:     "play the audio from a voice file through system speakers",
				ArgsUsage: argsUsageFZV,
				UsageText: `Play the audio from an FZ voice file through the system audio device.
Plays only the generator range (genst to gened), matching what the
FZ hardware plays on note-on. Uses native audio on macOS and Windows;
on Linux, requires aplay, paplay, or ffplay.

   FZV  the .fzv voice file to play

Example:
   fizzle fzv play kick.fzv`,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 1 {
						return cli.Exit("usage: fizzle fzv play FZV", exitUsage)
					}
					p := audioplayer.NewPlayer()
					if !p.Available() {
						return cli.Exit("no audio player available (on Linux, install aplay, paplay, or ffplay)", exitError)
					}
					fzvPath := cmd.Args().Get(0)
					tmpDir, err := os.MkdirTemp("", "fizzle-play-*")
					if err != nil {
						return err
					}
					defer os.RemoveAll(tmpDir) //nolint:errcheck
					wavPath := filepath.Join(tmpDir, "play.wav")
					if err := voiceextract.ExtractPlayback(fzvPath, wavPath, audioplayer.LeadInMs); err != nil {
						return err
					}
					return p.PlayWAV(ctx, wavPath)
				},
			},
			{
				Name:      "edit",
				Usage:     "modify voice parameters in an FZV file",
				ArgsUsage: argsUsageFZV,
				UsageText: `Modify parameters of an FZ voice file in place. Only the specified
flags are changed; all other parameters are preserved.

   FZV  the .fzv voice file to modify

Example:
   fizzle fzv edit pad.fzv --lfo-wave sine --lfo-rate 25 --lfo-filter 50
   fizzle fzv edit kick.fzv --cutoff 64 --resonance 7
   fizzle fzv edit pad.fzv --name "MY PAD"
   fizzle fzv edit pad.fzv --dca-sustain 2 --dca-end 3
   fizzle fzv edit pad.fzv --dca-rate-1 99 --dca-stop-1 85`,
				Flags: editFlags(),
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 1 {
						return cli.Exit("usage: fizzle fzv edit FZV [flags]", exitUsage)
					}
					path := cmd.Args().Get(0)
					params, err := fzvinfo.Parse(path)
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
					return voiceedit.ApplyToFZV(path, patches)
				},
			},
		},
	}
}
