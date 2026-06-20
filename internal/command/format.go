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
	Paths []string `name:"path" help:"Files or directories to scan"                                            arg:"" optional:"" clib:"terse='Paths to scan'" predictor:"path"`
	Check bool     `            help:"Report directives that need formatting and exit non-zero (do not write)"                    clib:"terse='Check only'"`
}

// Run canonicalises (or, with --check, checks) the directives under the paths.
func (c *cmdFormat) Run(cfg *config.Config) error {
	launch()
	ctx := context.Background()

	summary, err := mode.Format(
		ctx,
		roots(c.Paths),
		c.Check,
		pipeline.WithExclude(cfg.ExcludeGlobs()),
	)
	if err != nil {
		return err
	}

	report.Format(clog.Default, summary, c.Check)
	if c.Check && !summary.OK() {
		return fmt.Errorf(
			"%s would be reformatted",
			human.Pluralize(summary.Changed(), "directive", "directives"),
		)
	}
	return nil
}
