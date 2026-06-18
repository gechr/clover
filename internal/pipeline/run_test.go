package pipeline_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
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
	return []provider.Key{{Name: "repository", Required: false}}
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
		"app.txt": "# clover: provider=fake repository=x/y\nversion: 1.2.0\n",
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
		[]string{"# clover: provider=fake repository=x/y", "version: 1.3.0", ""},
		files[0].Rewritten(),
	)
}

func TestRunPreservesStyle(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "styled",
		candidates: []model.Candidate{candidate(t, "1.4.0")},
	})

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=styled repository=x/y\nimage: v1.2\n",
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
		"a.txt": "# clover: provider=lead repository=x/y id=app\nlead: 1.0.0\n",
		"b.txt": "# clover: from=app value=version\nfollower: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 2)

	// a.txt sorts before b.txt; the producer resolves, then the follower reuses it.
	require.Equal(t, "2.0.0", files[0].Results[0].Resolved)
	require.Equal(t, "2.0.0", files[1].Results[0].Resolved)
	require.Equal(t, "follower: 2.0.0", files[1].Results[0].NewLine)
}

// TestRunAllowDowngradeOverride confirms the run-level flag overrides the
// per-directive allow-downgrade rule: nil leaves the directive in force, true
// forces a downgrade the directive did not permit, and false blocks one it did.
func TestRunAllowDowngradeOverride(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "downflag",
		candidates: []model.Candidate{candidate(t, "1.0.0")}, // only a lower version upstream
	})
	provider.Register(fakeProvider{
		name:       "downrule",
		candidates: []model.Candidate{candidate(t, "1.0.0")},
	})

	// Directive silent on downgrade: default refuses, the flag forces it.
	noRule := write(t, map[string]string{
		"app.txt": "# clover: provider=downflag repository=x/y\nversion: 2.0.0\n",
	})
	files, err := pipeline.Run(context.Background(), []string{noRule})
	require.NoError(t, err)
	require.Error(t, files[0].Results[0].Err, "downgrade refused by default")

	files, err = pipeline.Run(context.Background(), []string{noRule},
		pipeline.WithAllowDowngrade(new(true)))
	require.NoError(t, err)
	require.True(t, files[0].Results[0].Changed)
	require.Equal(t, "1.0.0", files[0].Results[0].Resolved, "flag forced the downgrade")

	// Directive allows downgrade: nil keeps it, false overrides to block.
	withRule := write(t, map[string]string{
		"app.txt": "# clover: provider=downrule repository=x/y allow-downgrade=true\nversion: 2.0.0\n",
	})
	files, err = pipeline.Run(context.Background(), []string{withRule})
	require.NoError(t, err)
	require.Equal(t, "1.0.0", files[0].Results[0].Resolved, "directive allows the downgrade")

	files, err = pipeline.Run(context.Background(), []string{withRule},
		pipeline.WithAllowDowngrade(new(false)))
	require.NoError(t, err)
	require.Error(t, files[0].Results[0].Err, "flag overrode the directive to block")
}

// TestRunPrereleaseOverride confirms WithPrerelease(true) lets a marker select a
// prerelease the per-directive rule would otherwise exclude.
func TestRunPrereleaseOverride(t *testing.T) {
	provider.Register(fakeProvider{
		name: "preflag",
		candidates: []model.Candidate{
			candidate(t, "1.0.0"),
			candidate(t, "2.0.0-rc.1"),
		},
	})

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=preflag repository=x/y\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.False(t, files[0].Results[0].Changed, "prereleases excluded by default")

	files, err = pipeline.Run(context.Background(), []string{dir},
		pipeline.WithPrerelease(new(true)))
	require.NoError(t, err)
	require.True(t, files[0].Results[0].Changed)
	require.Equal(t, "2.0.0-rc.1", files[0].Results[0].Resolved, "flag allowed the prerelease")
}

func TestRunUnknownProviderErrors(t *testing.T) {
	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=nope repository=x/y\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Error(t, files[0].Results[0].Err)
	require.False(t, files[0].Results[0].Changed)
}

// TestRunUnresolvedAutoErrors confirms a provider=auto marker whose target line
// matches no inference rule fails with a message pointing at the fix, not a
// confusing "unknown provider auto".
func TestRunUnresolvedAutoErrors(t *testing.T) {
	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=auto\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.ErrorContains(t, files[0].Results[0].Err, "could not infer a provider")
	require.False(t, files[0].Results[0].Changed)
}

func TestRunDanglingFollowSkips(t *testing.T) {
	dir := write(t, map[string]string{
		"app.txt": "# clover: from=missing value=version\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.True(t, files[0].Results[0].Skipped)
	// The reason names the bare id the user wrote, not the internal namespaced key.
	require.Equal(t, `unknown from "missing"`, files[0].Results[0].Reason)
}

func TestRunAmbiguousTargetErrors(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "ambig",
		candidates: []model.Candidate{candidate(t, "1.3.0")},
	})

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=ambig repository=x/y\nrange 1.0.0 to 2.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Error(t, files[0].Results[0].Err)
}
