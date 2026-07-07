package mode_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
	"github.com/stretchr/testify/require"
)

// TestRunInferUpdatesUnannotatedLines covers run --infer: a recognized line
// with no directive earns a synthetic provider=auto marker and is updated in
// place, an unrecognized line is left alone, a clover:ignore control still
// opts a line out, and no comments are written.
func TestRunInferUpdatesUnannotatedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mise.toml")
	body := "[tools]\n" +
		"node = \"24.0.0\"\n" +
		"# clover:ignore pinned deliberately\n" +
		"node = \"22.0.0\"\n" +
		"java = \"21.0.0\"\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	summary, err := mode.Run(context.Background(), []string{dir}, false, testWorkers,
		pipeline.WithInfer(true),
		pipeline.WithRequireDirective(false),
	)
	require.NoError(t, err)
	require.Len(t, summary.Outcomes, 1)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(
		t,
		"[tools]\n"+
			"node = \"26.0.0\"\n"+
			"# clover:ignore pinned deliberately\n"+
			"node = \"22.0.0\"\n"+
			"java = \"21.0.0\"\n",
		string(got),
		"the inferred node line is bumped, the ignored and unrecognized lines stay put",
	)
}

// TestRunWithoutInferIgnoresUnannotatedLines locks the default in: the same
// tree resolves nothing when --infer is off, since no directive exists.
func TestRunWithoutInferIgnoresUnannotatedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mise.toml")
	body := "[tools]\nnode = \"24.0.0\"\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	summary, err := mode.Run(context.Background(), []string{dir}, false, testWorkers)
	require.NoError(t, err)
	require.Empty(t, summary.Outcomes, "no directives, no work")

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, body, string(got))
}

// fakeProvider returns canned candidates without touching the network.
type fakeProvider struct {
	name       string
	candidates []model.Candidate
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
	dir := write(t, "# clover: provider=run repository=x/y\nversion: 1.2.0\n")

	summary, err := mode.Run(context.Background(), []string{dir}, false, testWorkers)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Changed())
	require.True(t, summary.Outcomes[0].Written)
	require.NoError(t, summary.Outcomes[0].WriteErr)

	got, err := os.ReadFile(filepath.Join(dir, "app.txt"))
	require.NoError(t, err)
	require.Equal(t, "# clover: provider=run repository=x/y\nversion: 1.5.0\n", string(got))
}

// TestRunAddsActionPinComment confirms run documents an undocumented SHA pin: a
// uses: line with a commit but no # version comment resolves per the directive
// (latest, here 4.2.0) and gains both the updated SHA and a fresh comment.
func TestRunAddsActionPinComment(t *testing.T) {
	const (
		oldSHA = "1234567890abcdef1234567890abcdef12345678"
		newSHA = "abcdef1234567890abcdef1234567890abcdef12"
	)
	latest := candidate(t, "4.2.0")
	latest.Commit = newSHA
	provider.Register(fakeProvider{name: "github", candidates: []model.Candidate{latest}})

	dir := t.TempDir()
	path := filepath.Join(dir, "ci.yaml")
	require.NoError(t, os.WriteFile(
		path,
		[]byte(
			"# clover: provider=github repository=actions/checkout\n- uses: actions/checkout@"+oldSHA+"\n",
		),
		0o644,
	))

	summary, err := mode.Run(context.Background(), []string{dir}, false, testWorkers)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Changed())

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(
		t,
		"# clover: provider=github repository=actions/checkout\n- uses: actions/checkout@"+newSHA+" # v4.2.0\n",
		string(got),
	)
}

// TestRunWritesTargetAnchoredLine confirms target= redirects the rewrite: the
// governed line is the first match below the comment, not the line immediately
// under it, and intervening lines stay untouched.
func TestRunWritesTargetAnchoredLine(t *testing.T) {
	provider.Register(
		fakeProvider{name: "anchor", candidates: []model.Candidate{candidate(t, "1.5.0")}},
	)
	dir := write(
		t,
		"# clover: provider=anchor repository=x/y target=/^version:/\n"+
			"name: app\n"+
			"description: version 1.0.0 of app\n"+
			"version: 1.2.0\n",
	)

	summary, err := mode.Run(context.Background(), []string{dir}, false, testWorkers)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Changed())

	got, err := os.ReadFile(filepath.Join(dir, "app.txt"))
	require.NoError(t, err)
	require.Equal(
		t,
		"# clover: provider=anchor repository=x/y target=/^version:/\n"+
			"name: app\n"+
			"description: version 1.0.0 of app\n"+
			"version: 1.5.0\n",
		string(got),
	)
}

