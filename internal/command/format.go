package command

import (
	"context"
	"fmt"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/report"
	"github.com/gechr/x/human"
)

// cmdFormat canonicalises directive comments. With --check it reports what would
// change and exits non-zero without writing - the formatting CI gate.
type cmdFormat struct {
	Paths []string `name:"path" help:"Files or directories to scan"                                            arg:"" optional:"" clib:"terse='Paths to scan'"      predictor:"path"`
	Check bool     `            help:"Report directives that need formatting and exit non-zero (do not write)"                    clib:"terse='Check only'"`
	Prune bool     `            help:"Remove unknown keys instead of erroring on them"                                            clib:"terse='Prune unknown keys'"`
}

// Run canonicalises (or, with --check, checks) the directives under the paths.
func (c *cmdFormat) Run(cfg *config.Config) error {
	launch()
	ctx := context.Background()

	summary, err := mode.Format(
		ctx,
		roots(c.Paths),
		c.Check,
		c.Prune,
		pipeline.WithExclude(cfg.ExcludeGlobs()),
	)
	if err != nil {
		return err
	}

	report.Format(clog.Default, summary, c.Check)
	// An unknown key fails format like it fails lint and run, so a stale or
	// mistyped key cannot pass a CI gate; --prune removes them instead, so there
	// is nothing left to reject.
	if summary.Errored() > 0 {
		return fmt.Errorf(
			"%s with an unknown key (use --prune to remove)",
			human.Pluralize(summary.Errored(), "directive", "directives"),
		)
	}
	if c.Check && summary.Changed() > 0 {
		return fmt.Errorf(
			"%s would be reformatted",
			human.Pluralize(summary.Changed(), "directive", "directives"),
		)
	}
	return nil
}
