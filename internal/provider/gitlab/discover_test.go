package gitlab_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/gitlab"
	xslices "github.com/gechr/x/slices"
	"github.com/stretchr/testify/require"
)

// roundTripFunc adapts a function to an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

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

// resourceFor builds a validated resource for the given directive pairs.
func resourceFor(t *testing.T, p *gitlab.Provider, pairs ...directive.KV) provider.Resource {
	t.Helper()
	res, err := p.Resource(directiveOf(pairs...))
	require.NoError(t, err)
	return res
}

// newProvider returns a provider whose transport serves the given body for every
// request, recording the requests it receives.
func newProvider(body string, seen *[]*http.Request) *gitlab.Provider {
	return gitlab.New(gitlab.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if seen != nil {
				*seen = append(*seen, req)
			}
			return jsonResponse(req, body), nil
		}),
	))
}

// versions extracts the version strings from candidates, in order.
func versions(candidates []model.Candidate) []string {
	return xslices.Map(candidates, func(c model.Candidate) string { return c.Version })
}

// TestDiscoverTagsRequest covers the tags request URL: the project path is
// encoded with %2F separators and the listing is ordered highest-version-first.
func TestDiscoverTagsRequest(t *testing.T) {
	t.Parallel()

	var seen []*http.Request
	p := newProvider(`[{"name":"v2.0.0","commit":{"id":"abc"}}]`, &seen)
	_, err := p.Discover(t.Context(), resourceFor(t, p,
		directive.KV{Key: "repository", Value: "group/subgroup/project"},
	))
	require.NoError(t, err)

	require.Len(t, seen, 1)
	require.Equal(t,
		"/api/v4/projects/group%2Fsubgroup%2Fproject/repository/tags",
		seen[0].URL.EscapedPath(),
	)
	require.Equal(t, "version", seen[0].URL.Query().Get("order_by"))
	require.Equal(t, "desc", seen[0].URL.Query().Get("sort"))
}

// TestDiscoverTagsQualifierFiltersServerSide covers the qualifier hint: a hinted
// lookup narrows the tag listing with search=, an unhinted one stays unfiltered.
func TestDiscoverTagsQualifierFiltersServerSide(t *testing.T) {
	t.Parallel()

	var seen []*http.Request
	p := newProvider(`[{"name":"v2.0.0-ent","commit":{"id":"abc"}}]`, &seen)
	res := resourceFor(t, p, directive.KV{Key: "repository", Value: "group/project"})

	_, err := p.Discover(provider.WithQualifier(t.Context(), "ent"), res)
	require.NoError(t, err)
	require.Len(t, seen, 1)
	require.Equal(t, "ent", seen[0].URL.Query().Get("search"),
		"a qualifier hint narrows the listing server-side")

	_, err = p.Discover(t.Context(), res)
	require.NoError(t, err)
	require.Len(t, seen, 2)
	require.False(t, seen[1].URL.Query().Has("search"), "no hint leaves the listing unfiltered")

	ctx := provider.WithQualifier(t.Context(), "ent")
	_, err = p.Discover(provider.WithTagPrefix(ctx, "api/"), res)
	require.NoError(t, err)
	require.Len(t, seen, 3)
	require.Equal(t, "^api/", seen[2].URL.Query().Get("search"),
		"the tag-prefix wins over the qualifier and anchors the search")
}

// TestDiscoverRoutesSelfManagedHost covers a self-managed host: the request
// targets https://<host>/api/v4 rather than gitlab.com, on the same /api/v4
// surface.
func TestDiscoverRoutesSelfManagedHost(t *testing.T) {
	t.Parallel()

	var seen []*http.Request
	p := newProvider(`[]`, &seen)
	_, err := p.Discover(t.Context(), resourceFor(t, p,
		directive.KV{Key: "repository", Value: "group/project"},
		directive.KV{Key: "host", Value: "gitlab.example.com"},
	))
	require.NoError(t, err)
	require.Len(t, seen, 1)
	require.Equal(t, "gitlab.example.com", seen[0].URL.Host)
	require.Equal(t,
		"/api/v4/projects/group%2Fproject/repository/tags",
		seen[0].URL.EscapedPath(),
	)
}

// TestDiscoverTagsCandidate covers a tag's projection: the commit SHA and the
// tag's own creation date (not the commit date) ride along on the candidate.
func TestDiscoverTagsCandidate(t *testing.T) {
	t.Parallel()

	const body = `[{
		"name": "v2.0.0",
		"created_at": "2026-06-25T11:31:53.000+00:00",
		"commit": {"id": "deadbeef", "committed_date": "2020-01-01T00:00:00.000+00:00"}
	}]`
	p := newProvider(body, nil)
	got, err := p.Discover(t.Context(), resourceFor(t, p,
		directive.KV{Key: "repository", Value: "group/project"},
	))
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "v2.0.0", got[0].Version)
	require.Equal(t, "v2.0.0", got[0].Ref)
	require.Equal(t, "deadbeef", got[0].Commit)
	require.NotNil(t, got[0].Semver)
	// The tag's created_at, not the (much older) commit date.
	require.Equal(t, 2026, got[0].PublishedAt.Year())
}

