package mode

import (
	"context"

	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/scan"
)

// Run resolves every marker under roots against its provider and rewrites each
// changed target line in place, atomically and preserving file mode. With dryRun
// set it resolves and renders but writes nothing, so the summary shows what would
// change. The pipeline options forward scan and resolution settings (workers,
// clock, ignore files).
func Run(
	ctx context.Context,
	roots []string,
	dryRun bool,
	opts ...pipeline.Option,
) (Summary, error) {
	files, err := pipeline.Run(ctx, roots, opts...)
	if err != nil {
		return Summary{}, err
	}

	outcomes := make([]Outcome, 0, len(files))
	for _, file := range files {
		if scan.IsSidecar(file.Path) {
			softenSidecarErrors(file.Results)
		}
		outcome := Outcome{FileResult: file}
		if !dryRun && changed(file) {
			outcome.Written, outcome.WriteErr = apply(file)
		}
		outcomes = append(outcomes, outcome)
	}
	return Summary{Outcomes: outcomes}, nil
}

// softenSidecarErrors downgrades a broken sidecar's structural errors to
// skip-with-warning at run: a malformed sidecar should not fail the run the way
// it fails lint, so the run still updates every other marker and merely warns.
// It is applied only to a sidecar's diagnostics File, so a resolved sidecar
// entry's resolution failure stays as hard as the inline equivalent.
func softenSidecarErrors(results []pipeline.Result) {
	for i := range results {
		if r := &results[i]; r.Err != nil {
			r.Skipped, r.Reason, r.Err = true, r.Err.Error(), nil
		}
	}
}

// apply writes the file's rewritten lines back to disk, preserving its mode. A
// write failure is returned rather than aborting the run, so one unwritable file
// never sinks the rest.
func apply(file pipeline.FileResult) (bool, error) {
	if err := writeFile(file.Path, file.Rewritten()); err != nil {
		return false, err
	}
	return true, nil
}

// changed reports whether any of the file's markers rewrote their target line.
func changed(file pipeline.FileResult) bool {
	for _, r := range file.Results {
		if r.Changed {
			return true
		}
	}
	return false
}
