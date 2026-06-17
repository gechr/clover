package pipeline_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/cusp/internal/directive"
	"github.com/gechr/cusp/internal/model"
	"github.com/gechr/cusp/internal/pipeline"
	"github.com/gechr/cusp/internal/provider"
	"github.com/gechr/cusp/internal/version"
	"github.com/stretchr/testify/require"
)

// fakeProvider is a registered provider that returns canned candidates without
// touching the network, so a run resolves deterministically in tests.
type fakeProvider struct {
	name       string
	candidates []model.Candidate
	err        error
}

func (f fakeProvider) Name() string { return f.name }

func (f fakeProvider) Keys() []provider.Key {
	return []provider.Key{{Name: "repo", Required: false}}
}

func (f fakeProvider) Resource(directive.Directive) (provider.Resource, error) {
	return f.name, nil
}

func (f fakeProvider) Describe(provider.Resource) string { return f.name }

func (f fakeProvider) Discover(context.Context, provider.Resource) ([]model.Candidate, error) {
	return f.candidates, f.err
}

// candidate parses tag into a candidate the selection chain can order.
func candidate(t *testing.T, tag string) model.Candidate {
	t.Helper()
	semver, err := version.Parse(tag)
	require.NoError(t, err)
	return model.Candidate{Version: tag, Semver: semver}
}

// write lays out files under a fresh temp dir and returns its path.
func write(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	return dir
}

func TestRunResolvesProducer(t *testing.T) {
	provider.Register(fakeProvider{
		name: "fake",
		candidates: []model.Candidate{
			candidate(t, "1.2.0"),
			candidate(t, "1.3.0"),
			candidate(t, "1.2.5"),
		},
	})

	dir := write(t, map[string]string{
		"app.txt": "# cusp: provider=fake repo=x/y\nversion: 1.2.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)

	results := files[0].Results
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	require.False(t, results[0].Skipped)
	require.Equal(t, "1.2.0", results[0].Current)
	require.Equal(t, "1.3.0", results[0].Resolved)
	require.True(t, results[0].Changed)
	require.Equal(t, "version: 1.3.0", results[0].NewLine)

	require.Equal(
		t,
		[]string{"# cusp: provider=fake repo=x/y", "version: 1.3.0", ""},
		files[0].Rewritten(),
	)
}

func TestRunPreservesStyle(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "styled",
		candidates: []model.Candidate{candidate(t, "1.4.0")},
	})

	dir := write(t, map[string]string{
		"app.txt": "# cusp: provider=styled repo=x/y\nimage: v1.2\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)

	// v-prefix and two-component precision are preserved from the target line.
	require.Equal(t, "image: v1.4", files[0].Results[0].NewLine)
}

func TestRunFollower(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "lead",
		candidates: []model.Candidate{candidate(t, "2.0.0")},
	})

	dir := write(t, map[string]string{
		"a.txt": "# cusp: provider=lead repo=x/y id=app\nlead: 1.0.0\n",
		"b.txt": "# cusp: from=app value=version\nfollower: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 2)

	// a.txt sorts before b.txt; the producer resolves, then the follower reuses it.
	require.Equal(t, "2.0.0", files[0].Results[0].Resolved)
	require.Equal(t, "2.0.0", files[1].Results[0].Resolved)
	require.Equal(t, "follower: 2.0.0", files[1].Results[0].NewLine)
}

func TestRunUnknownProviderErrors(t *testing.T) {
	dir := write(t, map[string]string{
		"app.txt": "# cusp: provider=nope repo=x/y\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Error(t, files[0].Results[0].Err)
	require.False(t, files[0].Results[0].Changed)
}

func TestRunDanglingFollowSkips(t *testing.T) {
	dir := write(t, map[string]string{
		"app.txt": "# cusp: from=missing value=version\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.True(t, files[0].Results[0].Skipped)
	require.NotEmpty(t, files[0].Results[0].Reason)
}

func TestRunAmbiguousTargetErrors(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "ambig",
		candidates: []model.Candidate{candidate(t, "1.3.0")},
	})

	dir := write(t, map[string]string{
		"app.txt": "# cusp: provider=ambig repo=x/y\nrange 1.0.0 to 2.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Error(t, files[0].Results[0].Err)
}
