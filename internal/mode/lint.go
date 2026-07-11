package mode

import (
	"context"

	"github.com/gechr/clover/internal/pipeline"
	xslices "github.com/gechr/x/slices"
)

// Lint validates every marker under roots offline - no network, no writes -
// and reports each one's problems. It is the CI gate: a clean summary proves
// every directive will resolve, an errored or skipped marker proves it will not.
func Lint(ctx context.Context, roots []string, opts ...pipeline.Option) (Summary, error) {
	files, err := pipeline.Validate(ctx, roots, opts...)
	if err != nil {
		return Summary{}, err
	}

	outcomes := xslices.Map(files, func(file pipeline.FileResult) Outcome {
		return Outcome{FileResult: file}
	})
	return Summary{Outcomes: outcomes}, nil
}

// OK reports whether every marker validated cleanly - nothing errored or
// skipped. It is the signal a CI gate exits on.
func (s Summary) OK() bool { return s.Errored() == 0 && s.Skipped() == 0 }
