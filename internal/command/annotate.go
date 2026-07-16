package command

import (
	"cmp"
	"context"
	"fmt"
	"time"

	"github.com/gechr/clive"
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/console"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/report"
	"github.com/gechr/x/human"
	"github.com/gechr/x/ptr"
	xslices "github.com/gechr/x/slices"
)

// cmdAnnotate adds clover: directives to lines Clover can already track but that
// carry none. It previews by default - a tool that inserts new lines should not
// rewrite the tree unasked - and writes only with --write. --check is the CI gate:
// it previews and exits non-zero when anything would change. --force additionally
// rewrites an existing annotation into its canonical minimal form.
type cmdAnnotate struct {
	Paths    []string `name:"path" help:"Files or directories to scan"                                            arg:"" optional:"" clib:"terse='Paths to scan'"                                predictor:"path"`
	DryRun   *bool    `            help:"Preview the proposed annotations without writing (default)"                                 clib:"terse='Dry run',group='Options/Mode'"                                  short:"n" aliases:"dry" xor:"write"`
	Write    *bool    `            help:"Apply the proposed annotations"                                                             clib:"terse='Write changes',group='Options/Mode'"                            short:"w"               xor:"write"`
	Check    *bool    `            help:"Report annotations that would be added and exit non-zero (do not write)"                    clib:"terse='Check only',group='Options/Mode'"`
	Force    bool     `            help:"Rewrite an existing annotation when Clover can infer a leaner one"                          clib:"terse='Overwrite existing',group='Options/Selection'"`
	NoIgnore bool     "            help:\"Scan files that `.gitignore` would exclude (VCS directories stay excluded)\"                    clib:\"terse='No ignore',group='Options/Scanning'\""
	Sidecar  *bool    "            help:\"Propose inline comments only, never generating a sidecar for a comment-less target\"                clib:\"terse='Sidecars',group='Options/Selection',negative\"     negatable:\"\""
}

// Help returns the detailed blurb shown in `clover annotate --help`.
func (c *cmdAnnotate) Help() string {
	return "Adds `@clover` (shorthand for `clover: provider=auto`) above lines Clover can already track. For example, GitHub Actions `uses:` pins and container image references can be annotated automatically. This is the inverse of the auto-detection that later resolves them. " +
		"Every annotation is verified offline first. Unresolvable lines are left untouched.\n\n" +
		"It previews by default, listing what it would add. Pass `--dry-run` to request that mode explicitly, `--write` to apply, or `--check` to fail when anything would be annotated.\n\n" +
		"Existing annotations are untouched unless `--force`, which collapses an inferable one back to `@clover` (preserving every selection rule) and leaves a deliberately explicit directive alone.\n\n" +
		"A comment-less target (a strict-JSON file, or a pyenv `.python-version`) earns a sidecar instead of an inline comment. Pass `--no-sidecar` (or set `annotate.sidecar: false`) to opt out, leaving such targets untouched."
}

// Run previews (or, with --write, applies) the annotations under the paths.
// --check previews without writing and exits non-zero when annotate found work,
// making it suitable as a CI gate.
func (c *cmdAnnotate) Run(configs *config.Resolver, workers parallelism) error {
	launch(false)
	ctx := context.Background()
	start := time.Now()
	cfg, err := configs.PrimaryForPaths(roots(c.Paths))
	if err != nil {
		return err
	}
	write, check := c.mode(cfg)

	reporter := console.New(ctx, clog.Default)
	summary, err := mode.Annotate(
		ctx,
		roots(c.Paths),
		write,
		c.Force,
		c.sidecar(cfg),
		configs,
		reporter,
		int(workers),
		pipeline.WithNoIgnore(c.NoIgnore),
		pipeline.WithReporter(reporter),
		pipeline.WithScanLabel(scanLabelCandidates),
		pipeline.WithVersion(clive.Current()),
		pipeline.WithWorkers(int(workers)),
	)
	if err != nil {
		return err
	}
	summary.Elapsed = time.Since(start)

	annotateDiscovered(summary)
	report.Annotate(clog.Default, summary, write)

	if failed := writeFailures(summary); failed > 0 {
		return fmt.Errorf("%s could not be written", human.Pluralize(failed, "file", "files"))
	}
	if check && summary.Total() > 0 {
		return fmt.Errorf(
			"%s found",
			human.Pluralize(summary.Total(), "annotation candidate", "annotation candidates"),
		)
	}
	return nil
}

// mode resolves annotate's preview/write/check mode. Explicit CLI mode flags
// win as a group, then annotate.check, then annotate.write, then preview.
func (c *cmdAnnotate) mode(cfg *config.Config) (bool, bool) {
	switch {
	case ptr.Deref(c.Check):
		return false, true
	case ptr.Deref(c.DryRun):
		return false, false
	case ptr.Deref(c.Write):
		return true, false
	case ptr.Deref(cfg.AnnotateCheck()):
		return false, true
	case ptr.Deref(cfg.AnnotateWrite()):
		return true, false
	default:
		return false, false
	}
}

// sidecar resolves whether comment-less targets earn generated sidecars: an
// explicit --[no-]sidecar flag wins, then annotate.sidecar, then enabled.
func (c *cmdAnnotate) sidecar(cfg *config.Config) bool {
	if v := cmp.Or(c.Sidecar, cfg.AnnotateSidecar()); v != nil {
		return *v
	}
	return true
}

// annotateDiscovered logs the scan result that supplants the transient scan
// line: the candidate lines annotate found, a notice that every recognized line
// is already annotated, or a warning when it found nothing trackable at all.
func annotateDiscovered(summary mode.AnnotateSummary) {
	candidates := summary.Total()
	if candidates == 0 {
		if annotated := summary.Annotated(); annotated > 0 {
			clog.Info().
				Symbol("🍃").
				Int(field.Scanned, summary.Scanned).
				Int(field.Annotated, annotated).
				Msg("Every trackable line is already annotated")
			return
		}
		clog.Warn().
			Symbol("🫠").
			Int(field.Scanned, summary.Scanned).
			Msg("No Clover annotation candidates found")
		return
	}
	files := xslices.CountFunc(summary.Files, func(f mode.AnnotateFile) bool {
		return len(f.Changes) > 0 || (f.Sidecar != nil && len(f.Sidecar.Entries) > 0)
	})
	clog.Info().
		Symbol("💬").
		Int(field.Scanned, summary.Scanned).
		Int(field.Files, files).
		Int(field.Candidates, candidates).
		Msg("Found Clover annotation candidates")
}

// writeFailures counts the files whose annotations could not be written.
func writeFailures(summary mode.AnnotateSummary) int {
	return xslices.CountFunc(summary.Files, func(f mode.AnnotateFile) bool {
		return f.WriteErr != nil
	})
}
