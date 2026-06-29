package mode_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// failingProvider resolves its resource but errors on Discover, so a marker that
// reaches the network fails - used to prove a resolved sidecar entry's failure is
// not softened at run.
type failingProvider struct{ name string }

func (p failingProvider) Name() string { return p.name }

func (p failingProvider) Keys() []provider.Key {
	return []provider.Key{{Name: "repository", Required: false}}
}

func (p failingProvider) Resource(directive.Directive) (provider.Resource, error) {
	return p.name, nil
}

func (p failingProvider) Describe(provider.Resource) string { return p.name }

func (p failingProvider) Discover(context.Context, provider.Resource) ([]model.Candidate, error) {
	return nil, errors.New("upstream down")
}

// writeTree writes named files under a fresh temp dir, for sidecar fixtures that
// need a target plus its <target>.clover.yaml sibling.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
	return dir
}

const schemaTarget = `{
  "$schema": "https://example.test/schemas/1.5.3/schema.json"
}
`

// A broken sidecar (a find that matches no line) fails lint: a hard, CI-blocking
// error reported against the sidecar file, not the target.
func TestLintBrokenSidecarErrors(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"tsconfig.json": schemaTarget,
		"tsconfig.json.clover.yaml": "- provider: github\n" +
			"  repository: a/b\n" +
			"  find: nonesuch\n",
	})

	summary, err := mode.Lint(context.Background(), []string{dir})
	require.NoError(t, err)
	require.False(t, summary.OK())
	require.Equal(t, 1, summary.Errored())

	var (
		loc  string
		line int
	)
	for _, o := range summary.Outcomes {
		for _, r := range o.Results {
			if r.Err != nil {
				loc, line = filepath.Base(r.Marker.File), r.Marker.Target
			}
		}
	}
	require.Equal(t, "tsconfig.json.clover.yaml", loc, "reported against the sidecar file")
	require.Equal(t, 0, line, "at the sidecar entry line, not target line 1")
}

// The same broken sidecar at run is a skip-with-warning, not a failure: the run
// proceeds and merely warns, so one malformed sidecar never sinks the run.
func TestRunBrokenSidecarSkips(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"tsconfig.json": schemaTarget,
		"tsconfig.json.clover.yaml": "- provider: github\n" +
			"  repository: a/b\n" +
			"  find: nonesuch\n",
	})

	summary, err := mode.Run(context.Background(), []string{dir}, true /* dry-run */)
	require.NoError(t, err)
	require.Equal(t, 0, summary.Errored(), "a broken sidecar does not fail the run")
	require.Equal(t, 1, summary.Skipped(), "it surfaces as a skip-with-warning")
}

// A clover:ignore-suppressed sidecar entry surfaces as a visible skip at lint -
// the local opt-out wins, but it is not silently dropped.
func TestLintIgnoreSuppressedSidecarSkips(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"tsconfig.jsonc": "{\n  // clover:ignore\n  \"$schema\": \"https://x/schemas/1.5.3/schema.json\"\n}\n",
		"tsconfig.jsonc.clover.yaml": "- provider: github\n" +
			"  repository: a/b\n" +
			"  find: schemas/<version>/schema.json\n",
	})

	summary, err := mode.Lint(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, 0, summary.Errored())
	require.Equal(t, 1, summary.Skipped(), "the suppressed entry is a visible skip")
}

// A resolved sidecar entry whose upstream resolution fails stays a hard error at
// run, exactly like the inline equivalent: only structural sidecar diagnostics
// are softened to skips, not genuine resolution failures.
func TestRunResolvedSidecarFailureStaysHard(t *testing.T) {
	provider.Register(failingProvider{name: "parityfake"})
	dir := writeTree(t, map[string]string{
		"tsconfig.json": schemaTarget,
		"tsconfig.json.clover.yaml": "- provider: parityfake\n" +
			"  repository: a/b\n" +
			"  find: schemas/<version>/schema.json\n",
	})

	summary, err := mode.Run(context.Background(), []string{dir}, true /* dry-run */)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Errored(), "an upstream failure is not softened away")
	require.Equal(t, 0, summary.Skipped())
}

// A dangling sidecar (no target sibling) fails lint and names the missing target.
func TestLintDanglingSidecarErrors(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"orphan.json.clover.yaml": "- provider: github\n  find: x\n",
	})

	summary, err := mode.Lint(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, 1, summary.Errored())
}

// A sidecar entry carrying an unknown key fails lint, with a did-you-mean
// suggestion flowing through CheckKeysSidecar.
func TestLintSidecarUnknownKeyErrors(t *testing.T) {
	provider.Register(offlineProvider{name: "uksidecar"})
	dir := writeTree(t, map[string]string{
		"tsconfig.json": schemaTarget,
		"tsconfig.json.clover.yaml": "- provider: uksidecar\n" +
			"  repository: a/b\n" +
			"  find: schemas/<version>/schema.json\n" +
			"  repositroy: typo\n",
	})

	summary, err := mode.Lint(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, 1, summary.Errored())

	var lintErr error
	for _, o := range summary.Outcomes {
		for _, r := range o.Results {
			if r.Err != nil {
				lintErr = r.Err
			}
		}
	}
	require.EqualError(t, lintErr, `unknown key "repositroy" (did you mean "repository"?)`)
}
