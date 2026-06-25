package command

import (
	"context"
	"fmt"
	"os"

	"github.com/gechr/clive"
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/output"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/report"
)

// cmdLint checks every directive resolves, offline and without writing. It is
// the CI gate: a non-zero exit means at least one directive will not resolve.
type cmdLint struct {
	Paths  []string     `name:"path" help:"Files or directories to scan"              arg:"" optional:"" clib:"terse='Paths to scan'"  predictor:"path"`
	Tags   []string     `name:"tag"  help:"Only check directives matching these tags"                    clib:"terse='Filter by tags'"                  short:"t" aliases:"tags" placeholder:"<tag>"`
	Output *output.Mode "            help:\"Output detail\"                                                clib:\"terse='Output detail'\"                   short:\"o\"                                                   enum:\"text,wide,github\""
}

// Run validates the markers under the given paths and fails when any did not.
func (c *cmdLint) Run(configs *config.Resolver) error {
	launch()
	ctx := context.Background()

	filter, err := tagFilter(c.Tags)
	if err != nil {
		return err
	}

	summary, err := mode.Lint(ctx, roots(c.Paths),
		pipeline.WithConfig(configs),
		pipeline.WithVersion(clive.Current()),
		pipeline.WithTagFilter(filter),
	)
	if err != nil {
		return err
	}

	detail := configs.Primary().LintOutput(c.Output)
	if detail == output.GitHub {
		report.GitHub(os.Stdout, summary, false)
	} else {
		report.Lint(clog.Default, summary, detail)
	}
	if !summary.OK() {
		return fmt.Errorf("%d errored, %d skipped", summary.Errored(), summary.Skipped())
	}
	return nil
}
