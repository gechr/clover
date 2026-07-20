package command

import (
	"context"
	"os"
	"time"

	"github.com/gechr/clive"
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/console"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/output"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/report"
)

// cmdLint checks every directive resolves, offline and without writing. It is
// the CI gate: a non-zero exit means at least one directive will not resolve.
type cmdLint struct {
	Paths    []string     `name:"path" help:"Files or directories to scan"              arg:"" optional:"" clib:"terse='Paths to scan'"                                                           predictor:"path"`
	Tags     []string     `name:"tag"  help:"Only check directives matching these tags"                    clib:"terse='Filter by tags',complete='predictor=tag,comma',group='Options/Selection'"                  short:"t" aliases:"tags" placeholder:"<tag>"`
	NoIgnore bool         "            help:\"Scan files that `.gitignore` would exclude (VCS directories stay excluded)\"                    clib:\"terse='No ignore',group='Options/Scanning'\""
	Output   *output.Mode "            help:\"Output detail\"                                                clib:\"terse='Output detail',default='text',group='Options/Output'\" short:\"o\"                                                   enum:\"text,wide,github\""
}

// Help returns the detailed blurb shown in `clover lint --help`.
func (c *cmdLint) Help() string {
	return "Checks that every `clover:` directive under the given paths resolves to a version, without writing any changes. Intended as a CI gate: it exits non-zero when a directive cannot be resolved, catching a broken, mistyped, or unreachable reference before it merges."
}

// Run validates the markers under the given paths and fails when any did not.
func (c *cmdLint) Run(configs *config.Resolver, workers parallelism) error {
	launch(false)
	ctx := context.Background()
	start := time.Now()

	filter, err := tagFilter(c.Tags)
	if err != nil {
		return err
	}

	summary, err := mode.Lint(ctx, roots(c.Paths),
		pipeline.WithConfig(configs),
		pipeline.WithNoIgnore(c.NoIgnore),
		pipeline.WithReporter(console.New(ctx, clog.Default())),
		pipeline.WithScanLabel(scanLabelComments),
		pipeline.WithTagFilter(filter),
		pipeline.WithVersion(clive.Current()),
		pipeline.WithWorkers(int(workers)),
	)
	if err != nil {
		return err
	}
	summary.Elapsed = time.Since(start)

	detail := configs.Primary().LintOutput(c.Output)
	if detail == output.GitHub {
		report.GitHub(os.Stdout, summary, false)
	} else {
		report.Lint(clog.Default(), summary, detail)
	}
	if !summary.OK() {
		return failuresError(summary.Errored() + summary.Skipped())
	}
	return nil
}
