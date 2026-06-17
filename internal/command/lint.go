package command

import (
	"context"
	"fmt"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/mode"
)

// lintCmd checks every directive resolves, offline and without writing. It is
// the CI gate: a non-zero exit means at least one directive will not resolve.
type lintCmd struct {
	Paths []string `arg:"" optional:"" name:"path" help:"Files or directories to scan (default: current directory)." predictor:"path"`
}

// Run validates the markers under the given paths and fails when any did not.
func (c *lintCmd) Run() error {
	ctx := context.Background()

	summary, err := mode.Lint(ctx, roots(c.Paths))
	if err != nil {
		return err
	}

	clog.Info().
		Int("errored", summary.Errored()).
		Int("skipped", summary.Skipped()).
		Msg("Lint complete")
	if !summary.OK() {
		return fmt.Errorf("%d errored, %d skipped", summary.Errored(), summary.Skipped())
	}
	return nil
}
