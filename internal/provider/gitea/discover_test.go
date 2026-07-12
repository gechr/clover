package gitea_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/gitea"
	xslices "github.com/gechr/x/slices"
	"github.com/stretchr/testify/require"
)

func directiveOf(pairs ...directive.KV) directive.Directive {
	return directive.Directive{Pairs: pairs}
}

// roundTripFunc adapts a function to an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// jsonResponse builds a 200 JSON response. A non-empty linkNext sets a Link
// header advertising a next page, mimicking Gitea's pagination.
func jsonResponse(req *http.Request, body, linkNext string) *http.Response {
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	if linkNext != "" {
		header.Set("Link", "<"+linkNext+`>; rel="next"`)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

// resourceFor builds a validated resource for the given directive pairs,
// defaulting the required repository when a test does not exercise it.
func resourceFor(t *testing.T, p *gitea.Provider, pairs ...directive.KV) provider.Resource {
	t.Helper()
	if !hasKey(pairs, "repository") {
		pairs = append([]directive.KV{{Key: "repository", Value: "owner/name"}}, pairs...)
	}
	res, err := p.Resource(directiveOf(pairs...))
	require.NoError(t, err)
	return res
}

func hasKey(pairs []directive.KV, key string) bool {
	for _, p := range pairs {
		if p.Key == key {
			return true
		}
	}
	return false
}

// newProvider returns a provider whose transport serves tagsBody for a /tags
// request and releasesBody for a /releases request.
func newProvider(tagsBody, releasesBody string) *gitea.Provider {
	return gitea.New(gitea.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/releases") {
				return jsonResponse(req, releasesBody, ""), nil
			}
			return jsonResponse(req, tagsBody, ""), nil
		}),
	))
}

// versions extracts the version strings from candidates, in order.
func versions(candidates []model.Candidate) []string {
	return xslices.Map(candidates, func(c model.Candidate) string { return c.Version })
}

// TestDiscoverTags covers a tag listing: every tag surfaces in the API's order,
// carrying its commit SHA but no publication date - Gitea reports none for a tag,
// so cooldown is left inert rather than aged by the target commit.
func TestDiscoverTags(t *testing.T) {
	t.Parallel()

	const body = `[
		{"name": "v2.0.0", "commit": {"sha": "aaa"}},
		{"name": "v1.9.0", "commit": {"sha": "bbb"}}
	]`

	p := newProvider(body, "")
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"v2.0.0", "v1.9.0"}, versions(got))
	require.Equal(t, "aaa", got[0].Commit)
	require.True(t, got[0].PublishedAt.IsZero())
}

// TestDiscoverReleasesSkipsDraft covers a release listing: a draft release is
// unpublished and dropped, a published one keeps its date and assets. Commit is
// left empty - target_commitish is not a reliable SHA - even when present.
func TestDiscoverReleasesSkipsDraft(t *testing.T) {
	t.Parallel()

	const body = `[
		{"tag_name": "v3.0.0", "draft": true},
		{"tag_name": "v2.0.0", "draft": false, "target_commitish": "main",
		 "published_at": "2026-06-10T08:11:24+02:00",
		 "assets": [{"name": "tool-linux-amd64", "browser_download_url": "https://example/tool"}]}
	]`

	p := newProvider("", body)
	got, err := p.Discover(
		t.Context(),
		resourceFor(t, p, directive.KV{Key: "source", Value: "releases"}),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"v2.0.0"}, versions(got))
	require.Empty(t, got[0].Commit, "target_commitish must not be reported as a commit SHA")
	require.False(t, got[0].PublishedAt.IsZero())
	require.Equal(
		t,
		[]model.Asset{{Name: "tool-linux-amd64", URL: "https://example/tool"}},
		got[0].Assets,
	)
}

// TestDiscoverReleasesPrerelease covers the out-of-band prerelease flag being
// carried onto the candidate, so selection can exclude a release marked
// prerelease even when its tag looks stable.
func TestDiscoverReleasesPrerelease(t *testing.T) {
	t.Parallel()

	const body = `[
		{"tag_name": "v2.0.0", "prerelease": true},
		{"tag_name": "v1.0.0", "prerelease": false}
	]`

	p := newProvider("", body)
	got, err := p.Discover(
		t.Context(),
		resourceFor(t, p, directive.KV{Key: "source", Value: "releases"}),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"v2.0.0", "v1.0.0"}, versions(got))
	require.True(t, got[0].Prerelease)
	require.False(t, got[1].Prerelease)
}

