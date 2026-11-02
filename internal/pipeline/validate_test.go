package pipeline_test

import (
	"context"
	"testing"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/tag"
	"github.com/stretchr/testify/require"
)

// resultCount totals the markers across files that survived to a result.
func resultCount(files []pipeline.FileResult) int {
	var n int
	for _, f := range files {
		n += len(f.Results)
	}
	return n
}

// TestValidateFiltersByTags confirms a tag filter keeps only matching markers
// and drops untagged ones, so --tags targets exactly the tagged directives.
func TestValidateFiltersByTags(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "tagfake",
		candidates: []model.Candidate{candidate(t, "1.0.0")},
	})
	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=tagfake repository=x/y tags=prod\nversion: 1.2.0\n" +
			"# clover: provider=tagfake repository=x/y tags=ci\nversion: 1.2.0\n" +
			"# clover: provider=tagfake repository=x/y\nversion: 1.2.0\n",
	})

	all, err := pipeline.Validate(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, 3, resultCount(all), "no filter keeps every marker")

	filter, err := tag.Parse([]string{"prod"})
	require.NoError(t, err)
	prod, err := pipeline.Validate(context.Background(), []string{dir},
		pipeline.WithTagFilter(filter),
	)
	require.NoError(t, err)
	require.Equal(t, 1, resultCount(prod), "only the prod-tagged marker survives")
}

// TestValidateTagFilterOrphansFollower documents the deliberate consequence of
// the strict tag filter: a follower kept by the filter whose producer the filter
// drops cannot resolve, so it skips with a clear dangling reason (no crash).
func TestValidateTagFilterOrphansFollower(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "orphanfake",
		candidates: []model.Candidate{candidate(t, "1.0.0")},
	})
	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=orphanfake repository=x/y id=app tags=prod\nlead: 1.0.0\n",
		"b.txt": "# clover: from=app value=version tags=ci\nfollower: 1.0.0\n",
	})

	filter, err := tag.Parse([]string{"ci"}) // keeps the follower, drops its producer
	require.NoError(t, err)
	files, err := pipeline.Validate(
		context.Background(),
		[]string{dir},
		pipeline.WithTagFilter(filter),
	)
	require.NoError(t, err)
	require.Equal(t, 1, resultCount(files), "only the ci-tagged follower survives the filter")

	var skipped []pipeline.Result
	for _, f := range files {
		for _, r := range f.Results {
			if r.Skipped {
				skipped = append(skipped, r)
			}
		}
	}
	require.Len(t, skipped, 1)
	require.Equal(t, `unknown from "app"`, skipped[0].Reason)
}

// errored reports the number of markers across files whose validation failed.
func errored(files []pipeline.FileResult) int {
	var n int
	for _, f := range files {
		for _, r := range f.Results {
			if r.Err != nil {
				n++
			}
		}
	}
	return n
}

func TestValidateCleanMarker(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "vfake",
		candidates: []model.Candidate{candidate(t, "1.0.0")},
	})
	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=vfake repository=x/y\nversion: 1.2.0\n",
	})

	files, err := pipeline.Validate(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, 0, errored(files))
	require.NoError(t, files[0].Results[0].Err)
}

// TestValidatePopulatesResource confirms a lint validates the descriptor far
// enough to capture the tracked resource, so a lint report can name it just like
// a run does.
func TestValidatePopulatesResource(t *testing.T) {
	provider.Register(fakeProvider{
		name:        "vresfake",
		resource:    "x/y",
		resourceURL: "https://example.test/x/y",
	})
	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=vresfake repository=x/y\nversion: 1.2.0\n",
	})

	files, err := pipeline.Validate(context.Background(), []string{dir})
	require.NoError(t, err)
	require.NoError(t, files[0].Results[0].Err)
	require.Equal(t, "x/y", files[0].Results[0].Resource)
	require.Equal(t, "https://example.test/x/y", files[0].Results[0].ResourceURL)
}

