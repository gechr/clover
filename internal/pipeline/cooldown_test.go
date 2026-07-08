package pipeline_test

import (
	"context"
	"testing"
	"time"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// countingProvider records how many times Discover is called, so a test can prove
// the cooldown short-circuit skipped the fetch. It embeds fakeProvider and so is
// not a Dater.
type countingProvider struct {
	fakeProvider

	calls *int
}

func (p countingProvider) Discover(
	ctx context.Context,
	r provider.Resource,
) ([]model.Candidate, error) {
	*p.calls++
	return p.fakeProvider.Discover(ctx, r)
}

// datedProvider is a countingProvider that declares the Dater capability, so the
// cooldown short-circuit lets it through to Discover.
type datedProvider struct{ countingProvider }

func (datedProvider) Dated() {}

// A cooldown on a source that carries no publication dates can never be honored.
// An undated provider (no Dater) is skipped before Discover, saving the fetch,
// while a Dater still fetches and falls to the post-discovery date check - both
// skip with the same reason.
func TestRunCooldownShortCircuitsUndatedProvider(t *testing.T) {
	now := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	const reason = "cooldown not supported - source does not provide publication dates"

	t.Run("undated provider skips before discovery", func(t *testing.T) {
		var calls int
		provider.Register(countingProvider{
			fakeProvider: fakeProvider{
				name:       "undated",
				candidates: []model.Candidate{candidate(t, "1.3.0")},
			},
			calls: &calls,
		})

		dir := write(t, map[string]string{
			"app.txt": "# clover: provider=undated repository=x/y cooldown=7d\nversion: 1.2.0\n",
		})
		files, err := pipeline.Run(context.Background(), []string{dir}, pipeline.WithNow(now))
		require.NoError(t, err)

		r := files[0].Results[0]
		require.True(t, r.Skipped)
		require.Equal(t, reason, r.Reason)
		require.Zero(t, calls, "the fetch is skipped when the source can never date candidates")
	})

	t.Run("dater fetches then skips on undated candidates", func(t *testing.T) {
		var calls int
		provider.Register(datedProvider{countingProvider{
			fakeProvider: fakeProvider{
				name:       "dated",
				candidates: []model.Candidate{candidate(t, "1.3.0")},
			},
			calls: &calls,
		}})

		dir := write(t, map[string]string{
			"app.txt": "# clover: provider=dated repository=x/y cooldown=7d\nversion: 1.2.0\n",
		})
		files, err := pipeline.Run(context.Background(), []string{dir}, pipeline.WithNow(now))
		require.NoError(t, err)

		r := files[0].Results[0]
		require.True(t, r.Skipped)
		require.Equal(t, reason, r.Reason)
		require.Equal(t, 1, calls, "a Dater still fetches and falls to the post-discovery check")
	})
}
