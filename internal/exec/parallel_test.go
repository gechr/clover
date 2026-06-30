package exec_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gechr/clover/internal/exec"
	"github.com/stretchr/testify/require"
)

// TestParallelRunsEveryIndexOnce confirms Parallel invokes fn exactly once for
// each index in [0, n), the contract the indexed-result call sites rely on.
func TestParallelRunsEveryIndexOnce(t *testing.T) {
	t.Parallel()

	const n = 1000
	seen := make([]atomic.Int64, n)
	exec.Parallel(8, n, func(i int) { seen[i].Add(1) })

	for i := range n {
		require.Equal(t, int64(1), seen[i].Load(), "index %d ran exactly once", i)
	}
}

// TestParallelBoundsConcurrency confirms no more than workers calls are ever in
// flight at once.
func TestParallelBoundsConcurrency(t *testing.T) {
	t.Parallel()

	const workers = 4
	var inFlight, peak atomic.Int64
	exec.Parallel(workers, 200, func(int) {
		cur := inFlight.Add(1)
		for { // record the high-water mark
			old := peak.Load()
			if cur <= old || peak.CompareAndSwap(old, cur) {
				break
			}
		}
		inFlight.Add(-1)
	})

	require.LessOrEqual(t, peak.Load(), int64(workers), "never exceeds the worker cap")
	require.Positive(t, peak.Load())
}

// TestParallelClampsAndHandlesEmpty confirms workers < 1 runs serially rather
// than deadlocking on an unbuffered slot channel, and n == 0 is a no-op.
func TestParallelClampsAndHandlesEmpty(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	exec.Parallel(0, 5, func(int) { calls.Add(1) })
	require.Equal(t, int64(5), calls.Load(), "workers < 1 still runs every index")

	var none sync.Once
	ran := false
	exec.Parallel(4, 0, func(int) { none.Do(func() { ran = true }) })
	require.False(t, ran, "n == 0 invokes fn never")
}
