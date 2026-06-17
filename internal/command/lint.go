package command

import (
	"context"
	"fmt"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/report"
	"github.com/gechr/clover/internal/tag"
)

// lintCmd checks every directive resolves, offline and without writing. It is
// the CI gate: a non-zero exit means at least one directive will not resolve.
type lintCmd struct {
	Paths  []string      `arg:"" optional:"" name:"path" help:"Files or directories to scan"                                    predictor:"path" clib:"terse='Paths to scan'"`
	Tags   []string      `                   name:"tag"  help:"Only check directives carrying these tags (a,b = and; a/b = or)"                  clib:"terse='Filter by tags'" short:"t" aliases:"tags" placeholder:"<tag>"`
	Output report.Output `                               help:"Output detail"                                                                    clib:"terse='Output detail'"  short:"o"                                    enum:"text,wide" default:"text"`
}

// Run validates the markers under the given paths and fails when any did not.
func (c *lintCmd) Run(cfg *config.Config) error {
	ctx := context.Background()

	filter := tag.Parse(c.Tags)
	if !filter.Empty() {
		clog.Info().Str("tags", filter.String()).Msg("Filtering by tags")
	}

	summary, err := mode.Lint(ctx, roots(c.Paths),
		pipeline.WithExclude(cfg.ExcludeGlobs()),
		pipeline.WithTagFilter(filter),
	)
	if err != nil {
		return err
	}

	report.Lint(clog.Default, summary, c.Output)
	if !summary.OK() {
		return fmt.Errorf("%d errored, %d skipped", summary.Errored(), summary.Skipped())
	}
	return nil
}
