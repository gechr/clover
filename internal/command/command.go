// Package command implements the clover CLI: argument parsing and dispatch to
// the run, lint, and format modes. main configures logging then calls Run.
package command

import (
	"github.com/alecthomas/kong"
	"github.com/gechr/clog"
	_ "github.com/gechr/clover/internal/provider/github" // register github provider
)

// cli is the top-level command tree. Each field is a subcommand whose Run method
// kong invokes for the selected command.
type cli struct {
	Run    runCmd    `cmd:"" help:"Resolve version references and update them in place."`
	Lint   lintCmd   `cmd:"" help:"Check every directive resolves, offline and without writing."`
	Format formatCmd `cmd:"" help:"Canonicalise directive comments."`
}

// Run parses the command line and dispatches to the chosen mode, returning the
// process exit code.
func Run() int {
	var root cli
	kctx := kong.Parse(
		&root,
		kong.Name(name),
		kong.Description(
			"Keep version references synchronised with their upstream sources of truth.",
		),
	)
	if err := kctx.Run(); err != nil {
		clog.Error().Err(err).Msg("Command failed")
		return exitFailure
	}
	return exitSuccess
}

const (
	name        = "clover"
	exitSuccess = 0
	exitFailure = 1
)

// roots returns the paths to scan, defaulting to the current directory when none
// are given.
func roots(paths []string) []string {
	if len(paths) == 0 {
		return []string{"."}
	}
	return paths
}
