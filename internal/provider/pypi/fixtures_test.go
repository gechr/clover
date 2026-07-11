package pypi_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/stretchr/testify/require"
)

// fixtureDiscover resolves candidates for a package against its testdata
// fixture, a real, verbatim slice of pypi.org/pypi/<package>/json - selected
// versions with their first files, keeping the full field set (packagetype,
// requires_python, md5_digest, ...) the provider ignores - so the tests
// exercise the actual shape of the data.
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

// TestFixtureUvBuild covers the happy path: every installable version surfaces
// in natural order, the dashless rc normalized to canonical semver with its
// raw form on Ref, each dated by its earliest upload and carrying its files as
// digested assets.
func TestFixtureUvBuild(t *testing.T) {
	t.Parallel()

	got := fixtureDiscover(t, "uv-build.json", "uv_build")
	require.Equal(t,
		[]string{"0.5.30-rc1", "0.10.0", "0.11.16", "0.11.28"},
		versions(got),
	)

	rc := got[0]
	require.Equal(t, "0.5.30rc1", rc.Ref)
	require.NotNil(t, rc.Semver)
	require.Equal(t, "rc1", rc.Semver.Prerelease())

	latest := got[3]
	require.Equal(t, "0.11.28", latest.Ref)
	require.Equal(t,
		time.Date(2026, 7, 7, 23, 11, 52, 639934000, time.UTC),
		latest.PublishedAt,
		"the version is dated by its earliest file upload",
	)
	require.Equal(t, []model.Asset{
		{
			Name:   "uv_build-0.11.28-py3-none-linux_armv6l.whl",
			Digest: "sha256:fb10719142d431087f5e177d43c83f304391084a28ea52e1588542fe0f113f91",
			URL:    "https://files.pythonhosted.org/packages/59/95/5745516ff06d9245ba286732d90437aa8f6d4d493ee65c867acf8c676a22/uv_build-0.11.28-py3-none-linux_armv6l.whl",
		},
		{
			Name:   "uv_build-0.11.28-py3-none-macosx_10_12_x86_64.whl",
			Digest: "sha256:384a352d6b00df4824dcb56baa070498666795c3fd1b6d7377368649b695f864",
			URL:    "https://files.pythonhosted.org/packages/47/a2/3859648b141b4ce7adccf6f8e905afdaf002af306243e4905dd898cfdaef/uv_build-0.11.28-py3-none-macosx_10_12_x86_64.whl",
		},
	}, latest.Assets)

	for _, c := range got {
		require.NotNil(t, c.Semver, c.Version)
		require.False(t, c.PublishedAt.IsZero(), c.Version)
	}
}

// TestFixtureRequests covers the liveness gates against real records: the
// zero-file 0.0.1 and the fully yanked 2.32.1 both drop, leaving only the
// installable release.
func TestFixtureRequests(t *testing.T) {
	t.Parallel()

	got := fixtureDiscover(t, "requests.json", "requests")
	require.Equal(t, []string{"2.32.5"}, versions(got))
	require.Equal(t,
		time.Date(2025, 8, 18, 20, 46, 0, 542304000, time.UTC),
		got[0].PublishedAt,
	)
}
