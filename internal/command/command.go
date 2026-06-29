// Package command implements the clover CLI: argument parsing and dispatch to
// the run, lint, and format modes. main configures logging then calls Run.
package command

import (
	"errors"
	"os"
	"strings"

	"github.com/alecthomas/kong"
	clib "github.com/gechr/clib/cli/kong"
	"github.com/gechr/clib/complete"
	"github.com/gechr/clib/help"
	"github.com/gechr/clib/theme"
	"github.com/gechr/clive"
	"github.com/gechr/clive/notify"
	"github.com/gechr/clive/version"
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/logger"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/all"
	"github.com/gechr/clover/internal/provider/http"
	"github.com/gechr/clover/internal/tag"
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

	Config   string "help:\"Path to a `.clover.yaml` config file\"  placeholder:\"<path>\" clib:\"terse='Config file'\""
	NoConfig bool   "help:\"Do not load any `.clover.yaml` config\"                      clib:\"terse='Skip config'\""
	Verbose  bool   `help:"Enable debug logs" clib:"terse='Debug logs'"`

	Init     cmdInit     "help:\"Create a starter `.clover.yaml` interactively\"                             clib:\"terse='Scaffold a config'\" cmd:\"\""
	Login    cmdLogin    `help:"Authenticate Clover with a provider"                         clib:"terse='Authenticate'"     cmd:""`
	Run      cmdRun      `help:"Resolve version references and update them in place"         clib:"terse='Update versions'"  cmd:""`
	Lint     cmdLint     `help:"Check every directive resolves, offline and without writing" clib:"terse='Check directives'" cmd:""`
	Format   cmdFormat   `help:"Canonicalise directive comments"                             clib:"terse='Format comments'"  cmd:"" aliases:"fmt"`
	Annotate cmdAnnotate `help:"Add provider=auto directives to recognised version lines"    clib:"terse='Add directives'"   cmd:"" aliases:"add"`
	Update   cmdUpdate   `help:"Update Clover to the latest release via Homebrew"            clib:"terse='Self-update'"      cmd:"" aliases:"up"`
	Version  cmdVersion  `help:"Print version information"                                   clib:"terse='Print version'"    cmd:""`
}

// Run parses the command line and dispatches to the chosen mode, returning the
// process exit code.
func Run() int {
	provider.RegisterAll(all.New(http.WithVersion(clive.Current()))...)

	var root cli

	flags, err := clib.Reflect(&root)
	if err != nil {
		clog.Error().Err(err).Msg("Failed to inspect CLI")
		return exitFailure
	}
	gen := newGenerator(flags)

	parser := kong.Must(&root,
		kong.Name(name),
		kong.Description(description),
		kong.Help(clib.HelpPrinterFunc(
			help.NewRenderer(
				theme.Auto(),
				//nolint:mnd // self-explanatory
				help.WithDescriptionWidth(80),
			),
			clib.NodeSectionsFunc(),
			help.WithHelpFlags("Print short help", "Print long help"),
		)),
		kong.Bind(gen),
	)
	// Populate subcommand specs from the parser model before completion so the
	// generated script lists run/lint/format and their flags.
	gen.Subs = clib.Subcommands(parser)

	// Completion runs before parsing so it works without a subcommand, which kong
	// would otherwise demand.
	if completion, args, ok := clib.Preflight(); ok {
		if _, handleErr := completion.Handle(gen, nil, clib.WithArgs(args)); handleErr != nil {
			clog.Error().Err(handleErr).Msg("Completion failed")
			return exitFailure
		}
		return exitSuccess
	}

	kctx, err := parser.Parse(os.Args[1:])
	if err != nil {
		parser.FatalIfErrorf(err)
	}
	logger.SetVerbose(root.Verbose)

	resolver, err := newResolver(root)
	if err != nil {
		clog.Error().Err(err).Msg("Failed to load config")
		return exitFailure
	}
	kctx.Bind(resolver)

	flush := startNotify(kctx.Command())
	runErr := kctx.Run()
	code := exitSuccess
	if runErr != nil {
		reportExit(runErr)
		code = exitFailure
	}
	flush()
	return code
}

// reportExit logs the friendly error line for a non-zero exit. A command that
// finished with failed markers returns a failuresError carrying the count, which
// renders as a clean summary rather than a generic wrapped-error string; any
// other error (config load, I/O) falls back to the plain "Failed" log.
func reportExit(err error) {
	if failures, ok := errors.AsType[failuresError](err); ok {
		clog.Error().Symbol("💥").Int(field.Failed, int(failures)).Msg("Experienced failures")
		return
	}
	clog.Error().Err(err).Msg("Failed")
}

// startNotify begins clive's update check before dispatch, for the commands a
// user runs repeatedly, and returns a function that prints any pending hint once
// the command finishes. clive serves the hint from cache and refreshes in the
// background, so this never blocks or slows the run; CLOVER_NO_UPDATE_CHECK
// disables it.
func startNotify(command string) func() {
	switch firstField(command) {
	case "run", "lint", "format":
		return notify.Check(updateConfig())
	default:
		return func() {}
	}
}

// firstField returns the first space-separated word of s, the leading verb of a
// kong command path like "run <path>".
func firstField(s string) string {
	if before, _, ok := strings.Cut(s, " "); ok {
		return before
	}
	return s
}

// newResolver builds the per-root config resolver bound for command dispatch. It
// loads the user (XDG) config once - the base every project config overlays -
// and hands it to the resolver, which discovers each scanned tree's project
// config lazily. --config and --no-config short-circuit discovery, the latter
// skipping the user layer too for a fully unconfigured run. The required-version
// gate is no longer applied here: it runs per repository root inside the scan.
func newResolver(root cli) (*config.Resolver, error) {
	var user *config.Config
	if !root.NoConfig {
		loaded, err := config.LoadUser()
		if err != nil {
			return nil, err
		}
		user = loaded
	}
	resolver := config.NewResolver(user, root.Config, root.NoConfig)
	// An explicit --config governs every path, so validate it once up front rather
	// than waiting for the scan to read it - a bad path or malformed file fails
	// fast with a clear "Failed to load config" before any work begins.
	if root.Config != "" {
		if _, err := resolver.ForDir("."); err != nil {
			return nil, err
		}
	}
	return resolver, nil
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

// launch logs the startup banner naming clover's version, so a run records up
// front what version produced it. The version field is omitted when unknown
// rather than logged empty.
func launch() {
	event := clog.Info().Symbol("🍀")
	if v := version.RemovePrefix(clive.Current()); v != "" {
		event = event.Link(field.Version, clive.Info{Module: module}.VersionURL(v), v)
	}
	event.Msg("Launching Clover")
}

// enabled reports whether a tri-state bool pointer is set and true, the form a
// resolved CLI-or-config toggle takes before it feeds a plain-bool decision.
func enabled(b *bool) bool { return b != nil && *b }

// roots returns the paths to scan, defaulting to the current directory when none
// are given.
func roots(paths []string) []string {
	if len(paths) == 0 {
		return []string{"."}
	}
	return paths
}

// tagFilter parses the --tag values into a filter, logging the active filter
// once so a run records exactly which markers it will touch. Shared by run and
// lint, which apply the same selection.
func tagFilter(tags []string) (tag.Filter, error) {
	filter, err := tag.Parse(tags)
	if err != nil {
		return tag.Filter{}, err
	}
	if !filter.Empty() {
		clog.Info().Symbol("🏷️").Str(field.Tags, filter.String()).Msg("Filtering by tags")
	}
	return filter, nil
}
