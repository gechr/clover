package command

import (
	"context"
	"errors"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/auth"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/console"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/report"
	"github.com/gechr/clover/internal/tui"
	xslices "github.com/gechr/x/slices"
	"github.com/gechr/x/terminal"
)

// runCmd resolves every directive's version and rewrites it in place.
type runCmd struct {
	Paths          []string      `arg:"" optional:"" name:"path" help:"Files or directories to scan"                                                           predictor:"path" clib:"terse='Paths to scan'"`
	Tags           []string      `                   name:"tag"  help:"Only process directives matching these tags"                                                             clib:"terse='Filter by tags'"    short:"t" aliases:"tags" placeholder:"<tag>"`
	DryRun         bool          `                               help:"Resolve and render but write nothing"                                                                    clib:"terse='Dry run'"           short:"n" aliases:"dry"`
	Deep           bool          `                               help:"Follow pagination to fetch every version (more accurate, but slower and more requests)"                  clib:"terse='Deep lookup'"`
	Yes            bool          `                               help:"Proceed without confirming a deep lookup"                                                                clib:"terse='Assume yes'"        short:"y"`
	AllowDowngrade *bool         `                               help:"Allow selecting versions older than the current one"                                                     clib:"terse='Allow downgrades'"                                               negatable:""`
	Prerelease     *bool         `                               help:"Allow selecting prerelease versions"                                                                     clib:"terse='Allow prereleases'"                                              negatable:""`
	Output         report.Output `                               help:"Output detail"                                                                                           clib:"terse='Output detail'"     short:"o"                                                 enum:"text,wide" default:"text"`
}

// Run resolves the markers under the given paths and reports a summary.
func (c *runCmd) Run(cfg *config.Config) error {
	launch()
	start := time.Now()
	ctx := context.Background()
	reporter := console.New(ctx, clog.Default)

	filter, err := tagFilter(c.Tags)
	if err != nil {
		return err
	}

	if c.Deep && !confirmDeep(c.Yes) {
		clog.Info().Msg("Deep lookup cancelled")
		return nil
	}

	// Collect truncated lookups during the run and report them after, so the
	// hints do not interleave with the live progress display.
	var (
		mu        sync.Mutex
		truncated []string
	)
	summary, err := mode.Run(ctx, roots(c.Paths), c.DryRun,
		pipeline.WithReporter(reporter),
		pipeline.WithExclude(cfg.ExcludeGlobs()),
		pipeline.WithTagFilter(filter),
		pipeline.WithDeep(c.Deep),
		pipeline.WithTruncationSink(func(resource string) {
			mu.Lock()
			defer mu.Unlock()
			truncated = append(truncated, resource)
		}),
		pipeline.WithAllowDowngrade(c.AllowDowngrade),
		pipeline.WithPrerelease(c.Prerelease),
	)
	if err != nil {
		return err
	}
	summary.Elapsed = time.Since(start)

	reportAuth(ctx, summary)
	reportDeep(summary, truncated)
	report.Run(clog.Default, summary, c.DryRun, c.Output)
	return nil
}

// reportDeep hints, after a run, that a deeper lookup might help: it warns about
// each lexically-ordered registry whose shallow listing was truncated (the newest
// version may have been missed), and once if any marker found no matching version
// on the first page. Both suggest --deep.
func reportDeep(summary mode.Summary, truncated []string) {
	for _, resource := range xslices.Unique(truncated) {
		clog.Warn().
			Str(field.Resource, resource).
			Str(field.Hint, "pass --deep").
			Msg("Shallow lookup may have missed newer versions")
	}

	for _, outcome := range summary.Outcomes {
		for _, result := range outcome.Results {
			if errors.Is(result.Err, pipeline.ErrNoCandidate) {
				clog.Hint().
					Str(field.Hint, "pass --deep").
					Msg("Some markers found no matching version on the first page")
				return
			}
		}
	}
}

// confirmDeep reports whether to go ahead with a deep lookup. --yes and a
// non-interactive session both proceed without prompting; on a TTY the user is
// warned that it may be slow or hit rate limits and asked to confirm.
func confirmDeep(yes bool) bool {
	if yes || !terminal.Is(os.Stdin) || !terminal.Is(os.Stdout) {
		return true
	}
	ok, err := tui.Confirm(
		"Run a deep lookup?",
		"A deep lookup fetches every page of versions - more accurate, but many "+
			"more requests that may be slow or hit rate limits.",
	)
	return err == nil && ok
}

// reportAuth hints, actionably, when a used provider fell back to anonymous
// access - the usual cause of rate-limit failures.
func reportAuth(ctx context.Context, summary mode.Summary) {
	for _, status := range auth.Check(ctx, usedProviders(summary)) {
		if status.Authenticated {
			continue
		}
		clog.Hint().
			Str(field.Provider, status.Provider).
			Str(field.Hint, status.Hint).
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
