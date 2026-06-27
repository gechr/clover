package command

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/gechr/clive"
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/auth"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/console"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/output"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/report"
	"github.com/gechr/clover/internal/tui"
	xslices "github.com/gechr/x/slices"
	"github.com/gechr/x/terminal"
)

// cmdRun resolves every directive's version and rewrites it in place.
type cmdRun struct {
	Paths      []string     `name:"path" help:"Files or directories to scan"                                             arg:"" optional:"" clib:"terse='Paths to scan'"     predictor:"path"`
	Tags       []string     `name:"tag"  help:"Only process directives matching these tags"                                                 clib:"terse='Filter by tags'"                     short:"t" aliases:"tags" placeholder:"<tag>"`
	NoIgnore   bool         `            help:"Scan files that .gitignore would exclude (VCS directories stay excluded)"                    clib:"terse='No ignore'"`
	DryRun     bool         `            help:"Resolve and render but write nothing"                                                        clib:"terse='Dry run'"                            short:"n" aliases:"dry"`
	Deep       *bool        `            help:"Follow pagination to fetch every version (more accurate, but slower)"                        clib:"terse='Deep lookup'"                                                                     negatable:""`
	Yes        bool         `            help:"Proceed without confirming a deep lookup"                                                    clib:"terse='Assume yes'"                         short:"y"`
	Downgrade  *bool        `            help:"Allow selecting versions older than the current one"                                         clib:"terse='Allow downgrades'"                                                                negatable:""`
	Prerelease *bool        `            help:"Allow selecting prerelease versions"                                                         clib:"terse='Allow prereleases'"                                                               negatable:""`
	Verify     *bool        "            help:\"Perform additional verification against upstream tags (implies `--deep`)\"                          negatable:\"\"                                        clib:\"terse='Verify tags'\""
	Output     *output.Mode "            help:\"Output detail\"                                                                           clib:\"terse='Output detail'\"                      short:\"o\"                                                                enum:\"text,wide,github\""
}

// Help returns the detailed blurb shown in `clover run --help`.
func (c *cmdRun) Help() string {
	return "Scans the given paths for `clover:` directives, resolves each one's " +
		"newest version allowed by its constraint from the upstream source, and " +
		"rewrites the target line in place. Paths default to the current " +
		"directory.\n\n" +
		"Use `--dry-run` to preview the changes without writing, and `--deep` to page " +
		"through every version when the newest may sit past the first page. Exits " +
		"non-zero when any directive fails to resolve, so it can gate CI."
}

// Run resolves the markers under the given paths and reports a summary.
func (c *cmdRun) Run(configs *config.Resolver) error {
	launch()
	start := time.Now()
	ctx := context.Background()
	reporter := console.New(ctx, clog.Default)

	filter, err := tagFilter(c.Tags)
	if err != nil {
		return err
	}

	// Only an explicit --deep triggers the confirmation; a configured run.deep or
	// a verify-implied deep proceed without prompting, like --verify.
	if enabled(c.Deep) && !confirmDeep(c.Yes) {
		clog.Info().Msg("Deep lookup cancelled")
		return nil
	}

	// Collect truncated lookups during the run and report them after, so the
	// hints do not interleave with the live progress display.
	var (
		mu        sync.Mutex
		truncated []provider.Truncation
	)
	// The selection toggles resolve per repository root inside the pipeline; only
	// the CLI overrides and the running version (for the per-root required-version
	// gate) pass through here.
	summary, err := mode.Run(ctx, roots(c.Paths), c.DryRun,
		pipeline.WithReporter(reporter),
		pipeline.WithConfig(configs),
		pipeline.WithVersion(clive.Current()),
		pipeline.WithTagFilter(filter),
		pipeline.WithDeep(c.Deep),
		pipeline.WithTruncationSink(func(t provider.Truncation) {
			mu.Lock()
			defer mu.Unlock()
			truncated = append(truncated, t)
		}),
		pipeline.WithDowngrade(c.Downgrade),
		pipeline.WithPrerelease(c.Prerelease),
		pipeline.WithVerify(c.Verify),
		pipeline.WithNoIgnore(c.NoIgnore),
	)
	if err != nil {
		return err
	}
	summary.Elapsed = time.Since(start)

	// Output detail is per-invocation, resolved after the scan: a single-repo
	// scan honours that repo's config, a multi-repo scan the user default.
	detail := configs.Primary().RunOutput(c.Output)
	// GitHub mode emits machine-parseable annotations only; the human hints would
	// be noise in a CI log.
	if detail == output.GitHub {
		report.GitHub(os.Stdout, summary, c.DryRun)
		return runErr(summary)
	}

	reportAuth(ctx, summary)
	reportDeep(truncated)
	report.Run(clog.Default, summary, c.DryRun, detail)
	return runErr(summary)
}

// failuresError is the exit-status error a command returns when its run finished
// but some markers failed. It carries the count so the top-level handler renders
// one friendly summary line; the per-marker errors are already reported.
type failuresError int

func (e failuresError) Error() string { return fmt.Sprintf("%d failed", int(e)) }

// runErr turns a run summary into the command's exit status: a failed marker
// makes `clover run` exit non-zero, so a CI step fails when a directive could
// not be resolved rather than passing on a green-looking log. A skip is not a
// failure - it is a dependency waiting on a failed producer (already counted) or
// a warned unknown key run deliberately tolerates - so it does not set the code.
// The per-marker errors are already reported; this only sets the code.
func runErr(summary mode.Summary) error {
	if summary.Errored() == 0 {
		return nil
	}
	return failuresError(summary.Errored())
}

// reportDeep hints, after a run, that a deeper lookup might help: it warns about
// each lexically-ordered resource whose shallow listing was truncated, so the
// newest version may sit on a later page. Every provider reports truncation (the
// OCI registries and GitHub alike), so this single per-resource warning is the
// only --deep hint; a no-candidate failure explains itself in its own error.
func reportDeep(truncated []provider.Truncation) {
	for _, t := range deepHints(truncated) {
		clog.Warn().
			Link(field.Resource, t.URL, t.Resource).
			Str(field.Hint, "pass --deep").
			Msg("Shallow lookup may have missed newer versions")
	}
}

// deepHints is the pure decision behind reportDeep: the unique truncated
// resources to warn about. Only shallow lookups feed the truncation sink - a
// deep lookup pages to exhaustion - so every collected truncation warrants a
// hint, even when other roots in the same run went deep.
func deepHints(truncated []provider.Truncation) []provider.Truncation {
	return xslices.Unique(truncated)
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
