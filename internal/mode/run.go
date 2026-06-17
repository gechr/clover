package mode

import (
	"context"
	"os"
	"strings"

	"github.com/gechr/cusp/internal/pipeline"
	xos "github.com/gechr/x/os"
)

// defaultPerm is the file mode used when an existing file's mode cannot be read,
// so a rewrite still produces a sensibly-permissioned file.
const defaultPerm os.FileMode = 0o644

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
		outcome := Outcome{FileResult: file}
		if !dryRun && changed(file) {
			outcome.Written, outcome.WriteErr = apply(file)
		}
		outcomes = append(outcomes, outcome)
	}
	return Summary{Outcomes: outcomes}, nil
}

// apply writes the file's rewritten lines back to disk, preserving its mode. A
// write failure is returned rather than aborting the run, so one unwritable file
// never sinks the rest.
func apply(file pipeline.FileResult) (bool, error) {
	data := strings.Join(file.Rewritten(), "\n")
	if err := xos.AtomicWrite(file.Path, []byte(data), perm(file.Path)); err != nil {
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

// perm returns path's current file mode, falling back to defaultPerm when it
// cannot be read.
func perm(path string) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return defaultPerm
}
