package command

import (
	"context"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/console"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/pipeline"
)

// runCmd resolves every directive's version and rewrites it in place.
type runCmd struct {
	Paths  []string `arg:"" optional:"" name:"path" help:"Files or directories to scan (default: current directory)."`
	DryRun bool     `                               help:"Resolve and render but write nothing."                      short:"n"`
}

// Run resolves the markers under the given paths and reports a summary.
func (c *runCmd) Run() error {
	ctx := context.Background()
	reporter := console.New(ctx, clog.Default)

	summary, err := mode.Run(ctx, roots(c.Paths), c.DryRun, pipeline.WithReporter(reporter))
	if err != nil {
		return err
	}

	event := clog.Info()
	if c.DryRun {
		event = clog.Dry()
	}
	event.
		Int("changed", summary.Changed()).
		Int("skipped", summary.Skipped()).
		Int("failed", summary.Errored()).
		Msg("Run complete")
	return nil
}
