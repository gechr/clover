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

// cmdRun resolves every directive's version and rewrites it in place.
type cmdRun struct {
	Paths      []string      `name:"path" help:"Files or directories to scan"                                         arg:"" optional:"" clib:"terse='Paths to scan'"     predictor:"path"`
	Tags       []string      `name:"tag"  help:"Only process directives matching these tags"                                             clib:"terse='Filter by tags'"                     short:"t" aliases:"tags" placeholder:"<tag>"`
	DryRun     bool          `            help:"Resolve and render but write nothing"                                                    clib:"terse='Dry run'"                            short:"n" aliases:"dry"`
	Deep       bool          `            help:"Follow pagination to fetch every version (more accurate, but slower)"                    clib:"terse='Deep lookup'"`
	Yes        bool          `            help:"Proceed without confirming a deep lookup"                                                clib:"terse='Assume yes'"                         short:"y"`
	Downgrade  *bool         `            help:"Allow selecting versions older than the current one"                                     clib:"terse='Allow downgrades'"                                                                negatable:""`
	Prerelease *bool         `            help:"Allow selecting prerelease versions"                                                     clib:"terse='Allow prereleases'"                                                               negatable:""`
	Verify     *bool         "            help:\"Perform additional verification against upstream tags (implies `--deep`)\"                          negatable:\"\"                                        clib:\"terse='Verify tags'\""
	Output     report.Output `            help:"Output detail"                                                                           clib:"terse='Output detail'"                      short:"o"                                                 default:"text" enum:"text,wide,github"`
}

// Run resolves the markers under the given paths and reports a summary.
func (c *cmdRun) Run(cfg *config.Config) error {
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
	// --verify needs the complete tag and branch history to resolve the true
	// newest version and its commit, so it implies a deep lookup. It is its own
	// explicit opt-in, so it does not trigger the deep-lookup confirmation.
	deep := c.Deep || (c.Verify != nil && *c.Verify)

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
		pipeline.WithDeep(deep),
		pipeline.WithTruncationSink(func(resource string) {
			mu.Lock()
			defer mu.Unlock()
			truncated = append(truncated, resource)
		}),
		pipeline.WithDowngrade(c.Downgrade),
		pipeline.WithPrerelease(c.Prerelease),
		pipeline.WithVerify(c.Verify),
	)
	if err != nil {
		return err
	}
	summary.Elapsed = time.Since(start)

	// GitHub mode emits machine-parseable annotations only; the human hints would
	// be noise in a CI log.
	if c.Output == report.OutputGitHub {
		report.GitHub(os.Stdout, summary, c.DryRun)
		return nil
	}

	reportAuth(ctx, summary)
	reportDeep(summary, truncated, c.Deep)
	report.Run(clog.Default, summary, c.DryRun, c.Output)
	return nil
}

// reportDeep hints, after a run, that a deeper lookup might help: it warns about
// each lexically-ordered registry whose shallow listing was truncated (the newest
// version may have been missed), and once if any marker found no matching version
// on the first page. Both suggest --deep. The no-candidate hint fires on any
// ErrNoCandidate, so it can show when --deep will not help (nothing qualifies at
// all) - the wording stays a suggestion rather than a promise.
func reportDeep(summary mode.Summary, truncated []string, deep bool) {
	resources, noCandidate := deepHints(summary, truncated, deep)
	for _, resource := range resources {
		clog.Warn().
			Str(field.Resource, resource).
			Str(field.Hint, "pass --deep").
			Msg("Shallow lookup may have missed newer versions")
	}
	if noCandidate {
		clog.Hint().
			Str(field.Hint, "pass --deep to fetch all pages").
			Msg("No matching version found in first page of results")
	}
}

// deepHints is the pure decision behind reportDeep: which truncated resources to
// warn about, and whether any marker found no candidate on the first page. A deep
// run already paged to exhaustion, so it suggests nothing.
func deepHints(summary mode.Summary, truncated []string, deep bool) ([]string, bool) {
	if deep {
		return nil, false
	}
	resources := xslices.Unique(truncated)
	for _, outcome := range summary.Outcomes {
		for _, result := range outcome.Results {
			if errors.Is(result.Err, pipeline.ErrNoCandidate) {
				return resources, true
			}
		}
	}
	return resources, false
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
