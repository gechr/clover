package command

import (
	"context"
	"fmt"

	"github.com/gechr/clive"
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/report"
	"github.com/gechr/x/human"
)

// cmdFormat canonicalises directive comments. With --check it reports what would
// change and exits non-zero without writing - the formatting CI gate; --dry-run
// previews the same rewrites but exits zero.
type cmdFormat struct {
	Paths    []string `name:"path" help:"Files or directories to scan"                                             arg:"" optional:"" clib:"terse='Paths to scan'"      predictor:"path"`
	Check    bool     `            help:"Report directives that need formatting and exit non-zero (do not write)"                     clib:"terse='Check only'"`
	DryRun   bool     `            help:"Report what would be reformatted without writing"                                            clib:"terse='Dry run'"                             short:"n" aliases:"dry"`
	NoIgnore bool     `            help:"Scan files that .gitignore would exclude (VCS directories stay excluded)"                    clib:"terse='No ignore'"`
	Prune    *bool    `            help:"Remove unknown keys instead of erroring on them"                                             clib:"terse='Prune unknown keys'"                                          negatable:""`
}

// Help returns the detailed blurb shown in `clover format --help`.
func (c *cmdFormat) Help() string {
	return "Rewrites `clover:` directive comments into their canonical form - normalizing key order and spacing - so annotations stay consistent across the codebase. With `--check` it reports which directives need formatting and exits non-zero without writing; " +
		"`--dry-run` previews the same rewrites but exits zero. An unknown key fails the run unless `--prune` removes it."
}

// Run canonicalises (or, with --check/--dry-run, previews) the directives under
// the paths. Both --check and --dry-run suppress writing; only --check escalates
// pending changes to a non-zero exit.
func (c *cmdFormat) Run(configs *config.Resolver) error {
	launch()
	ctx := context.Background()

	dry := c.Check || c.DryRun
	summary, err := mode.Format(
		ctx,
		roots(c.Paths),
		dry,
		c.Prune,
		configs,
		pipeline.WithVersion(clive.Current()),
		pipeline.WithNoIgnore(c.NoIgnore),
	)
	if err != nil {
		return err
	}

	report.Format(clog.Default, summary, dry)
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
