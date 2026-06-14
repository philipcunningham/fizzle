package main

import (
	"context"

	"github.com/urfave/cli/v3"

	studio "github.com/philipcunningham/fizzle/pkg/studio/app"
)

// studioCmd registers the `fizzle studio` subcommand. studio is a
// workspace-oriented Bubble Tea TUI for editing FZ-1 / FZ-10M /
// FZ-20M sound material. DIRECTORY is optional and points at the
// workspace root containing the .img / .fzf / .fzv / .wav files
// to browse. Omitting DIRECTORY uses the current working
// directory. Individual files are opened from the Workspace
// browser inside the TUI, not from the CLI.
func studioCmd() *cli.Command {
	return &cli.Command{
		Name:      "studio",
		Usage:     "interactive TUI for editing FZ-1 sound material",
		ArgsUsage: "[DIRECTORY]",
		UsageText: `Launch the studio TUI. DIRECTORY is optional and
points at the workspace folder containing .img / .fzf / .fzv /
.wav files. Files inside the workspace are opened from the
Workspace browser inside the TUI. Omitting DIRECTORY uses the
current working directory.

Examples:
   fizzle studio                  # use cwd as workspace
   fizzle studio ~/fz-library     # use a directory as workspace`,
		Action: func(_ context.Context, cmd *cli.Command) error {
			directory := ""
			if cmd.Args().Len() >= 1 {
				directory = cmd.Args().Get(0)
			}
			return studio.Run(directory)
		},
	}
}
