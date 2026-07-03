package mode

import (
	"time"

	"github.com/gechr/clover/internal/pipeline"
	xslices "github.com/gechr/x/slices"
)

// Outcome pairs a file's resolution results with what the mode did to it on
// disk: whether it was rewritten, and any error from that write. A mode that
// writes nothing (lint) leaves Written false and WriteErr nil.
type Outcome struct {
	pipeline.FileResult

	Written  bool
	WriteErr error
}

// Summary is everything a mode produced over its roots, in file order, ready for
// the reporter to render.
type Summary struct {
	Outcomes []Outcome
	Elapsed  time.Duration // wall-clock time the run took, for the summary line
}

// Changed reports the number of markers whose target line was rewritten.
func (s Summary) Changed() int { return s.count(func(r pipeline.Result) bool { return r.Changed }) }

// Skipped reports the number of markers skipped because a dependency failed.
func (s Summary) Skipped() int { return s.count(func(r pipeline.Result) bool { return r.Skipped }) }

// Disabled reports the number of markers a disabled= directive intentionally
// disabled. They are inert by design, so they never count toward lint failure.
func (s Summary) Disabled() int {
	return s.count(func(r pipeline.Result) bool { return r.Disabled })
}

// Errored reports the number of markers that failed to resolve.
func (s Summary) Errored() int {
	return s.count(func(r pipeline.Result) bool { return r.Err != nil })
}

// count tallies the markers across every outcome that satisfy pred.
func (s Summary) count(pred func(pipeline.Result) bool) int {
	var n int
	for _, o := range s.Outcomes {
		n += xslices.CountFunc(o.Results, pred)
	}
	return n
}
