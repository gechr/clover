package rust_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gechr/clover/internal/provider/rust"
	"github.com/stretchr/testify/require"
)

// fixtureProvider serves testdata/manifests.txt for every request. Each line is
// a verbatim entry from static.rust-lang.org's manifest index, kept in the
// index's own (chronological) order. The lines span the channel pointers
// (stable, beta, nightly), a stable release re-published two days later
// (1.8.0), the minor alias (1.75, 1.97), the moving beta aliases (1.75-beta,
// 1.75.0-beta), and numbered beta snapshots, so the tests exercise the real
// token shapes the provider must tell apart.
func fixtureProvider(t *testing.T) *rust.Provider {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "manifests.txt"))
	require.NoError(t, err)
	return newProvider(string(body))
}

// TestFixtureDiscoverStable covers stable discovery against the real index
// shape: each full X.Y.Z manifest surfaces once, in index order, dated by its
// first directory, with every alias and channel pointer dropped.
func TestFixtureDiscoverStable(t *testing.T) {
	t.Parallel()

	p := fixtureProvider(t)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	require.Equal(t, []string{"1.8.0", "1.9.0", "1.75.0", "1.97.0"}, versions(got))

	// The 1.8.0 manifest was re-published on 2016-04-14; the release keeps its
	// original 2016-04-12 date.
	require.Equal(t, time.Date(2016, 4, 12, 0, 0, 0, 0, time.UTC), got[0].PublishedAt)

	for _, c := range got {
		require.NotNil(t, c.Semver, c.Version)
		require.False(t, c.PublishedAt.IsZero(), c.Version)
		require.Equal(t, c.Version, c.Ref)
		require.False(t, c.Prerelease, c.Version)
	}
}

// TestFixtureDiscoverBeta covers beta discovery against the real index shape:
// only the numbered snapshots surface - the moving 1.75-beta and 1.75.0-beta
// aliases republished with every snapshot are dropped, so each candidate is a
// distinct, pinnable version.
func TestFixtureDiscoverBeta(t *testing.T) {
	t.Parallel()

	p := fixtureProvider(t)
	got, err := p.Discover(t.Context(), resourceFor(t, p, betaChannel))
	require.NoError(t, err)

	require.Equal(
		t,
		[]string{"1.75.0-beta.1", "1.75.0-beta.2", "1.97.0-beta.6", "1.98.0-beta.1"},
		versions(got),
	)

	for _, c := range got {
		require.NotNil(t, c.Semver, c.Version)
		require.False(t, c.PublishedAt.IsZero(), c.Version)
	}
}
