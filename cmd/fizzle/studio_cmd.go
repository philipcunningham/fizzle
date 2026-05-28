package main

import (
	"context"

	"github.com/urfave/cli/v3"

	"github.com/philipcunningham/fizzle/pkg/studio/app"
)

// studioCmd registers the `fizzle studio` subcommand. The studio is a
// document-centric editor: one .fzf or .img per session.
func studioCmd() *cli.Command {
	return &cli.Command{
		Name:      "studio",
		Usage:     "edit a Casio FZ-1 full dump or disk image",
		ArgsUsage: "FILE",
		UsageText: `Launch the terminal UI for editing an FZ full dump or
voice file. FILE is a standalone .fzf or an .img disk image; for
.img the full-dump entry is extracted into memory automatically.

Example:
   fizzle studio sounds/CASIO001.fzf
   fizzle studio disk-images/01.img`,
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return cli.Exit("usage: fizzle studio FILE", exitUsage)
			}
			return app.Run(cmd.Args().Get(0))
		},
	}
}