// TestDiscoverTagsNullCreatedAt covers a tag whose created_at is null (a
// lightweight tag): PublishedAt is the zero time, so cooldown is inert rather than
// reading the tag as falsely old from the commit date.
func TestDiscoverTagsNullCreatedAt(t *testing.T) {
	t.Parallel()

	const body = `[{"name":"v2.0.0","created_at":null,"commit":{"id":"abc"}}]`
	p := newProvider(body, nil)
	got, err := p.Discover(t.Context(), resourceFor(t, p,
		directive.KV{Key: "repository", Value: "group/project"},
	))
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Zero(t, got[0].PublishedAt)
}

// TestDiscoverReleasesSkipsUpcoming covers an upcoming (scheduled, unpublished)
// release: it is dropped, so a future release cannot be selected ahead of the
// latest real one.
func TestDiscoverReleasesSkipsUpcoming(t *testing.T) {
	t.Parallel()

	const body = `[
		{"tag_name": "v3.0.0", "upcoming_release": true, "commit": {"id": "future"}},
		{"tag_name": "v2.0.0", "upcoming_release": false, "commit": {"id": "real"}}
	]`
	p := newProvider(body, nil)
	got, err := p.Discover(t.Context(), resourceFor(t, p,
		directive.KV{Key: "repository", Value: "group/project"},
		directive.KV{Key: "source", Value: "releases"},
	))
	require.NoError(t, err)
	require.Equal(t, []string{"v2.0.0"}, versions(got))
}

// TestDiscoverReleasesSkipsBlankTags covers a release with no tag_name: it is
// dropped rather than surfaced as an empty candidate. The kept release's asset
// links project to candidate assets (with no digest, which GitLab does not
// supply).
func TestDiscoverReleasesSkipsBlankTags(t *testing.T) {
	t.Parallel()

	const body = `[
		{"tag_name": "", "commit": {"id": "x"}},
		{
			"tag_name": "v1.5.0",
			"released_at": "2026-06-25T21:12:13.237Z",
			"commit": {"id": "cafe"},
			"assets": {"links": [{"name": "bin.tar.gz", "url": "https://example.test/bin.tar.gz"}]}
		}
	]`
	p := newProvider(body, nil)
	got, err := p.Discover(t.Context(), resourceFor(t, p,
		directive.KV{Key: "repository", Value: "group/project"},
		directive.KV{Key: "source", Value: "releases"},
	))
	require.NoError(t, err)
	require.Equal(t, []string{"v1.5.0"}, versions(got))
	require.Equal(t, "cafe", got[0].Commit)
	require.Len(t, got[0].Assets, 1)
	require.Equal(t, "bin.tar.gz", got[0].Assets[0].Name)
	require.Empty(t, got[0].Assets[0].Digest)
}

// TestDiscoverShallowReadsOnePage covers the shallow default: even when the
// X-Next-Page header advertises more pages, discovery is not paged past, so it
// costs a single request.
func TestDiscoverShallowReadsOnePage(t *testing.T) {
	t.Parallel()

	var seen []*http.Request
	p := gitlab.New(
		gitlab.WithTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			seen = append(seen, req)
			resp := jsonResponse(req, `[{"name":"v1.0.0","commit":{"id":"c0"}}]`)
			resp.Header.Set("X-Next-Page", "2") // more pages remain
			return resp, nil
		})),
	)
	_, err := p.Discover(t.Context(), resourceFor(t, p,
		directive.KV{Key: "repository", Value: "group/project"},
	))
	require.NoError(t, err)
	require.Len(t, seen, 1)
}

// TestDiscoverDeepStopsAtVersionFloor covers the floor cutoff: the tag listing
// is version-ordered, so a deep walk ends on the page that reaches below the
// hinted current version instead of paging to exhaustion.
func TestDiscoverDeepStopsAtVersionFloor(t *testing.T) {
	t.Parallel()

	var seen []*http.Request
	p := gitlab.New(
		gitlab.WithTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			seen = append(seen, req)
			resp := jsonResponse(req,
				`[{"name":"v2.0.0","commit":{"id":"c2"}},{"name":"v0.9.0","commit":{"id":"c0"}}]`)
			resp.Header.Set("X-Next-Page", "2") // more pages remain
			return resp, nil
		})),
	)
	res := resourceFor(t, p, directive.KV{Key: "repository", Value: "group/project"})

	ctx := provider.WithDeep(t.Context(), true)
	got, err := p.Discover(provider.WithVersionFloor(ctx, "1.0.0"), res)
	require.NoError(t, err)
	require.Len(t, seen, 1, "the page reaching below the floor is the last fetched")
	require.Len(t, got, 2, "the stopping page's own tags are still candidates")
}

func TestDiscoverError(t *testing.T) {
	t.Parallel()

	p := gitlab.New(gitlab.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Body:       io.NopCloser(strings.NewReader("404 Project Not Found")),
				Request:    req,
			}, nil
		}),
	))

	_, err := p.Discover(t.Context(), resourceFor(t, p,
		directive.KV{Key: "repository", Value: "group/project"},
	))
	require.Error(t, err)
}

func TestDiscoverInvalidResource(t *testing.T) {
	t.Parallel()

	_, err := gitlab.New().Discover(t.Context(), "not-a-resource")
	require.Error(t, err)
}