// TestRunWritesOffsetLine confirms offset= redirects the rewrite a fixed number
// of lines below the comment instead of the default next line.
func TestRunWritesOffsetLine(t *testing.T) {
	provider.Register(
		fakeProvider{name: "skew", candidates: []model.Candidate{candidate(t, "1.5.0")}},
	)
	dir := write(
		t,
		"# clover: provider=skew repository=x/y offset=3\n"+
			"name: app\n"+
			"kind: demo\n"+
			"version: 1.2.0\n",
	)

	summary, err := mode.Run(context.Background(), []string{dir}, false, testWorkers)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Changed())

	got, err := os.ReadFile(filepath.Join(dir, "app.txt"))
	require.NoError(t, err)
	require.Equal(
		t,
		"# clover: provider=skew repository=x/y offset=3\n"+
			"name: app\n"+
			"kind: demo\n"+
			"version: 1.5.0\n",
		string(got),
	)
}

func TestRunDryRunWritesNothing(t *testing.T) {
	provider.Register(
		fakeProvider{name: "dry", candidates: []model.Candidate{candidate(t, "1.5.0")}},
	)
	original := "# clover: provider=dry repository=x/y\nversion: 1.2.0\n"
	dir := write(t, original)

	summary, err := mode.Run(context.Background(), []string{dir}, true, testWorkers)
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
		os.WriteFile(path, []byte("# clover: provider=perm repository=x/y\nv=1.0.0\n"), 0o644),
	)
	// chmod after writing so umask does not reduce the mode under test.
	require.NoError(t, os.Chmod(path, 0o777))

	_, err := mode.Run(context.Background(), []string{dir}, false, testWorkers)
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o777), info.Mode().Perm()) // clover never changes perms
}

func TestRunLeavesUnchangedFileUntouched(t *testing.T) {
	provider.Register(
		fakeProvider{name: "same", candidates: []model.Candidate{candidate(t, "1.2.0")}},
	)
	original := "# clover: provider=same repository=x/y\nversion: 1.2.0\n"
	dir := write(t, original)

	summary, err := mode.Run(context.Background(), []string{dir}, false, testWorkers)
	require.NoError(t, err)
	require.Equal(t, 0, summary.Changed())
	require.False(t, summary.Outcomes[0].Written)

	got, err := os.ReadFile(filepath.Join(dir, "app.txt"))
	require.NoError(t, err)
	require.Equal(t, original, string(got))
}

func TestRunErroredMarkerNotWritten(t *testing.T) {
	original := "# clover: provider=ghost repository=x/y\nversion: 1.0.0\n"
	dir := write(t, original)

	summary, err := mode.Run(context.Background(), []string{dir}, false, testWorkers)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Errored())
	require.False(t, summary.Outcomes[0].Written)

	got, err := os.ReadFile(filepath.Join(dir, "app.txt"))
	require.NoError(t, err)
	require.Equal(t, original, string(got))
}

func TestRunWarnsAndSkipsUnknownKey(t *testing.T) {
	provider.Register(
		fakeProvider{name: "ukrun", candidates: []model.Candidate{candidate(t, "1.5.0")}},
	)
	// The first marker carries an unknown key; the second is valid.
	dir := write(t,
		"# clover: provider=ukrun repository=x/y max-major=4\nversion: 1.2.0\n"+
			"# clover: provider=ukrun repository=x/y\nother: 1.2.0\n",
	)

	summary, err := mode.Run(context.Background(), []string{dir}, false, testWorkers)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Skipped(), "an unknown-key marker is skipped, not errored")
	require.Equal(t, 0, summary.Errored(), "run tolerates an unknown key")
	require.Equal(t, 1, summary.Changed(), "the valid marker still resolves")
}
