package main

import (
	"context"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/diskadd"
	"github.com/philipcunningham/fizzle/pkg/diskcopy"
	"github.com/philipcunningham/fizzle/pkg/diskformat"
	"github.com/philipcunningham/fizzle/pkg/diskget"
	"github.com/philipcunningham/fizzle/pkg/disklist"
)

func diskCmd() *cli.Command {
	return &cli.Command{
		Name:        "disk",
		Usage:       "manage FZ series floppy disk images",
		Description: "A disk image is a .img file on your computer that represents a floppy disk. Create one with 'disk new', populate it with 'disk add', then copy it to a USB stick or floppy emulator to use on the sampler.",
		Commands: []*cli.Command{
			{
				Name:      "new",
				Usage:     "create a blank formatted disk image",
				ArgsUsage: "LABEL IMAGE",
				UsageText: `Create a blank 1.25 MB FZ series disk image file.

   LABEL  the name displayed on the sampler when this disk is loaded,
          up to 12 characters (e.g. "My Drums")
   IMAGE  the .img file to create on your computer. Copy this to a USB
          stick or floppy emulator to use with the sampler

Example:
   fizzle disk new "My Drums" mydrums.img`,
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 2 {
						return cli.Exit("usage: fizzle disk new LABEL IMAGE", exitUsage)
					}
					label := cmd.Args().Get(0)
					if strings.Contains(label, string(os.PathSeparator)) || strings.HasSuffix(strings.ToLower(label), ".img") {
						return cli.Exit("it looks like the arguments may be swapped. Usage: fizzle disk new LABEL IMAGE", exitUsage)
					}
					imagePath := cmd.Args().Get(1)
					if info, err := os.Stat(imagePath); err == nil && !info.IsDir() {
						log.Warn().Str("file", imagePath).Msg("overwriting existing disk image")
					}
					return diskformat.Format(imagePath, label)
				},
			},
			{
				Name:      "ls",
				Usage:     "list the contents of a disk image",
				ArgsUsage: "IMAGE",
				UsageText: `List the disk label and all files stored in a disk image file.

   IMAGE  the .img disk image file to inspect

   --json  output as JSON instead of a table

Example:
   fizzle disk ls mydrums.img
   fizzle disk ls --json mydrums.img`,
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: flagJSON, Usage: flagJSONUsage},
				},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 1 {
						return cli.Exit("usage: fizzle disk ls IMAGE", exitUsage)
					}
					listing, err := disklist.Parse(cmd.Args().Get(0))
					if err != nil {
						return err
					}
					if cmd.Bool(flagJSON) {
						return disklist.RenderJSON(os.Stdout, listing)
					}
					disklist.Render(os.Stdout, listing)
					return nil
				},
			},
			{
				Name:      "add",
				Usage:     "add a voice (.fzv) or full dump (.fzf) file to a disk image",
				ArgsUsage: "IMAGE FILE",
				UsageText: `Copy a voice or full dump file onto a disk image so the sampler can load it.
The file type is detected automatically from the file contents.

   IMAGE       the .img disk image file to add the file to
   FILE        the .fzv voice file or .fzf full dump file to copy onto the disk

   --disk-num  which disk this is in a 2-disk split: 1 for the first disk
               (default), 2 for the second disk

Example:
   fizzle disk add mydrums.img kick.fzv
   fizzle disk add --disk-num 2 jungle-2.img jungle-2.fzf`,
				Flags: []cli.Flag{
					&cli.UintFlag{
						Name:  "disk-num",
						Value: 1,
						Usage: "which disk this is in a 2-disk split (1 = first/only disk, 2 = second disk)",
					},
				},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 2 {
						return cli.Exit("usage: fizzle disk add [--disk-num N] IMAGE FILE", exitUsage)
					}
					diskNum, err := disk.ValidateDiskNum(int(cmd.Uint("disk-num"))) //nolint:gosec // urfave/cli rejects negative uints
					if err != nil {
						return cli.Exit(err.Error(), exitUsage)
					}
					return diskadd.Add(cmd.Args().Get(0), cmd.Args().Get(1), diskNum)
				},
			},
			{
				Name:      "get",
				Usage:     "extract a named file from a disk image",
				ArgsUsage: "IMAGE NAME OUTPUT",
				UsageText: `Extract a file from a disk image by its name on the disk.
Use 'fizzle disk ls' to see what files are on a disk and what they are named.
The name is matched case-insensitively.

   IMAGE   the .img disk image file to read from
   NAME    the name of the file as it appears on the disk (e.g. KICK)
   OUTPUT  the file to write on your computer (e.g. kick.fzv)

Example:
   fizzle disk get mydrums.img KICK kick.fzv`,
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 3 {
						return cli.Exit("usage: fizzle disk get IMAGE NAME OUTPUT", exitUsage)
					}
					return diskget.Get(cmd.Args().Get(0), cmd.Args().Get(1), cmd.Args().Get(2))
				},
			},
			{
				Name:      "copy",
				Usage:     "copy a named file from one disk image to another",
				ArgsUsage: "SRC-IMAGE NAME DEST-IMAGE",
				UsageText: `Copy a file from one disk image to another in a single step.
Equivalent to 'disk get' followed by 'disk add'.

   SRC-IMAGE   the .img disk image file to copy from
   NAME        the name of the file on the source disk (case-insensitive)
   DEST-IMAGE  the .img disk image file to copy into

Example:
   fizzle disk copy library.img HOOVER mydrums.img`,
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 3 {
						return cli.Exit("usage: fizzle disk copy SRC-IMAGE NAME DEST-IMAGE", exitUsage)
					}
					return diskcopy.Copy(cmd.Args().Get(0), cmd.Args().Get(1), cmd.Args().Get(2))
				},
			},
		},
	}
}
