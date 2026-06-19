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

// TestReporterDiscovered confirms the scan-total line: the discovery counts
// when comments were found, and the warning (with the scanned count) when none
// were.
func TestReporterDiscovered(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		var buf bytes.Buffer
		console.New(context.Background(), clog.NewWriter(&buf)).Discovered(12, 3, 7)
		require.Equal(t, "INF 💬 Discovered Clover comments files=3 comments=7\n", buf.String())
	})

	t.Run("none", func(t *testing.T) {
		var buf bytes.Buffer
		console.New(context.Background(), clog.NewWriter(&buf)).Discovered(10, 0, 0)
		require.Equal(t, "WRN 🫥 No Clover comments found scanned=10\n", buf.String())
	})
}

func TestReporterEmptyIsNoop(t *testing.T) {
	var buf bytes.Buffer
	reporter := console.New(context.Background(), clog.NewWriter(&buf))

	tasks, wait := reporter.Begin(nil)
	require.Empty(t, tasks)
	wait() // must not block
	require.Empty(t, strings.TrimSpace(buf.String()))
}
