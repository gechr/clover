package registry_test

import (
	"strconv"
	"sync"
	"testing"

	"github.com/gechr/cusp/internal/model"
	"github.com/gechr/cusp/internal/registry"
	"github.com/stretchr/testify/require"
)

func TestSetGet(t *testing.T) {
	t.Parallel()

	reg := registry.New()

	_, ok := reg.Get("missing")
	require.False(t, ok)

	reg.Set("tool", registry.Entry{
		Old: model.Candidate{Version: "1.2.0"},
		New: model.Candidate{Version: "1.2.3", Commit: "abc"},
	})
	got, ok := reg.Get("tool")
	require.True(t, ok)
	require.Equal(t, "1.2.0", got.Old.Version)
	require.Equal(t, "1.2.3", got.New.Version)
	require.Equal(t, "abc", got.New.Commit)
}

func TestConcurrentSet(t *testing.T) {
	t.Parallel()

	reg := registry.New()

	const writers = 50
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := range writers {
		go func() {
			defer wg.Done()
			reg.Set(
				"id"+strconv.Itoa(i),
				registry.Entry{New: model.Candidate{Version: strconv.Itoa(i)}},
			)
		}()
	}
	wg.Wait()

	for i := range writers {
		got, ok := reg.Get("id" + strconv.Itoa(i))
		require.True(t, ok)
		require.Equal(t, strconv.Itoa(i), got.New.Version)
	}
}
