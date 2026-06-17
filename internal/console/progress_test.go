package console_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/console"
	"github.com/stretchr/testify/require"
)

// TestReporterAggregatesTasks drives every task to a terminal state and confirms
// the three markers collapse to one progress line reading 3/3. Off a TTY clog
// emits the final state as a single line, so the rendered output is exact.
func TestReporterAggregatesTasks(t *testing.T) {
	var buf bytes.Buffer
	reporter := console.New(context.Background(), clog.NewWriter(&buf))

	tasks, wait := reporter.Begin([]string{"a:1", "b:2", "c:3"})
	require.Len(t, tasks, 3)

	tasks[0].Done("1.0.0")
	tasks[1].Fail("boom")
	tasks[2].Skip("dependency failed")

	wait()
	require.Equal(t, "INF ⏳ Resolving progress=3/3\n", buf.String())
}

func TestReporterEmptyIsNoop(t *testing.T) {
	var buf bytes.Buffer
	reporter := console.New(context.Background(), clog.NewWriter(&buf))

	tasks, wait := reporter.Begin(nil)
	require.Empty(t, tasks)
	wait() // must not block
	require.Empty(t, strings.TrimSpace(buf.String()))
}
