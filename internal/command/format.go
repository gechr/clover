package command

import (
	"context"
	"fmt"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/report"
	"github.com/gechr/x/human"
)

// formatCmd canonicalises directive comments. With --check it reports what would
// change and exits non-zero without writing - the formatting CI gate.
type formatCmd struct {
	Paths []string `arg:"" optional:"" name:"path" help:"Files or directories to scan (default: current directory)."              predictor:"path"`
	Check bool     `                               help:"Report directives that need formatting and exit non-zero; do not write."`
}

// Run canonicalises (or, with --check, checks) the directives under the paths.
func (c *formatCmd) Run() error {
	ctx := context.Background()

	summary, err := mode.Format(ctx, roots(c.Paths), c.Check)
	if err != nil {
		return err
	}

	report.Format(clog.Default, summary, c.Check)
	if c.Check && !summary.OK() {
		return fmt.Errorf(
			"%s would be reformatted",
			human.Pluralize(summary.Changed(), "directive", "directives"),
		)
	}
	return nil
}
