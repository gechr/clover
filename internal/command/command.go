// Package command implements the clover CLI: argument parsing and dispatch to
// the run, lint, and format modes. main configures logging then calls Run.
package command

import (
	"cmp"
	"os"
	"strings"

	"github.com/alecthomas/kong"
	clib "github.com/gechr/clib/cli/kong"
	"github.com/gechr/clib/complete"
	"github.com/gechr/clib/help"
	"github.com/gechr/clib/theme"
	"github.com/gechr/clive"
	"github.com/gechr/clive/update/notify"
	"github.com/gechr/clive/version"
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/logger"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/docker"
	"github.com/gechr/clover/internal/provider/github"
	"github.com/gechr/clover/internal/provider/helm"
	"github.com/gechr/clover/internal/provider/manual"
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

	Init    cmdInit    "help:\"Create a starter `.clover.yaml` interactively\"                             clib:\"terse='Scaffold a config'\" cmd:\"\""
	Login   cmdLogin   `help:"Authenticate clover with a provider via its device flow"     clib:"terse='Authenticate'"     cmd:""`
	Run     cmdRun     `help:"Resolve version references and update them in place"         clib:"terse='Update versions'"  cmd:""`
	Lint    cmdLint    `help:"Check every directive resolves, offline and without writing" clib:"terse='Check directives'" cmd:""`
	Format  cmdFormat  `help:"Canonicalise directive comments"                             clib:"terse='Format comments'"  cmd:"" aliases:"fmt"`
	Update  cmdUpdate  `help:"Update clover to the latest release via Homebrew"            clib:"terse='Self-update'"      cmd:""`
	Version cmdVersion `help:"Print version information"                                   clib:"terse='Print version'"    cmd:""`
}

// Run parses the command line and dispatches to the chosen mode, returning the
// process exit code.
func Run() int {
	provider.RegisterAll(
		docker.New(),
		github.New(),
		helm.New(),
		manual.New(),
	)

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
			help.NewRenderer(theme.Auto()),
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

	cfg, err := loadConfig(root)
	if err != nil {
		clog.Error().Err(err).Msg("Failed to load config")
		return exitFailure
	}
	if err := cfg.CheckVersion(clive.Current()); err != nil {
		clog.Error().Err(err).Msg("Version requirement not met")
		return exitFailure
	}
	kctx.Bind(cfg)

	flush := startNotify(kctx.Command())
	runErr := kctx.Run()
	code := exitSuccess
	if runErr != nil {
		clog.Error().Err(runErr).Msg("Command failed")
		code = exitFailure
	}
	flush()
	return code
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

// loadConfig loads the effective config: the user config overlaid by the
// project config. It honours --config and --no-config, the latter skipping both
// layers for a fully unconfigured run.
func loadConfig(root cli) (*config.Config, error) {
	if root.NoConfig {
		return nil, nil //nolint:nilnil // no config requested
	}
	user, err := config.LoadUser()
	if err != nil {
		return nil, err
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	project, err := config.Load(dir, root.Config)
	if err != nil {
		return nil, err
	}
	return config.Merge(user, project), nil
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
		event = event.Str(field.Version, v)
	}
	event.Msg("Launching Clover")
}

// enabled reports whether a tri-state bool pointer is set and true, the form a
// resolved CLI-or-config toggle takes before it feeds a plain-bool decision.
func enabled(b *bool) bool { return b != nil && *b }

// resolveDeep reports whether a run does a deep lookup: an explicit --deep /
// --no-deep wins over a configured run.deep, and --verify forces it on because
// verification needs the complete history.
func resolveDeep(cli *bool, cfg *config.Config, verify *bool) bool {
	return enabled(cmp.Or(cli, cfg.Deep())) || enabled(verify)
}

// resolvePrune reports whether format strips unknown keys: an explicit --prune /
// --no-prune wins over a configured fmt.prune.
func resolvePrune(cli *bool, cfg *config.Config) bool {
	return enabled(cmp.Or(cli, cfg.Prune()))
}

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
		clog.Info().Str(field.Tags, filter.String()).Msg("Filtering by tags")
	}
	return filter, nil
}
