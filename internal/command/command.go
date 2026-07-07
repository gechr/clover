// Package command implements the clover CLI: argument parsing and dispatch to
// the run, lint, and format modes. main configures logging then calls Run.
package command

import (
	"errors"
	"os"

	"github.com/alecthomas/kong"
	clib "github.com/gechr/clib/cli/kong"
	"github.com/gechr/clive"
	"github.com/gechr/clive/updater"
	"github.com/gechr/clive/version"
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/logger"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/all"
	"github.com/gechr/clover/internal/provider/http"
	"github.com/gechr/clover/internal/tag"
	"github.com/gechr/conductor"
	cli "github.com/gechr/conductor/cli/kong"
)

const (
	name        = "clover"
	description = "Keep version references synchronised with their upstream sources of truth."

	exitSuccess = 0
	exitFailure = 1

	// scanLabelComments labels the scan-progress line for commands that walk for
	// existing directives (run, lint, format); scanLabelCandidates for annotate,
	// which walks for lines it could newly annotate.
	scanLabelComments   = "Scanning for Clover comments"
	scanLabelCandidates = "Scanning for Clover candidates"
)

// root is the top-level command tree. The embedded CompletionFlags add the hidden
// shell-completion flags; each remaining field is a subcommand whose Run method
// kong invokes for the selected command.
type root struct {
	clib.CompletionFlags

	Config      string "help:\"Path to a `.clover.yaml` config file\"  placeholder:\"<path>\" clib:\"terse='Config file',group='Global Options/Configuration'\""
	NoConfig    bool   "help:\"Do not load any `.clover.yaml` config\"                      clib:\"terse='Skip config',group='Global Options/Configuration'\""
	Parallelism int    `help:"Maximum number of files processed concurrently" clib:"terse='Parallelism',group='Global Options/Execution'"  short:"P" default:"10" placeholder:"<n>"`
	Verbose     bool   `help:"Enable debug logs"                              clib:"terse='Debug logs',group='Global Options/Diagnostics'"`

	Annotate cmdAnnotate "help:\"Add `provider=auto` directives to detected version lines\"   clib:\"terse='Add directives'\" cmd:\"\""
	Format   cmdFormat   `help:"Canonicalise directive comments"                             clib:"terse='Format comments'"  cmd:"" aliases:"fmt"`
	Init     cmdInit     "help:\"Create a starter `.clover.yaml` interactively\"                             clib:\"terse='Scaffold a config'\" cmd:\"\""
	Lint     cmdLint     `help:"Check every directive resolves, offline and without writing" clib:"terse='Check directives'" cmd:""`
	Login    cmdLogin    `help:"Authenticate Clover with a provider"                         clib:"terse='Authenticate'"     cmd:""`
	Run      cmdRun      `help:"Resolve version references and update them in place"         clib:"terse='Update versions'"  cmd:""`
	Update   cmdUpdate   `help:"Update Clover to the latest release via Homebrew"            clib:"terse='Self-update'"      cmd:"" aliases:"up"`
	Version  cmdVersion  `help:"Print version information"                                   clib:"terse='Print version'"    cmd:""`

	VersionFlag kong.VersionFlag `name:"version" short:"V" help:"Print version information" hidden:""`
}

// parallelism is the bound per-file worker count from the global -P flag, a named
// type so kong injects it into a command's Run by type without colliding with a
// bare int.
type parallelism int

// Run parses the command line and dispatches to the chosen mode, returning the
// process exit code.
func Run() int {
	app := conductor.New(conductor.App{
		Name:         name,
		DisplayName:  "Clover",
		Description:  description,
		Module:       module,
		Updater:      updateConfig(),
		NotifyOnly:   []string{"run", "lint", "format"},
		HelpLong:     "Print long help",
		ConfigureLog: logger.Configure,
	})

	provider.RegisterAll(all.New(http.WithVersion(clive.Current()))...)

	var r root
	prog, err := cli.New(app, &r)
	if err != nil {
		clog.Error().Err(err).Msg("Failed to build CLI")
		return exitFailure
	}

	// Completion runs before parsing so it works without a subcommand, which kong
	// would otherwise demand.
	kctx, handled, code, err := prog.Parse(os.Args[1:])
	if handled {
		return code
	}
	if err != nil {
		prog.Parser.FatalIfErrorf(err)
	}
	logger.SetVerbose(r.Verbose)

	resolver, err := newResolver(r)
	if err != nil {
		clog.Error().Err(err).Msg("Failed to load config")
		return exitFailure
	}
	kctx.Bind(resolver)
	kctx.Bind(parallelism(r.Parallelism))

	flush := app.Notify(kctx.Command())
	runErr := kctx.Run()
	code = exitSuccess
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
	if errors.Is(err, updater.ErrReported) {
		// The failure line is already on screen (e.g. a self-update timeout).
		return
	}
	if failures, ok := errors.AsType[failuresError](err); ok {
		clog.Error().Symbol("💥").Int(field.Failed, int(failures)).Msg("Experienced failures")
		return
	}
	clog.Error().Err(err).Msg("Failed")
}

// newResolver builds the per-root config resolver bound for command dispatch. It
// loads the user (XDG) config once - the base every project config overlays -
// and hands it to the resolver, which discovers each scanned tree's project
// config lazily. --config and --no-config short-circuit discovery, the latter
// skipping the user layer too for a fully unconfigured run. The required-version
// gate is no longer applied here: it runs per repository root inside the scan.
func newResolver(r root) (*config.Resolver, error) {
	var user *config.Config
	if !r.NoConfig {
		loaded, err := config.LoadUser()
		if err != nil {
			return nil, err
		}
		user = loaded
	}
	resolver := config.NewResolver(user, r.Config, r.NoConfig)
	// An explicit --config governs every path, so validate it once up front rather
	// than waiting for the scan to read it - a bad path or malformed file fails
	// fast with a clear "Failed to load config" before any work begins.
	if r.Config != "" {
		if _, err := resolver.ForDir("."); err != nil {
			return nil, err
		}
	}
	return resolver, nil
}

// launch logs the startup banner naming Clover's version, so a run records up
// front what version produced it. The version field is omitted when unknown
// rather than logged empty.
func launch() {
	event := clog.Info().Symbol("🍀")
	if v := version.RemovePrefix(clive.Current()); v != "" {
		event = event.Link(field.Version, clive.Info{Module: module}.VersionURL(v), v)
	}
	event.Msg("Launching Clover")
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
		clog.Info().Symbol("🏷️").Str(field.Tags, filter.String()).Msg("Filtering by tags")
	}
	return filter, nil
}

// providerFilter builds the --enable/--disable provider selection, logging the
// active filter once so a run records which providers it will touch.
func providerFilter(enable, disable []string) (provider.Filter, error) {
	filter, err := provider.NewFilter(enable, disable)
	if err != nil {
		return provider.Filter{}, err
	}
	if !filter.Empty() {
		clog.Info().Symbol("🔌").Str(field.Provider, filter.String()).Msg("Filtering by provider")
	}
	return filter, nil
}
