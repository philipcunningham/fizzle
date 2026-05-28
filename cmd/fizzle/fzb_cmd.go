package main

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/philipcunningham/fizzle/pkg/fzbinfo"
)

func fzbCmd() *cli.Command {
	return &cli.Command{
		Name:        "fzb",
		Usage:       "work with FZ series bank dump files (.fzb)",
		Description: "A bank dump (.fzb) stores a bank sector (key mappings, velocity, MIDI channel, output assignment, effects) plus voice headers, but no audio data.",
		Commands: []*cli.Command{
			{
				Name:      subcmdInfo,
				Usage:     "show the voice map of a bank dump file",
				ArgsUsage: argsUsageFZB,
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: flagJSON, Usage: flagJSONUsage},
				},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 1 {
						return cli.Exit("usage: fizzle fzb info FZB", exitUsage)
					}
					info, err := fzbinfo.Parse(cmd.Args().Get(0))
					if err != nil {
						return err
					}
					if cmd.Bool(flagJSON) {
						return fzbinfo.RenderJSON(os.Stdout, info)
					}
					fzbinfo.Render(os.Stdout, info)
					return nil
				},
			},
		},
	}
}