// TestValidateFollowerReportsProducerResource confirms a lint names a
// follower's resource as its producer's, matching what a run reports, and leaves
// it empty when the follower's source does not exist.
func TestValidateFollowerReportsProducerResource(t *testing.T) {
	provider.Register(fakeProvider{
		name:        "vfollowres",
		resource:    "x/y",
		resourceURL: "https://example.test/x/y",
	})
	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=vfollowres repository=x/y id=app\nversion: 1.2.0\n" +
			"# clover: from=app value=version\nfollower: 1.2.0\n" +
			"# clover: from=missing value=version\norphan: 1.2.0\n",
	})

	files, err := pipeline.Validate(context.Background(), []string{dir})
	require.NoError(t, err)
	results := files[0].Results
	require.Len(t, results, 3)
	require.Equal(t, "x/y", results[1].Resource)
	require.Equal(t, "https://example.test/x/y", results[1].ResourceURL)
	require.Empty(t, results[2].Resource)
}

// TestValidateFollowerResourceRefusesAmbiguousIDs confirms a follower names no
// resource when any hop of its chain reads an id several markers publish - lint
// fails the duplicates without cascading to the follower, so the lookup itself
// must refuse to pick one.
func TestValidateFollowerResourceRefusesAmbiguousIDs(t *testing.T) {
	provider.Register(fakeProvider{
		name:        "vambiguousres",
		resource:    "x/y",
		resourceURL: "https://example.test/x/y",
	})
	tests := []struct {
		name  string
		lines string
	}{
		{
			name: "duplicate producer",
			lines: "# clover: provider=vambiguousres repository=x/y id=app\nversion: 1.2.0\n" +
				"# clover: provider=vambiguousres repository=x/y id=app\nversion: 1.2.0\n",
		},
		{
			name: "duplicate intermediate follower",
			lines: "# clover: provider=vambiguousres repository=x/y id=lead\nversion: 1.2.0\n" +
				"# clover: from=lead value=version id=app\nmid: 1.2.0\n" +
				"# clover: from=lead value=version id=app\nmid: 1.2.0\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := write(t, map[string]string{
				"a.txt": tt.lines + "# clover: from=app value=version\nfollower: 1.2.0\n",
			})

			files, err := pipeline.Validate(context.Background(), []string{dir})
			require.NoError(t, err)
			results := files[0].Results
			follower := results[len(results)-1]
			require.Empty(t, follower.Resource)
			require.Empty(t, follower.ResourceURL)
		})
	}
}

func TestValidateStaysOffline(t *testing.T) {
	// fakeProvider with an error set would surface only if Discover were called;
	// validation never calls it, so a clean marker validates regardless.
	provider.Register(fakeProvider{name: "voffline", err: context.Canceled})
	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=voffline repository=x/y\nversion: 1.2.0\n",
	})

	files, err := pipeline.Validate(context.Background(), []string{dir})
	require.NoError(t, err)
	require.NoError(t, files[0].Results[0].Err)
}

func TestValidateUnknownProviderErrors(t *testing.T) {
	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=nosuch repository=x/y\nversion: 1.2.0\n",
	})

	files, err := pipeline.Validate(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Error(t, files[0].Results[0].Err)
}

func TestValidateDanglingFollowSkips(t *testing.T) {
	dir := write(t, map[string]string{
		"app.txt": "# clover: from=ghost value=version\nversion: 1.2.0\n",
	})

	files, err := pipeline.Validate(context.Background(), []string{dir})
	require.NoError(t, err)
	require.True(t, files[0].Results[0].Skipped)
	require.Equal(t, `unknown from "ghost"`, files[0].Results[0].Reason)
}

func TestValidateUnsupportedFollowValueErrors(t *testing.T) {
	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=vfake repository=x/y id=p\nlead: 1.0.0\n",
		"b.txt": "# clover: from=p value=sha256\ndigest: 1.0.0\n",
	})

	files, err := pipeline.Validate(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, 1, errored(files))
}
