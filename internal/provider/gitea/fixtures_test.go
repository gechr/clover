package gitea_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider/gitea"
	xslices "github.com/gechr/x/slices"
	"github.com/stretchr/testify/require"
)

// fixtureProvider serves verbatim slices of the live Forgejo (Codeberg) API:
// testdata/tags.json for a /tags request, testdata/releases.json for /releases.
// Each object is byte-unmodified from codeberg.org/forgejo/forgejo, so the tests
// exercise the real field shapes (annotated-tag messages, the full release author
// and asset blocks the provider ignores).
func fixtureProvider(t *testing.T) *gitea.Provider {
	t.Helper()
	tags := readFixture(t, "tags.json")
	releases := readFixture(t, "releases.json")
	return newProvider(tags, releases)
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(body)
}

// TestFixtureTags covers the real tag listing: every tag surfaces in the API's
// creation-date order (note v17.0.0-dev, a pre-release, sits above the stable
// lines - the very reason the provider is not recency-ordered), each carrying its
// commit SHA and no publication date.
func TestFixtureTags(t *testing.T) {
	t.Parallel()

	got, err := fixtureProvider(t).Discover(t.Context(),
		resourceFor(t, gitea.New(), directive.KV{Key: "repository", Value: "forgejo/forgejo"}))
	require.NoError(t, err)
	require.Equal(t, []string{"v17.0.0-dev", "v15.0.3", "v11.0.15"}, versions(got))
	require.Equal(t, "d0dec3d857868e8e7187f92c29699fa3091c95ce", got[0].Commit)
	for _, c := range got {
		require.True(t, c.PublishedAt.IsZero(), c.Version)
	}
}

// TestFixtureReleases covers the real release listing: each release keeps its own
// publication date and carries the published assets.
func TestFixtureReleases(t *testing.T) {
	t.Parallel()

	got, err := fixtureProvider(t).Discover(t.Context(),
		resourceFor(t, gitea.New(),
			directive.KV{Key: "repository", Value: "forgejo/forgejo"},
			directive.KV{Key: "source", Value: "releases"},
		))
	require.NoError(t, err)
	require.Equal(t, []string{"v15.0.3", "v11.0.15"}, versions(got))

	require.Equal(t, "2026-06-10T08:11:24+02:00", got[0].PublishedAt.Format(time.RFC3339))
	require.NotEmpty(t, got[0].Assets)
	require.Contains(t, assetNames(got[0].Assets), "forgejo-15.0.3-linux-amd64")
}

func assetNames(assets []model.Asset) []string {
	return xslices.Map(assets, func(a model.Asset) string { return a.Name })
}
