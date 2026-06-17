package exec_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/gechr/cusp/internal/exec"
	"github.com/stretchr/testify/require"
)

// recorder collects the order tasks ran in, concurrency-safe.
type recorder struct {
	mu  sync.Mutex
	ran []string
}

func (r *recorder) task(id, from string, err error) exec.Task {
	return exec.Task{
		ID:   id,
		From: from,
		Run: func(context.Context) error {
			r.mu.Lock()
			r.ran = append(r.ran, id)
			r.mu.Unlock()
			return err
		},
	}
}

func (r *recorder) order() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ran...)
}

func byID(results []exec.Result) map[string]exec.Result {
	m := make(map[string]exec.Result, len(results))
	for _, res := range results {
		m[res.ID] = res
	}
	return m
}

func TestIndependentAllRun(t *testing.T) {
	t.Parallel()

	var rec recorder
	results := exec.Execute(t.Context(), []exec.Task{
		rec.task("a", "", nil),
		rec.task("b", "", nil),
		rec.task("c", "", nil),
	}, 4)

	require.Len(t, rec.order(), 3)
	for _, res := range results {
		require.NoError(t, res.Err)
		require.False(t, res.Skipped)
	}
}

func TestChainRunsInOrder(t *testing.T) {
	t.Parallel()

	var rec recorder
	exec.Execute(t.Context(), []exec.Task{
		rec.task("c", "b", nil),
		rec.task("b", "a", nil),
		rec.task("a", "", nil),
	}, 4)

	require.Equal(t, []string{"a", "b", "c"}, rec.order())
}

func TestProducerErrorSkipsFollowers(t *testing.T) {
	t.Parallel()

	var rec recorder
	results := byID(exec.Execute(t.Context(), []exec.Task{
		rec.task("a", "", errors.New("boom")),
		rec.task("b", "a", nil),
		rec.task("c", "b", nil),
		rec.task("d", "", nil), // independent, unaffected
	}, 4))

	require.Error(t, results["a"].Err)
	require.True(t, results["b"].Skipped)
	require.True(t, results["c"].Skipped, "skip cascades down the chain")
	require.NoError(t, results["d"].Err)
	require.NotContains(t, rec.order(), "b")
	require.NotContains(t, rec.order(), "c")
}

func TestCycleSkipped(t *testing.T) {
	t.Parallel()

	var rec recorder
	results := byID(exec.Execute(t.Context(), []exec.Task{
		rec.task("a", "b", nil),
		rec.task("b", "a", nil),
	}, 4))

	require.True(t, results["a"].Skipped)
	require.True(t, results["b"].Skipped)
	require.Empty(t, rec.order())
}

func TestUnknownFromSkipped(t *testing.T) {
	t.Parallel()

	var rec recorder
	results := byID(exec.Execute(t.Context(), []exec.Task{
		rec.task("a", "ghost", nil),
		rec.task("b", "", nil),
	}, 4))

	require.True(t, results["a"].Skipped)
	require.Contains(t, results["a"].Reason, "ghost")
	require.NoError(t, results["b"].Err)
}

func TestEmpty(t *testing.T) {
	t.Parallel()

	require.Empty(t, exec.Execute(t.Context(), nil, 4))
}
