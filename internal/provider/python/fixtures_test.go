package python_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/provider/python"
	"github.com/stretchr/testify/require"
)

// fixtureProvider serves testdata/releases.json for every request. Each object
// is a verbatim entry from python.org's downloads API, kept in the API's own
// order (ascending by release), with the full field set the API returns. The
// entries span two stable releases, a beta, and a non-interpreter "Python
// install manager" row, so the tests exercise the real data shapes the provider
// must handle.
func fixtureProvider(t *testing.T) *python.Provider {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "releases.json"))
	require.NoError(t, err)
	return newProvider(string(body))
}

// TestFixtureDiscover covers discovery against the real API shape: each
// interpreter release surfaces in the API's order with its "Python " prefix
// stripped to canonical semver, the prerelease flag, and the publication date
// cooldown consumes. The "Python install manager" row, whose name has no version
// after the prefix, is dropped rather than surfaced as an unparseable candidate.
func TestFixtureDiscover(t *testing.T) {
	t.Parallel()

	p := fixtureProvider(t)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	require.Equal(t, []string{"3.13.14", "3.14.6", "3.15.0-b3"}, versions(got))
	require.Equal(t, "3.13.14", got[0].Ref)

	// The beta is flagged; the stable releases are not.
	require.True(t, got[2].Prerelease)
	require.False(t, got[0].Prerelease)

	for _, c := range got {
		require.NotNil(t, c.Semver, c.Version)
		require.False(t, c.PublishedAt.IsZero(), c.Version)
	}
}
