package command

import (
	"context"
	"fmt"
	"os"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/report"
)

// cmdLint checks every directive resolves, offline and without writing. It is
// the CI gate: a non-zero exit means at least one directive will not resolve.
type cmdLint struct {
	Paths  []string      `name:"path" help:"Files or directories to scan"              arg:"" optional:"" clib:"terse='Paths to scan'"  predictor:"path"`
	Tags   []string      `name:"tag"  help:"Only check directives matching these tags"                    clib:"terse='Filter by tags'"                  short:"t" aliases:"tags" placeholder:"<tag>"`
	Output report.Output `            help:"Output detail"                                                clib:"terse='Output detail'"                   short:"o"                                    default:"text" enum:"text,wide,github"`
}

// Run validates the markers under the given paths and fails when any did not.
func (c *cmdLint) Run(cfg *config.Config) error {
	launch()
	ctx := context.Background()

	filter, err := tagFilter(c.Tags)
	if err != nil {
		return err
	}

	summary, err := mode.Lint(ctx, roots(c.Paths),
		pipeline.WithExclude(cfg.ExcludeGlobs()),
		pipeline.WithTagFilter(filter),
	)
	if err != nil {
		return err
	}

	if c.Output == report.OutputGitHub {
		report.GitHub(os.Stdout, summary, false)
	} else {
		report.Lint(clog.Default, summary, c.Output)
	}
	if !summary.OK() {
		return fmt.Errorf("%d errored, %d skipped", summary.Errored(), summary.Skipped())
	}
	return nil
}
