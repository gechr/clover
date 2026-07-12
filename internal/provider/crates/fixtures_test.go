package crates_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/stretchr/testify/require"
)

// fixtureDiscover resolves candidates for a crate against its testdata fixture,
// a real, verbatim slice of crates.io/api/v1/crates/<crate>/versions - selected
// version records keeping the full field set (features, audit_actions,
// published_by, ...) the provider ignores - so the tests exercise the actual
// shape of the data.
func fixtureDiscover(t *testing.T, name, pkg string) []model.Candidate {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	p := newProvider(string(body))
	candidates, err := p.Discover(
		t.Context(),
		resourceFor(t, p, directive.KV{Key: "package", Value: pkg}),
	)
	require.NoError(t, err)
	return candidates
}

// TestFixtureClap covers the happy path and the liveness gate against real
// records: the yanked 2.21.0 drops, the surviving versions surface in natural
// order, each dated by its publish time and carrying its .crate file as a
// digested asset.
func TestFixtureClap(t *testing.T) {
	t.Parallel()

	got := fixtureDiscover(t, "clap.json", "clap")
	require.Equal(t, []string{"4.0.0-rc.3", "4.6.1"}, versions(got))

	rc := got[0]
	require.Equal(t, "4.0.0-rc.3", rc.Ref)
	require.NotNil(t, rc.Semver)
	require.Equal(t, "rc.3", rc.Semver.Prerelease())

	latest := got[1]
	require.Equal(t, "4.6.1", latest.Ref)
	require.Equal(t,
		time.Date(2026, 4, 15, 18, 59, 5, 142929000, time.UTC),
		latest.PublishedAt,
	)
	require.Equal(t, []model.Asset{
		{
			Name:   "clap-4.6.1.crate",
			Digest: "sha256:1ddb117e43bbf7dacf0a4190fef4d345b9bad68dfc649cb349e7d17d28428e51",
			URL:    "https://crates.io/api/v1/crates/clap/4.6.1/download",
		},
	}, latest.Assets)

	for _, c := range got {
		require.NotNil(t, c.Semver, c.Version)
		require.False(t, c.PublishedAt.IsZero(), c.Version)
	}
}
