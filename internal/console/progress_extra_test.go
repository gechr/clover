package console_test

import (
	"bytes"
	"strconv"
	"sync"
	"testing"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/console"
	"github.com/stretchr/testify/require"
)

// TestLineAdvanceIsConcurrencySafe drives every task from its own goroutine, as
// the parallel resolve wave does. The tasks share one clog update, which is not
// safe for concurrent mutation, so an unguarded advance races on its field
// slice and panics inside the field builder.
func TestLineAdvanceIsConcurrencySafe(t *testing.T) {
	const total = 64

	var buf bytes.Buffer
	reporter := console.New(t.Context(), clog.NewWriter(&buf))

	names := make([]string, total)
	for i := range names {
		names[i] = "a:" + strconv.Itoa(i)
	}
	tasks, wait := reporter.Begin(names)
	require.Len(t, tasks, total)

	var wg sync.WaitGroup
	wg.Add(total)
	for _, task := range tasks {
		go func() {
			defer wg.Done()
			task.Done("1.0.0")
		}()
	}
	wg.Wait()

	wait()
	require.Empty(t, buf.String(), "off a TTY the aggregated line is suppressed")
}

// TestLineUpdateIsNoop confirms that the aggregated progress line's Update is a
// deliberate no-op: it carries no per-marker detail into the rendered output,
// and driving a task through Update then Done off a TTY renders nothing.
func TestLineUpdateIsNoop(t *testing.T) {
	var buf bytes.Buffer
	reporter := console.New(t.Context(), clog.NewWriter(&buf))

	tasks, wait := reporter.Begin([]string{"a:1"})
	require.Len(t, tasks, 1)

	tasks[0].Update("resolving detail")
	tasks[0].Done("1.0.0")

	wait()
	require.Empty(
		t,
		buf.String(),
		"off a TTY the aggregated line, including Update detail, is suppressed",
	)
}
