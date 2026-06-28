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

// cmdAnnotate adds clover: directives to lines clover can already track but that
// carry none. It previews by default - a tool that inserts new lines should not
// rewrite the tree unasked - and writes only with --write. --force additionally
// rewrites an existing annotation into its canonical minimal form.
type cmdAnnotate struct {
	Paths    []string `name:"path" help:"Files or directories to scan"                                      arg:"" optional:"" clib:"terse='Paths to scan'"      predictor:"path"`
	Write    bool     `            help:"Apply the proposed annotations (default: preview only)"                               clib:"terse='Write changes'"                       short:"w"`
	Force    bool     `            help:"Rewrite an existing annotation when clover can infer a leaner one"                    clib:"terse='Overwrite existing'"`
	NoIgnore bool     "            help:\"Scan files that `.gitignore` would exclude (VCS directories stay excluded)\"                    clib:\"terse='No ignore'\""
}

// Help returns the detailed blurb shown in `clover annotate --help`.
func (c *cmdAnnotate) Help() string {
	return "Scans for lines clover can already track - GitHub Actions `uses:` pins, container image references - and adds a minimal `clover: provider=auto` directive above each one, the inverse of the auto-detection that later resolves it. " +
		"It previews by default, listing what it would add; pass `--write` to apply. Every annotation is verified offline first, so a line clover cannot actually resolve is left alone. Existing annotations are untouched unless `--force`, which collapses an inferable one back to `provider=auto` (preserving every selection rule) and leaves a deliberately explicit directive alone."
}

// Run previews (or, with --write, applies) the annotations under the paths. It
// exits non-zero only on a real failure - a file that could not be written - so
// a preview, which always writes nothing, is informational and exits zero.
func (c *cmdAnnotate) Run(configs *config.Resolver) error {
	launch()
	ctx := context.Background()

	summary, err := mode.Annotate(
		ctx,
		roots(c.Paths),
		c.Write,
		c.Force,
		configs,
		pipeline.WithVersion(clive.Current()),
		pipeline.WithNoIgnore(c.NoIgnore),
	)
	if err != nil {
		return err
	}

	report.Annotate(clog.Default, summary, c.Write)

	if failed := writeFailures(summary); failed > 0 {
		return fmt.Errorf("%s could not be written", human.Pluralize(failed, "file", "files"))
	}
	return nil
}

// writeFailures counts the files whose annotations could not be written.
func writeFailures(summary mode.AnnotateSummary) int {
	var n int
	for _, file := range summary.Files {
		if file.WriteErr != nil {
			n++
		}
	}
	return n
}
