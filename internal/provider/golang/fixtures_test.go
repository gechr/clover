package golang_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider/golang"
	"github.com/stretchr/testify/require"
)

// fixtureProvider serves testdata/dl.json for every request. The fixture is a
// real, verbatim slice of go.dev/dl/?mode=json&include=all - an rc and two
// stable releases, each with a subset of its real files (source, a linux and a
// darwin archive, a windows installer) - so the tests exercise the actual shape
// of the data.
func fixtureProvider(t *testing.T) *golang.Provider {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "dl.json"))
	require.NoError(t, err)
	return newProvider(string(body))
}

// TestFixtureDiscover covers discovery against the real index shape: every
// release surfaces newest-first with its "go" prefix stripped to clean semver, a
// parsed semver, and its per-file checksums captured as assets.
func TestFixtureDiscover(t *testing.T) {
	t.Parallel()

	p := fixtureProvider(t)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	require.Equal(t, []string{"1.27.0-rc1", "1.26.5", "1.26.4"}, versions(got))
	require.Equal(t, "go1.27rc1", got[0].Ref)
	for _, c := range got {
		require.NotNil(t, c.Semver, c.Version)
	}

	// The latest stable release exposes its linux/amd64 archive checksum as a
	// free digest, sourced without a download.
	require.Contains(t, got[1].Assets, model.Asset{
		Name:   "go1.26.5.linux-amd64.tar.gz",
		Digest: "sha256:5c2c3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393f053",
		URL:    "https://go.dev/dl/go1.26.5.linux-amd64.tar.gz",
	})
}
