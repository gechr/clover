package mode_test

import (
	"context"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// offlineProvider fails if Discover is ever called, proving lint stays offline.
type offlineProvider struct{ name string }

func (p offlineProvider) Name() string { return p.name }

func (p offlineProvider) Color(bool) color.Color { return color.Gray{Y: 0x80} }

func (p offlineProvider) Keys() []provider.Key {
	return []provider.Key{{Name: "repository", Required: true}}
}

func (p offlineProvider) Resource(d directive.Directive) (provider.Resource, error) {
	if _, ok := d.Get("repository"); !ok {
		return nil, errors.New("repo is required")
	}
	return p.name, nil
}

func (p offlineProvider) Describe(provider.Resource) string { return p.name }

func (p offlineProvider) Discover(context.Context, provider.Resource) ([]model.Candidate, error) {
	panic("lint must never call Discover")
}

func TestLintCleanIsOK(t *testing.T) {
	provider.Register(offlineProvider{name: "lint"})
	dir := write(t, "# clover: provider=lint repository=x/y\nversion: 1.2.0\n")

	summary, err := mode.Lint(context.Background(), []string{dir})
	require.NoError(t, err)
	require.True(t, summary.OK())
	require.Equal(t, 0, summary.Errored())
}

func TestLintMissingRequiredKeyErrors(t *testing.T) {
	provider.Register(offlineProvider{name: "lintreq"})
	dir := write(t, "# clover: provider=lintreq\nversion: 1.2.0\n")

	summary, err := mode.Lint(context.Background(), []string{dir})
	require.NoError(t, err)
	require.False(t, summary.OK())
	require.Equal(t, 1, summary.Errored())
}

func TestLintAmbiguousTargetErrors(t *testing.T) {
	provider.Register(offlineProvider{name: "lintambig"})
	dir := write(t, "# clover: provider=lintambig repository=x/y\nfrom 1.0.0 to 2.0.0\n")

	summary, err := mode.Lint(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, 1, summary.Errored())
}

func TestLintBadConstraintErrors(t *testing.T) {
	provider.Register(offlineProvider{name: "lintc"})
	dir := write(
		t,
		"# clover: provider=lintc repository=x/y constraint=not-a-range\nversion: 1.2.0\n",
	)

	summary, err := mode.Lint(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, 1, summary.Errored())
}

func TestLintMalformedDirectiveErrors(t *testing.T) {
	dir := write(t, "# clover: provider=\"unterminated\nversion: 1.2.0\n")

	summary, err := mode.Lint(context.Background(), []string{dir})
	require.NoError(t, err)
	require.False(t, summary.OK())
	require.Equal(t, 1, summary.Errored())
}

func TestLintDanglingFollowSkips(t *testing.T) {
	dir := write(t, "# clover: from=ghost value=version\nversion: 1.2.0\n")

	summary, err := mode.Lint(context.Background(), []string{dir})
	require.NoError(t, err)
	require.False(t, summary.OK())
	require.Equal(t, 1, summary.Skipped())
}

func TestLintWritesNothing(t *testing.T) {
	provider.Register(offlineProvider{name: "lintw"})
	original := "# clover: provider=lintw repository=x/y\nversion: 1.2.0\n"
	dir := write(t, original)

	_, err := mode.Lint(context.Background(), []string{dir})
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(dir, "app.txt"))
	require.NoError(t, err)
	require.Equal(t, original, string(got))
}

// TestLintTargetAnchor covers the target= anchor offline: a resolving anchor
// lints clean, and one matching no line below the comment is a hard error.
func TestLintTargetAnchor(t *testing.T) {
	provider.Register(offlineProvider{name: "lintanchor"})

	clean := write(
		t,
		"# clover: provider=lintanchor repository=x/y target=version:*\n"+
			"name: app\n"+
			"version: 1.2.0\n",
	)
	summary, err := mode.Lint(context.Background(), []string{clean})
	require.NoError(t, err)
	require.True(t, summary.OK())

	noMatch := write(
		t,
		"# clover: provider=lintanchor repository=x/y target=nonesuch*\n"+
			"version: 1.2.0\n",
	)
	summary, err = mode.Lint(context.Background(), []string{noMatch})
	require.NoError(t, err)
	require.Equal(t, 1, summary.Errored(), "an anchor matching no line is a lint error")
}

func TestLintRejectsUnknownKey(t *testing.T) {
	provider.Register(fakeProvider{name: "uklint"})
	dir := write(t, "# clover: provider=uklint repository=x/y max-major=4\nversion: 1.2.0\n")

	summary, err := mode.Lint(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, 1, summary.Errored(), "lint rejects an unknown key")
	require.Equal(t, 0, summary.Skipped())
}
