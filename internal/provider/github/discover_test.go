package github_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/github"
	"github.com/stretchr/testify/require"
)

// tagsBody builds a JSON tags array of n entries, for exercising pagination.
func tagsBody(n int) string {
	var b strings.Builder
	b.WriteByte('[')
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"name":"1.0.%d","commit":{"sha":"s%d"}}`, i, i)
	}
	b.WriteByte(']')
	return b.String()
}

// pagedTags answers page 1 with a full page and page 2 with a short one,
// recording the page numbers requested.
func pagedTags(pages *[]string) roundTripFunc {
	full, short := tagsBody(100), tagsBody(5)
	return func(req *http.Request) (*http.Response, error) {
		page := req.URL.Query().Get("page")
		*pages = append(*pages, page)
		if page == "1" {
			return jsonResponse(req, full), nil
		}
		return jsonResponse(req, short), nil
	}
}

func TestDiscoverTagsShallowReadsOnePage(t *testing.T) {
	t.Parallel()

	var pages []string
	p := github.New(github.WithTransport(pagedTags(&pages)))
	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "owner/name"}))
	require.NoError(t, err)

	candidates, err := p.Discover(t.Context(), res)
	require.NoError(t, err)
	require.Len(t, candidates, 100)
	require.Equal(t, []string{"1"}, pages, "the shallow default stops after the first page")
}

func TestDiscoverTagsDeepPaginates(t *testing.T) {
	t.Parallel()

	var pages []string
	p := github.New(github.WithTransport(pagedTags(&pages)))
	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "owner/name"}))
	require.NoError(t, err)

	candidates, err := p.Discover(provider.WithDeep(t.Context(), true), res)
	require.NoError(t, err)
	require.Len(t, candidates, 105)
	require.Equal(t, []string{"1", "2"}, pages, "a deep lookup follows pages until a short one")
}

func TestDiscoverTagsShallowNotesTruncation(t *testing.T) {
	t.Parallel()

	var p1, p2 []string
	shallow := github.New(github.WithTransport(pagedTags(&p1)))
	deep := github.New(github.WithTransport(pagedTags(&p2)))

	res, err := shallow.Resource(directiveOf(directive.KV{Key: "repository", Value: "owner/name"}))
	require.NoError(t, err)
	resDeep, err := deep.Resource(directiveOf(directive.KV{Key: "repository", Value: "owner/name"}))
	require.NoError(t, err)

	var shallowTrunc []string
	sctx := provider.WithTruncationSink(t.Context(),
		func(r string) { shallowTrunc = append(shallowTrunc, r) })
	_, err = shallow.Discover(sctx, res)
	require.NoError(t, err)
	require.Equal(t, []string{"github.com/owner/name"}, shallowTrunc,
		"a full first page on a shallow lookup means more tags exist")

	var deepTrunc []string
	dctx := provider.WithTruncationSink(provider.WithDeep(t.Context(), true),
		func(r string) { deepTrunc = append(deepTrunc, r) })
	_, err = deep.Discover(dctx, resDeep)
	require.NoError(t, err)
	require.Empty(t, deepTrunc, "a deep lookup exhausts the pages, so nothing is truncated")
}

// roundTripFunc adapts a function to an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// jsonTransport answers every request with body and records the requested path.
func jsonTransport(body string, path *string) roundTripFunc {
	return func(req *http.Request) (*http.Response, error) {
		*path = req.URL.Path
		return jsonResponse(req, body), nil
	}
}

// routeTransport answers each request with the body whose key is a substring of
// the request path, so a single provider can serve distinct endpoints.
func routeTransport(routes map[string]string) roundTripFunc {
	return func(req *http.Request) (*http.Response, error) {
		for key, body := range routes {
			if strings.Contains(req.URL.Path, key) {
				return jsonResponse(req, body), nil
			}
		}
		return nil, fmt.Errorf("no route for %s", req.URL.Path)
	}
}

// jsonResponse builds a 200 JSON response carrying body.
func jsonResponse(req *http.Request, body string) *http.Response {
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
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

	res, err := provider.Resource(directiveOf(directive.KV{Key: "repository", Value: "owner/name"}))
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

	const releases = `[
		{"tag_name": "v2.0.0", "published_at": "2026-01-02T03:04:05Z", "draft": false,
		 "assets": [{"name": "tool_linux_amd64.tar.gz", "digest": "sha256:abc",
		             "browser_download_url": "https://h/tool_linux_amd64.tar.gz"}]},
		{"tag_name": "v2.1.0", "published_at": "2026-02-02T03:04:05Z", "draft": true}
	]`
	const tags = `[
		{"name": "v2.0.0", "commit": {"sha": "deadbeef"}}
	]`
	provider := github.New(github.WithTransport(routeTransport(map[string]string{
		"/releases": releases,
		"/tags":     tags,
	})))

	res, err := provider.Resource(directiveOf(
		directive.KV{Key: "repository", Value: "owner/name"},
		directive.KV{Key: "source", Value: "releases"},
	))
	require.NoError(t, err)
	candidates, err := provider.Discover(t.Context(), res)
	require.NoError(t, err)

	require.Len(t, candidates, 1, "draft release is skipped")
	require.Equal(t, "v2.0.0", candidates[0].Version)
	require.Equal(t, "deadbeef", candidates[0].Commit, "commit resolved by joining the tags list")
	require.False(t, candidates[0].PublishedAt.IsZero())
	require.Equal(t, []model.Asset{{
		Name:   "tool_linux_amd64.tar.gz",
		Digest: "sha256:abc",
		URL:    "https://h/tool_linux_amd64.tar.gz",
	}}, candidates[0].Assets, "release assets and their digests are captured")
}

func TestDiscoverReleasesPrereleaseFlag(t *testing.T) {
	t.Parallel()

	// A published (non-draft) release flagged pre-release on a clean tag: the
	// flag is captured so selection can exclude it without the tag saying so.
	const releases = `[
		{"tag_name": "v2.0.0", "published_at": "2026-01-02T03:04:05Z", "draft": false, "prerelease": false},
		{"tag_name": "v2.1.0", "published_at": "2026-02-02T03:04:05Z", "draft": false, "prerelease": true}
	]`
	const tags = `[{"name": "v2.0.0", "commit": {"sha": "deadbeef"}}]`
	provider := github.New(github.WithTransport(routeTransport(map[string]string{
		"/releases": releases,
		"/tags":     tags,
	})))

	res, err := provider.Resource(directiveOf(
		directive.KV{Key: "repository", Value: "owner/name"},
		directive.KV{Key: "source", Value: "releases"},
	))
	require.NoError(t, err)
	candidates, err := provider.Discover(t.Context(), res)
	require.NoError(t, err)

	require.Len(t, candidates, 2, "a flagged prerelease is still discovered, just marked")
	byTag := map[string]bool{}
	for _, c := range candidates {
		byTag[c.Version] = c.Prerelease
	}
	require.False(t, byTag["v2.0.0"], "stable release not flagged")
	require.True(t, byTag["v2.1.0"], "flagged release carries Prerelease")
}

func TestDiscoverReleasesUnmatchedTagHasNoCommit(t *testing.T) {
	t.Parallel()

	const releases = `[
		{"tag_name": "v9.9.9", "published_at": "2026-01-02T03:04:05Z", "draft": false}
	]`
	const tags = `[
		{"name": "v2.0.0", "commit": {"sha": "deadbeef"}}
	]`
	provider := github.New(github.WithTransport(routeTransport(map[string]string{
		"/releases": releases,
		"/tags":     tags,
	})))

	res, err := provider.Resource(directiveOf(
		directive.KV{Key: "repository", Value: "owner/name"},
		directive.KV{Key: "source", Value: "releases"},
	))
	require.NoError(t, err)
	candidates, err := provider.Discover(t.Context(), res)
	require.NoError(t, err)

	require.Len(t, candidates, 1)
	require.Empty(
		t,
		candidates[0].Commit,
		"a release tag outside the tags page resolves to no commit",
	)
}
