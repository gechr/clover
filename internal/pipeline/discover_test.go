package pipeline_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// memoProvider counts Discover calls atomically, so a test can prove duplicate
// lookups coalesced into one fetch - or stayed apart across differing hints,
// where the calls run concurrently.
type memoProvider struct {
	fakeProvider

	calls *atomic.Int32
}

func (p memoProvider) Discover(
	ctx context.Context,
	r provider.Resource,
) ([]model.Candidate, error) {
	p.calls.Add(1)
	return p.fakeProvider.Discover(ctx, r)
}

// memoRecencyProvider is a memoProvider that lists newest-first, so truncation
// feeds the per-marker flag rather than the run-wide blanket sink.
type memoRecencyProvider struct{ memoProvider }

func (memoRecencyProvider) RecencyOrdered() {}

// Two markers naming the same effective lookup share one Discover call, and
// both resolve from the shared candidates.
func TestRunDuplicateLookupsCoalesce(t *testing.T) {
	var calls atomic.Int32
	provider.Register(memoProvider{
		fakeProvider: fakeProvider{
			name:       "memodup",
			candidates: []model.Candidate{candidate(t, "1.3.0")},
		},
		calls: &calls,
	})

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=memodup repository=x/y\nversion: 1.2.0\n",
		"b.txt": "# clover: provider=memodup repository=x/y\nversion: 1.2.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, int32(1), calls.Load(), "duplicate lookups share one fetch")
	for _, f := range files {
		require.NoError(t, f.Results[0].Err)
		require.Equal(t, "version: 1.3.0", f.Results[0].NewLine)
	}
}

// Markers whose hints differ must not share a lookup: a tag-prefix narrows what
// the provider may return, so the prefixed marker fetches on its own key.
func TestRunDifferingHintsDoNotCoalesce(t *testing.T) {
	var calls atomic.Int32
	provider.Register(memoProvider{
		fakeProvider: fakeProvider{
			name: "memohint",
			candidates: []model.Candidate{
				{Version: "api/v1.5.0"},
				candidate(t, "1.3.0"),
			},
		},
		calls: &calls,
	})

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=memohint repository=x/y tag-prefix=api/\nversion: v1.4.0\n",
		"b.txt": "# clover: provider=memohint repository=x/y\nversion: 1.2.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, int32(2), calls.Load(), "differing hints fetch separately")
	require.Equal(t, "version: v1.5.0", files[0].Results[0].NewLine,
		"the prefixed marker resolves its component's tag")
	require.Equal(t, "version: 1.3.0", files[1].Results[0].NewLine,
		"the bare marker resolves the plain tag")
}

// A failed lookup is memoized like a successful one: every duplicate marker
// fails with the shared error, without retrying the fetch.
func TestRunDiscoveryErrorShared(t *testing.T) {
	var calls atomic.Int32
	provider.Register(memoProvider{
		fakeProvider: fakeProvider{
			name: "memoerr",
			err:  errors.New("upstream unavailable"),
		},
		calls: &calls,
	})

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=memoerr repository=x/y\nversion: 1.2.0\n",
		"b.txt": "# clover: provider=memoerr repository=x/y\nversion: 1.2.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, int32(1), calls.Load(), "the failed fetch is not retried")
	for _, f := range files {
		require.EqualError(t, f.Results[0].Err, "upstream unavailable")
	}
}

