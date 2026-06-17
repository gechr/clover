// Package command implements the clover CLI: argument parsing and dispatch to
// the run, lint, and format modes. main configures logging then calls Run.
package command

import (
	"os"

	"github.com/alecthomas/kong"
	clib "github.com/gechr/clib/cli/kong"
	"github.com/gechr/clib/complete"
	"github.com/gechr/clib/help"
	"github.com/gechr/clib/theme"
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/github"
)

const (
	name        = "clover"
	description = "Keep version references synchronised with their upstream sources of truth."

	exitSuccess = 0
	exitFailure = 1
)

// cli is the top-level command tree. The embedded CompletionFlags add the hidden
// shell-completion flags; each remaining field is a subcommand whose Run method
// kong invokes for the selected command.
type cli struct {
	clib.CompletionFlags

	Run    runCmd    `cmd:"" help:"Resolve version references and update them in place."`
	Lint   lintCmd   `cmd:"" help:"Check every directive resolves, offline and without writing."`
	Format formatCmd `cmd:"" help:"Canonicalise directive comments."`
}

// Run parses the command line and dispatches to the chosen mode, returning the
// process exit code.
func Run() int {
	provider.RegisterAll(github.New())

	var root cli

	flags, err := clib.Reflect(&root)
	if err != nil {
		clog.Error().Err(err).Msg("Failed to inspect CLI")
		return exitFailure
	}
	gen := newGenerator(flags)

	// Completion runs before parsing so it works without a subcommand, which kong
	// would otherwise demand.
	if completion, args, ok := clib.Preflight(); ok {
		if _, handleErr := completion.Handle(gen, nil, clib.WithArgs(args)); handleErr != nil {
			clog.Error().Err(handleErr).Msg("Completion failed")
			return exitFailure
		}
		return exitSuccess
	}

	parser := kong.Must(&root,
		kong.Name(name),
		kong.Description(description),
		kong.Help(clib.HelpPrinterFunc(
			help.NewRenderer(theme.Auto()),
			clib.NodeSectionsFunc(),
			help.WithHelpFlags("Print short help", "Print long help"),
		)),
		kong.Bind(gen),
	)
	gen.Subs = clib.Subcommands(parser)

	kctx, err := parser.Parse(os.Args[1:])
	if err != nil {
		parser.FatalIfErrorf(err)
	}

	if err := kctx.Run(); err != nil {
		clog.Error().Err(err).Msg("Command failed")
		return exitFailure
	}
	return exitSuccess
}

// newGenerator builds the shell-completion generator from the CLI's flags, plus
// specs for the help flags kong adds itself.
func newGenerator(flags []complete.FlagMeta) *complete.Generator {
	gen := complete.NewGenerator(name).FromFlags(flags)
	gen.Specs = append(gen.Specs,
		complete.Spec{ShortFlag: "h", Terse: "Print short help"},
		complete.Spec{LongFlag: "help", Terse: "Print long help"},
	)
	return gen
}

// roots returns the paths to scan, defaulting to the current directory when none
// are given.
func roots(paths []string) []string {
	if len(paths) == 0 {
		return []string{"."}
	}
	return paths
}
