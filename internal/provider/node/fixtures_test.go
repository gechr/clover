package node_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider/node"
	"github.com/stretchr/testify/require"
)

// fixtureProvider serves testdata/index.json for every request. The fixture is a
// real, verbatim slice of nodejs.org/dist/index.json - currents and four LTS
// codenames, with the full field set (files, npm, v8, security, ...) the provider
// ignores - so the tests exercise the actual shape of the data.
func fixtureProvider(t *testing.T) *node.Provider {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "index.json"))
	require.NoError(t, err)
	return newProvider(string(body))
}

// discoverFixture resolves candidates against the fixture index.
func discoverFixture(t *testing.T, extra ...directive.KV) []model.Candidate {
	t.Helper()
	p := fixtureProvider(t)
	candidates, err := p.Discover(t.Context(), resourceFor(t, p, extra...))
	require.NoError(t, err)
	return candidates
}

// TestFixtureDiscover covers the default scope: every release surfaces in the
// index's own order (version-descending, not date-ordered - note v24.18.0 dated
// after v25.0.0 yet listed below it), each carrying its v-prefixed version, a
// parsed semver, and the publication date the index supplied.
func TestFixtureDiscover(t *testing.T) {
	t.Parallel()

	got := discoverFixture(t)
	require.Equal(t, []string{
		"v26.4.0", "v26.3.1", "v26.3.0", "v25.0.0",
		"v24.18.0", "v22.23.1", "v20.20.2", "v18.20.8",
	}, versions(got))

	require.Equal(t, "v26.4.0", got[0].Ref)
	require.Equal(t, time.Date(2026, 6, 24, 23, 59, 59, 0, time.UTC), got[0].PublishedAt)
	for _, c := range got {
		require.NotNil(t, c.Semver, c.Version)
		require.False(t, c.PublishedAt.IsZero(), c.Version)
	}
}

// TestFixtureLTS covers the lts scope: only the codenamed LTS lines (Krypton,
// Jod, Iron, Hydrogen) survive; the current (lts:false) releases drop.
func TestFixtureLTS(t *testing.T) {
	t.Parallel()

	got := discoverFixture(t, directive.KV{Key: "lts", Value: "true"})
	require.Equal(t,
		[]string{"v24.18.0", "v22.23.1", "v20.20.2", "v18.20.8"},
		versions(got),
	)
}
