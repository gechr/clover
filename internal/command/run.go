package command

import (
	"context"
	"slices"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/auth"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/console"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/report"
	"github.com/gechr/clover/internal/tag"
)

// runCmd resolves every directive's version and rewrites it in place.
type runCmd struct {
	Paths          []string      `arg:"" optional:"" name:"path" help:"Files or directories to scan"                        predictor:"path" clib:"terse='Paths to scan'"`
	Tags           []string      `                   name:"tag"  help:"Only process directives matching these tags"                          clib:"terse='Filter by tags'"    short:"t" aliases:"tags" placeholder:"<tag>"`
	DryRun         bool          `                               help:"Resolve and render but write nothing"                                 clib:"terse='Dry run'"           short:"n" aliases:"dry"`
	AllowDowngrade *bool         `                               help:"Allow selecting versions older than the current one"                  clib:"terse='Allow downgrades'"                                               negatable:""`
	Prerelease     *bool         `                               help:"Allow selecting prerelease versions"                                  clib:"terse='Allow prereleases'"                                              negatable:""`
	Output         report.Output `                               help:"Output detail"                                                        clib:"terse='Output detail'"     short:"o"                                                 enum:"text,wide" default:"text"`
}

// Run resolves the markers under the given paths and reports a summary.
func (c *runCmd) Run(cfg *config.Config) error {
	ctx := context.Background()
	reporter := console.New(ctx, clog.Default)

	filter := tag.Parse(c.Tags)
	if !filter.Empty() {
		clog.Info().Str("tags", filter.String()).Msg("Filtering by tags")
	}

	summary, err := mode.Run(ctx, roots(c.Paths), c.DryRun,
		pipeline.WithReporter(reporter),
		pipeline.WithExclude(cfg.ExcludeGlobs()),
		pipeline.WithTagFilter(filter),
		pipeline.WithAllowDowngrade(c.AllowDowngrade),
		pipeline.WithPrerelease(c.Prerelease),
	)
	if err != nil {
		return err
	}

	reportAuth(ctx, summary)
	report.Run(clog.Default, summary, c.DryRun, c.Output)
	return nil
}

// reportAuth hints, actionably, when a used provider fell back to anonymous
// access - the usual cause of rate-limit failures.
func reportAuth(ctx context.Context, summary mode.Summary) {
	for _, status := range auth.Check(ctx, usedProviders(summary)) {
		if status.Authenticated {
			continue
		}
		clog.Hint().
			Str("provider", status.Provider).
			Str("action", status.Hint).
			Msg("Using anonymous access")
	}
}

// usedProviders returns the distinct upstream providers the run's markers used,
// sorted, excluding followers (which resolve from another marker, not a
// provider).
func usedProviders(summary mode.Summary) []string {
	seen := map[string]bool{}
	var names []string
	for _, outcome := range summary.Outcomes {
		for _, result := range outcome.Results {
			name := result.Marker.Provider
			if name == "" || name == constant.ProviderFollow || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}
