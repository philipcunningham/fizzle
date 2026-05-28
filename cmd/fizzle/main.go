// fizzle is a tool for working with FZ series samplers (FZ-1, FZ-10M, FZ-20M).
// It manages virtual floppy disk images and converts audio between WAV and
// the FZ voice format.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"

	"github.com/philipcunningham/fizzle/internal/licenses"
	"github.com/philipcunningham/fizzle/pkg/logger"
	fizzleversion "github.com/philipcunningham/fizzle/pkg/version"
)

const (
	exitUsage = 2
	exitError = 1

	flagJSON      = "json"
	flagJSONUsage = "output as JSON"
	argsUsageFZV  = "FZV"
	argsUsageFZF  = "FZF"
	argsUsageFZB  = "FZB"
	flagVoice     = "voice"
	subcmdInfo    = "info"
)

func main() {
	os.Exit(run())
}

func licensesCmd() *cli.Command {
	return &cli.Command{
		Name:  "licenses",
		Usage: "show the project license and third-party attribution",
		UsageText: `Print the project license followed by the full text of every
third-party dependency's license. Use --json to emit the CycloneDX
software bill of materials (SBOM) instead, which lists each module
and its license identifier in structured JSON suitable for
supply-chain tooling.

The text output satisfies the attribution clauses of the permissive
licenses in the dependency graph (MIT, BSD, Apache-2.0). The SBOM
alone does not, because it carries license identifiers but not the
verbatim notices those licenses require.

Example:
   fizzle licenses
   fizzle licenses --json | jq '.components[] | {name, licenses}'`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  flagJSON,
				Usage: "emit the CycloneDX SBOM as JSON instead of full attribution text",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Bool(flagJSON) {
				fmt.Println(licenses.SBOM)
				return nil
			}
			fmt.Print(licenses.Project)
			fmt.Print(licenses.ThirdParty)
			return nil
		},
	}
}

func run() int {
	app := &cli.Command{
		Name:                  "fizzle",
		Version:               fizzleversion.String(),
		Usage:                 "FZ series sampler disk and voice tool",
		EnableShellCompletion: true,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "debug",
				Usage: "enable debug logging",
			},
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			logger.Init(cmd.Bool("debug"))
			return ctx, nil
		},
		Commands: []*cli.Command{
			diskCmd(),
			fzvCmd(),
			fzfCmd(),
			fzbCmd(),
			sfzCmd(),
			studioCmd(),
			licensesCmd(),
		},
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "fizzle: %v\n", err)
		return 1
	}
	return 0
}
