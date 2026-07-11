package progress_test

import (
	"testing"

	"github.com/gechr/clover/internal/progress"
	"github.com/stretchr/testify/require"
)

// TestNopTrack drives the inert tracker through its full lifecycle, confirming
// Track/Set/Stop never panic or block.
func TestNopTrack(t *testing.T) {
	t.Parallel()

	tr := progress.Nop{}.Track("label", "field", 0)
	require.NotNil(t, tr)
	tr.Set(1)
	tr.Set(42)
	tr.Stop()
}

// TestNopDiscovered confirms the inert Discovered reports nothing without panic.
func TestNopDiscovered(t *testing.T) {
	t.Parallel()
	progress.Nop{}.Discovered(1, 2, 3)
}

// TestNopTaskLifecycle drives a task from Nop.Begin through every event,
// confirming Update and each terminal call is an inert no-op.
func TestNopTaskLifecycle(t *testing.T) {
	t.Parallel()

	tasks, wait := progress.Nop{}.Begin([]string{"a"})
	require.Len(t, tasks, 1)
	require.NotNil(t, wait)

	task := tasks[0]
	task.Update("x")
	task.Done("x")
	task.Fail("x")
	task.Skip("x")
	wait()
}