// TestDiscoverError covers a non-200 response surfacing as an error.
func TestDiscoverError(t *testing.T) {
	t.Parallel()

	p := gitea.New(gitea.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Body:       io.NopCloser(strings.NewReader("not found")),
				Request:    req,
			}, nil
		}),
	))

	_, err := p.Discover(t.Context(), resourceFor(t, p))
	require.Error(t, err)
}

// TestDiscoverInvalidResource covers Discover rejecting a foreign resource type.
func TestDiscoverInvalidResource(t *testing.T) {
	t.Parallel()

	_, err := gitea.New().Discover(t.Context(), "not-a-resource")
	require.EqualError(t, err, "gitea: invalid resource string")
}

// TestShallowTruncation covers a shallow lookup stopping at the first page when a
// Link header advertises more: the truncation sink fires, but only one page is
// read.
func TestShallowTruncation(t *testing.T) {
	t.Parallel()

	var pages int
	p := gitea.New(gitea.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			pages++
			return jsonResponse(req, `[{"name": "v1.0.0", "commit": {"sha": "a"}}]`,
				"https://codeberg.org/api/v1/next"), nil
		}),
	))

	var truncated *provider.Truncation
	ctx := provider.WithTruncationSink(
		t.Context(),
		func(tr provider.Truncation) { truncated = &tr },
	)
	got, err := p.Discover(ctx, resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"v1.0.0"}, versions(got))
	require.Equal(t, 1, pages)
	require.NotNil(t, truncated)
}

// TestDeepFollowsPages covers a deep lookup following the Link header to
// exhaustion, aggregating every page.
func TestDeepFollowsPages(t *testing.T) {
	t.Parallel()

	p := gitea.New(gitea.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.RawQuery, "page=2") {
				return jsonResponse(req, `[{"name": "v1.0.0", "commit": {"sha": "b"}}]`, ""), nil
			}
			return jsonResponse(req, `[{"name": "v2.0.0", "commit": {"sha": "a"}}]`,
				"https://codeberg.org/api/v1/repos/o/n/tags?page=2"), nil
		}),
	))

	ctx := provider.WithDeep(t.Context(), true)
	got, err := p.Discover(ctx, resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"v2.0.0", "v1.0.0"}, versions(got))
}

// TestAuthHeader covers an injected token attaching Gitea's token-scheme
// Authorization header to every request.
func TestAuthHeader(t *testing.T) {
	t.Parallel()

	var auth string
	p := gitea.New(
		gitea.WithToken("secret"),
		gitea.WithTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			auth = req.Header.Get("Authorization")
			return jsonResponse(req, `[]`, ""), nil
		})),
	)

	_, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, "token secret", auth)
}

// TestPATBoundToHost covers the exfiltration guard: a PAT is sent to the default
// flavor's host but withheld from a marker naming a different flavor, so a
// marker-controlled flavor= cannot redirect the token to another instance.
func TestPATBoundToHost(t *testing.T) {
	t.Parallel()

	var auth string
	capture := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		auth = req.Header.Get("Authorization")
		return jsonResponse(req, `[]`, ""), nil
	})

	def := gitea.New(gitea.WithToken("secret"), gitea.WithTransport(capture))
	_, err := def.Discover(t.Context(), resourceFor(t, def))
	require.NoError(t, err)
	require.Equal(t, "token secret", auth, "PAT is sent to the default flavor's host")

	auth = ""
	foreign := gitea.New(gitea.WithToken("secret"), gitea.WithTransport(capture))
	_, err = foreign.Discover(t.Context(),
		resourceFor(t, foreign, directive.KV{Key: "flavor", Value: "gitea"}))
	require.NoError(t, err)
	require.Empty(t, auth, "PAT must not be sent to a non-default flavor")
}

// TestDeepStopsAtForeignOrigin covers the pagination guard: a deep lookup does
// not follow a Link next URL to a different origin, so the credential cannot leak
// off the starting host.
func TestDeepStopsAtForeignOrigin(t *testing.T) {
	t.Parallel()

	var pages int
	p := gitea.New(
		gitea.WithTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			pages++
			return jsonResponse(req, `[{"name": "v1.0.0", "commit": {"sha": "a"}}]`,
				"https://attacker.example/api/v1/next"), nil
		})),
	)

	got, err := p.Discover(provider.WithDeep(t.Context(), true), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"v1.0.0"}, versions(got))
	require.Equal(t, 1, pages, "must not follow a next link to a foreign origin")
}
