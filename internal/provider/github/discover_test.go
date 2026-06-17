package github_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/provider/github"
	"github.com/stretchr/testify/require"
)

// roundTripFunc adapts a function to an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// jsonTransport answers every request with body and records the requested path.
func jsonTransport(body string, path *string) roundTripFunc {
	return func(req *http.Request) (*http.Response, error) {
		*path = req.URL.Path
		header := http.Header{}
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	}
}

func TestDiscoverTags(t *testing.T) {
	t.Parallel()

	const body = `[
		{"name": "v1.2.0", "commit": {"sha": "aaa"}},
		{"name": "v1.3.0", "commit": {"sha": "bbb"}},
		{"name": "nightly", "commit": {"sha": "ccc"}}
	]`
	var path string
	provider := github.New(github.WithTransport(jsonTransport(body, &path)))

	res, err := provider.Resource(directiveOf(directive.KV{Key: "repo", Value: "owner/name"}))
	require.NoError(t, err)
	candidates, err := provider.Discover(t.Context(), res)
	require.NoError(t, err)

	require.Contains(t, path, "/repos/owner/name/tags")
	require.Len(t, candidates, 3)
	require.Equal(t, "v1.2.0", candidates[0].Version)
	require.Equal(t, "aaa", candidates[0].Commit)
	require.NotNil(t, candidates[0].Semver)
	require.Nil(t, candidates[2].Semver, "non-semver tag yields a nil Semver")
}

func TestDiscoverReleases(t *testing.T) {
	t.Parallel()

	const body = `[
		{"tag_name": "v2.0.0", "published_at": "2026-01-02T03:04:05Z", "draft": false},
		{"tag_name": "v2.1.0", "published_at": "2026-02-02T03:04:05Z", "draft": true}
	]`
	var path string
	provider := github.New(github.WithTransport(jsonTransport(body, &path)))

	res, err := provider.Resource(directiveOf(
		directive.KV{Key: "repo", Value: "owner/name"},
		directive.KV{Key: "source", Value: "releases"},
	))
	require.NoError(t, err)
	candidates, err := provider.Discover(t.Context(), res)
	require.NoError(t, err)

	require.Contains(t, path, "/repos/owner/name/releases")
	require.Len(t, candidates, 1, "draft release is skipped")
	require.Equal(t, "v2.0.0", candidates[0].Version)
	require.False(t, candidates[0].PublishedAt.IsZero())
}
