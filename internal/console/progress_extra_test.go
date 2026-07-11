package console_test

import (
	"bytes"
	"testing"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/console"
	"github.com/stretchr/testify/require"
)

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
