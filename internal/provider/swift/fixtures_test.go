package swift_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider/swift"
	"github.com/stretchr/testify/require"
)

// fixtureProvider serves testdata/releases.json for every request. The fixture
// is a real, verbatim slice of swift.org's install/releases.json - 4.1.1 (a
// linux_only release with no xcode_release flag), 5.10 (a two-component name),
// and 6.3.3 (the SDK entries carrying inline checksums) - each trimmed to a few
// platforms but kept byte-verbatim, so the tests exercise the actual data shapes
// the provider must handle.
func fixtureProvider(t *testing.T) *swift.Provider {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "releases.json"))
	require.NoError(t, err)
	return newProvider(string(body))
}

// TestFixtureDiscover covers discovery against the real index shape: every
// release surfaces in the index's own oldest-first order, each with its tag as
// the ref, a parsed semver, a publication date, and the SDK checksums captured
// as free digests.
func TestFixtureDiscover(t *testing.T) {
	t.Parallel()

	p := fixtureProvider(t)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	require.Equal(t, []string{"4.1.1", "5.10", "6.3.3"}, versions(got))
	for _, c := range got {
		require.NotNil(t, c.Semver, c.Version)
		require.False(t, c.PublishedAt.IsZero(), c.Version)
	}

	require.Equal(t, "swift-5.10-RELEASE", candidateFor(t, got, "5.10").Ref)

	// 6.3.3 exposes its SDK checksums as free digests, sourced without a
	// download; the Linux toolchain entry carries none.
	require.Equal(t, []model.Asset{
		{
			Name:   "static-sdk",
			Digest: "sha256:87c3eaf908e67c0e13a84367119e12273cec1d2cd3d81f7d74bb36722d6b607b",
		},
		{
			Name:   "wasm-sdk",
			Digest: "sha256:cabfa08b73bb8ac783927ecd15fa386e99d0c139c5f232445067bcf58379cae7",
		},
	}, candidateFor(t, got, "6.3.3").Assets)
}
