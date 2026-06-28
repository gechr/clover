package gitlab_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider/gitlab"
	"github.com/stretchr/testify/require"
)

// fixtureProvider serves testdata/tags.json on the tags endpoint and
// testdata/releases.json on the releases endpoint. Both fixtures are real,
// verbatim slices of gitlab.com/api/v4 responses for gitlab-org/cli (the full
// field set the provider ignores included), so the tests exercise the actual
// shape of the data.
func fixtureProvider(t *testing.T) *gitlab.Provider {
	t.Helper()
	tags := readFixture(t, "tags.json")
	releases := readFixture(t, "releases.json")
	return gitlab.New(gitlab.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := "[]"
			switch {
			case strings.Contains(req.URL.Path, "/repository/tags"):
				body = tags
			case strings.Contains(req.URL.Path, "/releases"):
				body = releases
			}
			return jsonResponse(req, body), nil
		}),
	))
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(body)
}

// TestFixtureTags covers tag discovery against the real listing: each tag
// surfaces newest-first in the order the query asked for, carrying its commit SHA
// and committed date.
func TestFixtureTags(t *testing.T) {
	t.Parallel()

	p := fixtureProvider(t)
	got, err := p.Discover(t.Context(), resourceFor(t, p,
		directive.KV{Key: "repository", Value: "gitlab-org/cli"},
	))
	require.NoError(t, err)
	require.Equal(t,
		[]string{"v1.105.0", "v1.104.0", "v1.103.0", "v1.102.0"},
		versions(got),
	)
	for _, c := range got {
		require.NotNil(t, c.Semver, c.Version)
		require.NotEmpty(t, c.Commit, c.Version)
	}

	// The captured listing has a real mix: GitLab returns created_at for some
	// tags and null for others, even within one project. A present date populates
	// PublishedAt (for cooldown); a null one leaves it zero, rather than falling
	// back to the target commit's date.
	byVersion := map[string]time.Time{}
	for _, c := range got {
		byVersion[c.Version] = c.PublishedAt
	}
	require.Zero(t, byVersion["v1.105.0"]) // created_at null
	require.Equal(t, time.Date(2026, 6, 23, 12, 8, 11, 0, time.UTC), byVersion["v1.104.0"])
	require.Equal(t, time.Date(2026, 6, 16, 23, 50, 19, 0, time.UTC), byVersion["v1.103.0"])
	require.Zero(t, byVersion["v1.102.0"]) // created_at null
}

// TestFixtureReleases covers release discovery against the real listing: each
// release carries its commit SHA, release date, and asset links - the latter with
// no digest, which GitLab does not supply.
func TestFixtureReleases(t *testing.T) {
	t.Parallel()

	p := fixtureProvider(t)
	got, err := p.Discover(t.Context(), resourceFor(t, p,
		directive.KV{Key: "repository", Value: "gitlab-org/cli"},
		directive.KV{Key: "source", Value: "releases"},
	))
	require.NoError(t, err)
	require.Equal(t,
		[]string{"v1.105.0", "v1.104.0", "v1.103.0"},
		versions(got),
	)
	for _, c := range got {
		require.NotEmpty(t, c.Commit, c.Version)
		require.False(t, c.PublishedAt.IsZero(), c.Version)
		require.NotEmpty(t, c.Assets, c.Version)
		for _, a := range c.Assets {
			require.NotEmpty(t, a.Name)
			require.NotEmpty(t, a.URL)
			require.Empty(t, a.Digest)
		}
	}

	require.Equal(t, []model.Asset{
		{
			Name: "glab_1.105.0_linux_armv6.tar.gz",
			URL:  "https://gitlab.com/api/v4/projects/gitlab-org%2Fcli/packages/generic/glab/1%2E105%2E0/glab_1%2E105%2E0_linux_armv6%2Etar%2Egz",
		},
		{
			Name: "glab_1.105.0_windows_386.zip",
			URL:  "https://gitlab.com/api/v4/projects/gitlab-org%2Fcli/packages/generic/glab/1%2E105%2E0/glab_1%2E105%2E0_windows_386%2Ezip",
		},
	}, got[0].Assets)
}