// A shared truncated lookup replays its truncation to every marker: each one
// records the per-marker flag (recency-ordered) even though only the first
// fetched.
func TestRunSharedTruncationReplaysPerMarker(t *testing.T) {
	var calls atomic.Int32
	provider.Register(memoRecencyProvider{memoProvider{
		fakeProvider: fakeProvider{
			name:       "memotrunc",
			truncate:   true,
			candidates: []model.Candidate{candidate(t, "3.0.0")}, // a major bump
		},
		calls: &calls,
	}})

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=memotrunc repository=x/y constraint=minor\nversion: 1.2.0\n",
		"b.txt": "# clover: provider=memotrunc repository=x/y constraint=minor\nversion: 1.2.0\n",
	})

	var noted []provider.Truncation
	files, err := pipeline.Run(context.Background(), []string{dir},
		pipeline.WithTruncationSink(func(t provider.Truncation) { noted = append(noted, t) }))
	require.NoError(t, err)
	require.Equal(t, int32(1), calls.Load(), "duplicate lookups share one fetch")
	for _, f := range files {
		r := f.Results[0]
		require.ErrorIs(t, r.Err, pipeline.ErrNoCandidate)
		require.True(t, r.Truncated, "the shared truncation reaches every marker")
	}
	require.Empty(t, noted,
		"a recency-ordered source does not feed the blanket truncation sink")
}

// A shared truncated lookup on a lexically-ordered source feeds the blanket
// sink once per marker, exactly as independent fetches would have.
func TestRunSharedTruncationFeedsBlanketSinkPerMarker(t *testing.T) {
	var calls atomic.Int32
	provider.Register(memoProvider{
		fakeProvider: fakeProvider{
			name:       "memoblanket",
			truncate:   true,
			candidates: []model.Candidate{candidate(t, "1.3.0")},
		},
		calls: &calls,
	})

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=memoblanket repository=x/y\nversion: 1.2.0\n",
		"b.txt": "# clover: provider=memoblanket repository=x/y\nversion: 1.2.0\n",
	})

	// The markers replay concurrently, and the sink contract demands it be safe
	// for concurrent use, so the test's sink locks.
	var mu sync.Mutex
	var noted []provider.Truncation
	_, err := pipeline.Run(context.Background(), []string{dir},
		pipeline.WithTruncationSink(func(t provider.Truncation) {
			mu.Lock()
			noted = append(noted, t)
			mu.Unlock()
		}))
	require.NoError(t, err)
	require.Equal(t, int32(1), calls.Load(), "duplicate lookups share one fetch")
	want := provider.Truncation{Resource: "memoblanket", URL: "https://memoblanket"}
	require.Equal(t, []provider.Truncation{want, want}, noted,
		"each marker replays the truncation to the blanket sink")
}

// The shared candidate slice is never mutated: selection over unsorted
// candidates copies survivors rather than reordering its input, so the
// provider's slice reads back exactly as returned.
func TestRunSharedCandidatesNotMutated(t *testing.T) {
	var calls atomic.Int32
	cands := []model.Candidate{
		candidate(t, "1.3.0"),
		candidate(t, "1.2.5"),
		candidate(t, "1.4.0"),
	}
	original := slices.Clone(cands)
	provider.Register(memoProvider{
		fakeProvider: fakeProvider{name: "memoimmut", candidates: cands},
		calls:        &calls,
	})

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=memoimmut repository=x/y\nversion: 1.2.0\n",
		"b.txt": "# clover: provider=memoimmut repository=x/y\nversion: 1.2.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, int32(1), calls.Load(), "duplicate lookups share one fetch")
	for _, f := range files {
		require.Equal(t, "version: 1.4.0", f.Results[0].NewLine)
	}
	require.Equal(t, original, cands, "the shared slice is unmutated and unsorted")
}

// A follower of a coalesced producer projects the shared resolution: the two
// duplicate producers fetch once, and the follow edge still delivers the
// resolved version.
func TestRunFollowerOfCoalescedProducer(t *testing.T) {
	var calls atomic.Int32
	provider.Register(memoProvider{
		fakeProvider: fakeProvider{
			name:       "memoflw",
			candidates: []model.Candidate{candidate(t, "1.3.0")},
		},
		calls: &calls,
	})

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=memoflw repository=x/y id=app\nversion: 1.2.0\n",
		"b.txt": "# clover: provider=memoflw repository=x/y\nversion: 1.2.0\n",
		"c.txt": "# clover: from=app\ntool: 1.2.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, int32(1), calls.Load(), "duplicate producers share one fetch")
	require.Equal(t, "tool: 1.3.0", files[2].Results[0].NewLine,
		"the follower projects the shared producer's version")
}
