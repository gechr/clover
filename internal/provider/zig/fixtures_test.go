package zig_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider/zig"
	"github.com/stretchr/testify/require"
)

// fixtureProvider serves testdata/index.json for every request. The fixture is a
// real, verbatim slice of ziglang.org/download/index.json - the master nightly
// pointer, 0.16.0 (a new arch-os filename with version and notes fields), and
// 0.12.0 (an old os-arch filename with no version field) - each trimmed to a few
// platforms but kept byte-verbatim, so the tests exercise the actual data shapes
// the provider must handle.
func fixtureProvider(t *testing.T) *zig.Provider {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "index.json"))
	require.NoError(t, err)
	return newProvider(string(body))
}

// TestFixtureDiscover covers discovery against the real index shape: every
// release surfaces via its map key (master skipped), including the old 0.12.0
// entry that carries no version field, each with a parsed semver and the inline
// per-platform checksums captured as assets. The index is a map, so the version
// set is asserted sorted rather than in a fixed order.
func TestFixtureDiscover(t *testing.T) {
	t.Parallel()

	p := fixtureProvider(t)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	require.Equal(t, []string{"0.12.0", "0.16.0"}, sortedVersions(got))
	for _, c := range got {
		require.NotNil(t, c.Semver, c.Version)
		require.False(t, c.PublishedAt.IsZero(), c.Version)
	}

	// 0.16.0 exposes its x86_64-linux archive checksum as a free digest, sourced
	// without a download.
	require.Equal(t, model.Asset{
		Name:   "x86_64-linux",
		Digest: "sha256:70e49664a74374b48b51e6f3fdfbf437f6395d42509050588bd49abe52ba3d00",
		URL:    "https://ziglang.org/download/0.16.0/zig-x86_64-linux-0.16.0.tar.xz",
	}, assetFor(t, candidateFor(t, got, "0.16.0").Assets, "x86_64-linux"))
}
