// Package console renders engine progress at the CLI edge. It implements the
// clog-free [progress.Reporter] seam with a clog live display, so the pipeline
// and pure core stay free of any terminal dependency.
package console

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/gechr/clog"
	"github.com/gechr/clog/fx"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/progress"
)

const (
	// trackInterval is how often a live progress line refreshes its count.
	trackInterval = 100 * time.Millisecond
	// trackRenderDelay suppresses a progress line until the work has run this
	// long, so a quick task on a small tree never flashes a spinner.
	trackRenderDelay = 300 * time.Millisecond
)

// Reporter renders overall resolution progress as a single live line carrying a
// progress=done/total field. On a TTY clog updates the line in place; off a TTY
// (CI, pipes) it is suppressed entirely, since a progress line that never
// animates is just noise there. The per-marker detail the seam reports is
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

// Track starts the transient progress line labelled label and returns a handle
// the work drives with its running count. On a TTY clog animates a spinner
// carrying the live count - an open field=n when total is zero, a field=n/total
// fraction (with the resolve line's gradient) when it is positive - erased when
// the handle stops so the next log line supplants it; off a TTY (NonTTYSilent) it
// renders nothing.
func (r *Reporter) Track(label, field string, total int) progress.Tracker {
	group := r.logger.Group(r.ctx, fx.WithRenderDelay(trackRenderDelay))
	update, finish := group.Add(
		r.logger.Spinner(label).NonTTYSilent(true),
	).Manual()

	rendered := make(chan struct{})
	go func() {
		defer close(rendered)
		_ = group.Wait().Silent()
	}()

	t := &tracker{
		update:   update,
		finish:   finish,
		rendered: rendered,
		field:    field,
		total:    total,
		stop:     make(chan struct{}),
		runDone:  make(chan struct{}),
	}
	go t.run()
	return t
}

// tracker is a transient progress line. The work reports its running count
// through Set (lock-free, into an atomic); a single render goroutine reads it on
// a ticker and refreshes the count field, so the shared Update - which is not
// safe for concurrent mutation - is only ever touched by one goroutine.
type tracker struct {
	update   *fx.Update
	finish   func(error)
	rendered <-chan struct{}
	field    string
	total    int
	stop     chan struct{}
	runDone  chan struct{}
	count    atomic.Int64
	last     int64
}

// Set records the running count so far.
func (t *tracker) Set(n int) { t.count.Store(int64(n)) }

// run refreshes the line on a ticker until Stop closes stop, batching the work's
// increments into one render-rate update.
func (t *tracker) run() {
	defer close(t.runDone)
	ticker := time.NewTicker(trackInterval)
	defer ticker.Stop()
	for {
		select {
		case <-t.stop:
			return
		case <-ticker.C:
			t.flush()
		}
	}
}

// flush pushes the latest count to the line, skipping a redundant send when the
// count has not advanced since the last tick. A known total renders a fraction;
// an unknown one (zero) renders an open counter.
func (t *tracker) flush() {
	n := t.count.Load()
	if n == t.last {
		return
	}
	t.last = n
	if t.total > 0 {
		t.update.Fraction(t.field, int(n), t.total).Send()
	} else {
		t.update.Int(t.field, int(n)).Send()
	}
}

// Stop ends the line. It waits for the render goroutine to exit before the final
// flush, so flush stays single-goroutine, then finishes the task and blocks
// until rendering has drained.
func (t *tracker) Stop() {
	close(t.stop)
	<-t.runDone
	t.flush()
	t.finish(nil)
	<-t.rendered
}

// Discovered logs the scan totals before resolution begins, or warns when no
// Clover comments were found at all.
func (r *Reporter) Discovered(scanned, files, comments int) {
	if comments == 0 {
		r.logger.Warn().Symbol("🫠").Int(field.Scanned, scanned).Msg("No Clover comments found")
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
	update, finish := group.Add(
		r.logger.Spinner("Checking for updates").
			NonTTYSilent(true),
	).Manual()
	update.Fraction(field.Progress, 0, total).Send()

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
	l.update.Fraction(field.Progress, n, l.total).Send()
	if n >= l.total {
		l.finish(nil)
	}
}
