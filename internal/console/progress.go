// Package console renders engine progress at the CLI edge. It implements the
// clog-free [progress.Reporter] seam with a clog live display, so the pipeline
// and pure core stay free of any terminal dependency.
package console

import (
	"context"
	"sync/atomic"

	"github.com/gechr/clog"
	"github.com/gechr/clog/fx"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/progress"
)

// Reporter renders overall resolution progress as a single live line carrying a
// progress=done/total field. On a TTY clog updates the line in place; off a TTY
// it falls back to plain line logging. The per-marker detail the seam reports is
// aggregated into the one counter for now; a richer per-marker view can be added
// later without changing the seam.
type Reporter struct {
	ctx    context.Context //nolint:containedctx // the reporter drives a background render loop bound to this ctx
	logger *clog.Logger
}

// New returns a Reporter rendering through logger, which the CLI configures
// (typically clog.Default writing to stderr).
func New(ctx context.Context, logger *clog.Logger) *Reporter {
	return &Reporter{ctx: ctx, logger: logger}
}

// Discovered logs the scan totals before resolution begins, or warns when no
// Clover comments were found at all.
func (r *Reporter) Discovered(scanned, files, comments int) {
	if comments == 0 {
		r.logger.Warn().Symbol("💔").Int(field.Scanned, scanned).Msg("No Clover comments found")
		return
	}
	r.logger.Info().
		Symbol("💬").
		Int(field.Files, files).
		Int(field.Comments, comments).
		Msg("Discovered Clover comments")
}

// Begin starts one progress line totalling len(names) and returns a task per
// name, every one advancing the same counter as it reaches a terminal state.
// The returned wait blocks until the line has finished rendering.
func (r *Reporter) Begin(names []string) ([]progress.Task, func()) {
	total := len(names)
	if total == 0 {
		return nil, func() {}
	}

	group := r.logger.Group(r.ctx)
	update, finish := group.Add(r.logger.Spinner("Resolving")).Manual()
	update.Fraction("progress", 0, total).Send()

	shared := &line{update: update, finish: finish, total: total}
	tasks := make([]progress.Task, total)
	for i := range tasks {
		tasks[i] = shared
	}

	rendered := make(chan struct{})
	go func() {
		defer close(rendered)
		_ = group.Wait().Silent()
	}()
	return tasks, func() { <-rendered }
}

// line is the single overall progress line. Every marker's terminal event
// advances its counter; the last one finishes the underlying clog task.
type line struct {
	update *fx.Update
	finish func(error)
	total  int
	done   atomic.Int64
}

// Update is a no-op: the overall view shows only the running total, not
// per-marker detail.
func (l *line) Update(string) {}

func (l *line) Done(string) { l.advance() }
func (l *line) Fail(string) { l.advance() }
func (l *line) Skip(string) { l.advance() }

// advance records one finished marker and refreshes the progress field,
// finishing the line once every marker has reported.
func (l *line) advance() {
	n := int(l.done.Add(1))
	l.update.Fraction("progress", n, l.total).Send()
	if n >= l.total {
		l.finish(nil)
	}
}
