package pipeline_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/progress"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// captureReporter records every progress event so a test can assert what the
// engine emitted. It is safe for the executor's concurrent tasks.
type captureReporter struct {
	mu         sync.Mutex
	names      []string
	last       map[string]string // task name -> last terminal event ("done:..", "fail:..", "skip:..")
	trackLabel string
	trackField string
	trackTotal int
	tracked    int  // highest count the tracker observed
	trackStop  bool // whether the tracker was stopped
	waited     bool
}

func newCaptureReporter() *captureReporter {
	return &captureReporter{last: map[string]string{}}
}

func (r *captureReporter) Track(label, field string, total int) progress.Tracker {
	r.mu.Lock()
	r.trackLabel = label
	r.trackField = field
	r.trackTotal = total
	r.mu.Unlock()
	return &captureTracker{reporter: r}
}

func (r *captureReporter) Discovered(int, int, int) {}

func (r *captureReporter) Begin(names []string) ([]progress.Task, func()) {
	r.mu.Lock()
	r.names = names
	r.mu.Unlock()

	tasks := make([]progress.Task, len(names))
	for i, name := range names {
		tasks[i] = &captureTask{reporter: r, name: name}
	}
	return tasks, func() {
		r.mu.Lock()
		r.waited = true
		r.mu.Unlock()
	}
}

func (r *captureReporter) terminal(name string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last[name]
}

type captureTracker struct {
	reporter *captureReporter
}

func (t *captureTracker) Set(n int) {
	t.reporter.mu.Lock()
	defer t.reporter.mu.Unlock()
	t.reporter.tracked = max(t.reporter.tracked, n)
}

func (t *captureTracker) Stop() {
	t.reporter.mu.Lock()
	defer t.reporter.mu.Unlock()
	t.reporter.trackStop = true
}

type captureTask struct {
	reporter *captureReporter
	name     string
}

func (t *captureTask) Update(string) {}

func (t *captureTask) Done(msg string) { t.set("done:" + msg) }
func (t *captureTask) Fail(msg string) { t.set("fail:" + msg) }
func (t *captureTask) Skip(msg string) { t.set("skip:" + msg) }

func (t *captureTask) set(event string) {
	t.reporter.mu.Lock()
	defer t.reporter.mu.Unlock()
	t.reporter.last[t.name] = event
}

func TestRunReportsDoneOnSuccess(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "prog",
		candidates: []model.Candidate{candidate(t, "1.3.0")},
	})
	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=prog repository=x/y\nversion: 1.2.0\n",
	})

	reporter := newCaptureReporter()
	_, err := pipeline.Run(context.Background(), []string{dir}, pipeline.WithReporter(reporter))
	require.NoError(t, err)

	require.Len(t, reporter.names, 1)
	require.True(t, reporter.waited, "Run must call the wait function")
	require.Equal(t, "done:1.3.0", reporter.terminal(reporter.names[0]))
}

func TestRunReportsScanProgress(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "prog",
		candidates: []model.Candidate{candidate(t, "1.3.0")},
	})
	dir := write(t, map[string]string{
		"app.txt":   "# clover: provider=prog repository=x/y\nversion: 1.2.0\n",
		"other.txt": "nothing here\n",
	})

	reporter := newCaptureReporter()
	_, err := pipeline.Run(context.Background(), []string{dir},
		pipeline.WithReporter(reporter),
		pipeline.WithScanLabel("Scanning for Clover comments"),
	)
	require.NoError(t, err)

	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	require.Equal(t, "Scanning for Clover comments", reporter.trackLabel)
	require.Equal(t, field.Scanned, reporter.trackField)
	require.Equal(
		t,
		0,
		reporter.trackTotal,
		"the walk's size is unknown, so it shows an open counter",
	)
	require.GreaterOrEqual(t, reporter.tracked, 2, "every walked file is counted")
	require.True(t, reporter.trackStop, "the tracker must be stopped")
}

func TestRunReportsFailOnError(t *testing.T) {
	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=ghostprog repository=x/y\nversion: 1.0.0\n",
	})

	reporter := newCaptureReporter()
	_, err := pipeline.Run(context.Background(), []string{dir}, pipeline.WithReporter(reporter))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(reporter.terminal(reporter.names[0]), "fail:"))
}

func TestRunReportsSkipOnDanglingFollow(t *testing.T) {
	dir := write(t, map[string]string{
		"app.txt": "# clover: from=ghost value=version\nversion: 1.0.0\n",
	})

	reporter := newCaptureReporter()
	_, err := pipeline.Run(context.Background(), []string{dir}, pipeline.WithReporter(reporter))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(reporter.terminal(reporter.names[0]), "skip:"))
}
