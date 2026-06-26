package progress_test

import (
	"testing"

	"github.com/gechr/clover/internal/progress"
	"github.com/stretchr/testify/require"
)

func TestNopBeginReturnsInertTasks(t *testing.T) {
	tasks, wait := progress.Nop{}.Begin([]string{"a", "b"})
	require.Len(t, tasks, 2)
	require.NotNil(t, wait)

	// None of the terminal or update calls panic or block.
	for _, task := range tasks {
		task.Update("x")
		task.Done("x")
		task.Fail("x")
		task.Skip("x")
	}
	wait()
}

func TestNopBeginEmpty(t *testing.T) {
	tasks, wait := progress.Nop{}.Begin(nil)
	require.Empty(t, tasks)
	wait()
}
