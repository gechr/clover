package mode_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/cusp/internal/directive"
	"github.com/gechr/cusp/internal/mode"
	"github.com/gechr/cusp/internal/model"
	"github.com/gechr/cusp/internal/provider"
	"github.com/gechr/cusp/internal/version"
	"github.com/stretchr/testify/require"
)

// fakeProvider returns canned candidates without touching the network.
type fakeProvider struct {
	name       string
	candidates []model.Candidate
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
	return f.candidates, nil
}

func candidate(t *testing.T, tag string) model.Candidate {
	t.Helper()
	semver, err := version.Parse(tag)
	require.NoError(t, err)
	return model.Candidate{Version: tag, Semver: semver}
}

func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.txt"), []byte(body), 0o644))
	return dir
}

func TestRunWritesChanges(t *testing.T) {
	provider.Register(
		fakeProvider{name: "run", candidates: []model.Candidate{candidate(t, "1.5.0")}},
	)
	dir := write(t, "# cusp: provider=run repo=x/y\nversion: 1.2.0\n")

	summary, err := mode.Run(context.Background(), []string{dir}, false)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Changed())
	require.True(t, summary.Outcomes[0].Written)
	require.NoError(t, summary.Outcomes[0].WriteErr)

	got, err := os.ReadFile(filepath.Join(dir, "app.txt"))
	require.NoError(t, err)
	require.Equal(t, "# cusp: provider=run repo=x/y\nversion: 1.5.0\n", string(got))
}

func TestRunDryRunWritesNothing(t *testing.T) {
	provider.Register(
		fakeProvider{name: "dry", candidates: []model.Candidate{candidate(t, "1.5.0")}},
	)
	original := "# cusp: provider=dry repo=x/y\nversion: 1.2.0\n"
	dir := write(t, original)

	summary, err := mode.Run(context.Background(), []string{dir}, true)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Changed()) // change is computed...
	require.False(t, summary.Outcomes[0].Written)

	got, err := os.ReadFile(filepath.Join(dir, "app.txt"))
	require.NoError(t, err)
	require.Equal(t, original, string(got)) // ...but not written
}

func TestRunPreservesFileMode(t *testing.T) {
	provider.Register(
		fakeProvider{name: "perm", candidates: []model.Candidate{candidate(t, "2.0.0")}},
	)
	dir := t.TempDir()
	path := filepath.Join(dir, "run.sh")
	require.NoError(
		t,
		os.WriteFile(path, []byte("# cusp: provider=perm repo=x/y\nv=1.0.0\n"), 0o644),
	)
	// chmod after writing so umask does not reduce the mode under test.
	require.NoError(t, os.Chmod(path, 0o777))

	_, err := mode.Run(context.Background(), []string{dir}, false)
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o777), info.Mode().Perm()) // cusp never changes perms
}

func TestRunLeavesUnchangedFileUntouched(t *testing.T) {
	provider.Register(
		fakeProvider{name: "same", candidates: []model.Candidate{candidate(t, "1.2.0")}},
	)
	original := "# cusp: provider=same repo=x/y\nversion: 1.2.0\n"
	dir := write(t, original)

	summary, err := mode.Run(context.Background(), []string{dir}, false)
	require.NoError(t, err)
	require.Equal(t, 0, summary.Changed())
	require.False(t, summary.Outcomes[0].Written)

	got, err := os.ReadFile(filepath.Join(dir, "app.txt"))
	require.NoError(t, err)
	require.Equal(t, original, string(got))
}

func TestRunErroredMarkerNotWritten(t *testing.T) {
	original := "# cusp: provider=ghost repo=x/y\nversion: 1.0.0\n"
	dir := write(t, original)

	summary, err := mode.Run(context.Background(), []string{dir}, false)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Errored())
	require.False(t, summary.Outcomes[0].Written)

	got, err := os.ReadFile(filepath.Join(dir, "app.txt"))
	require.NoError(t, err)
	require.Equal(t, original, string(got))
}
