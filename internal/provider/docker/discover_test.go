package docker_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/docker"
	"github.com/stretchr/testify/require"
)

func TestDiscoverHub(t *testing.T) {
	t.Parallel()

	const body = `{"results":[
		{"name":"1.27.0","last_updated":"2026-01-02T03:04:05Z"},
		{"name":"1.28.0","last_updated":"2026-02-02T03:04:05Z"},
		{"name":"latest","last_updated":"2026-03-02T03:04:05Z"}
	]}`

	var path string
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		path = req.URL.Path
		return jsonResponse(req, body), nil
	})
	p := docker.New(docker.WithTransport(transport), anon())

	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "nginx"}))
	require.NoError(t, err)
	candidates, err := p.Discover(t.Context(), res)
	require.NoError(t, err)

	require.Contains(t, path, "/v2/repositories/library/nginx/tags")
	require.Len(t, candidates, 3)
	require.Equal(t, "1.27.0", candidates[0].Version)
	require.NotNil(t, candidates[0].Semver)
	require.False(t, candidates[0].PublishedAt.IsZero(), "the hub API supplies dates")
	require.Nil(t, candidates[2].Semver, "a non-semver tag yields a nil Semver")
}

func TestDiscoverHubVariantTagIsNotPrerelease(t *testing.T) {
	t.Parallel()

	const body = `{"results":[{"name":"1.27-alpine","last_updated":"2026-01-02T03:04:05Z"}]}`
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, body), nil
	})
	p := docker.New(docker.WithTransport(transport), anon())

	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "nginx"}))
	require.NoError(t, err)
	candidates, err := p.Discover(t.Context(), res)
	require.NoError(t, err)

	require.Len(t, candidates, 1)
	require.Equal(t, "1.27-alpine", candidates[0].Version, "the raw tag is preserved for rendering")
	require.NotNil(t, candidates[0].Semver)
	require.Empty(t, candidates[0].Semver.Prerelease(),
		"a variant suffix is stripped before parsing, so it is not a prerelease")
}

func TestDiscoverRegistryBearerChallenge(t *testing.T) {
	t.Parallel()

	var gotScope string
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			gotScope = req.URL.Query().Get("scope")
			require.Equal(t, "example.com", req.URL.Query().Get("service"))
			return jsonResponse(req, `{"token":"abc"}`), nil

		case strings.Contains(req.URL.Path, "/tags/list"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			require.Equal(t, "Bearer abc", req.Header.Get("Authorization"))
			return jsonResponse(req, `{"tags":["1.0.0","1.2.0","latest"]}`), nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})
	p := docker.New(docker.WithTransport(transport), anon())

	res, err := p.Resource(directiveOf(
		directive.KV{Key: "repository", Value: "team/img"},
		directive.KV{Key: "registry", Value: "registry.example.com"},
	))
	require.NoError(t, err)
	candidates, err := p.Discover(t.Context(), res)
	require.NoError(t, err)

	require.Equal(t, "repository:team/img:pull,push", gotScope,
		"the comma-bearing scope from the challenge survives parsing")
	require.Len(t, candidates, 3)
	require.Equal(t, "1.0.0", candidates[0].Version)
	require.NotNil(t, candidates[0].Semver)
	require.True(t, candidates[0].PublishedAt.IsZero(), "the registry tags list has no dates")
}

func TestDiscoverHubRequestsNewestFirst(t *testing.T) {
	t.Parallel()

	var gotQuery string
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotQuery = req.URL.RawQuery
		return jsonResponse(req, `{"results":[{"name":"1.0.0"}]}`), nil
	})
	p := docker.New(docker.WithTransport(transport), anon())

	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "nginx"}))
	require.NoError(t, err)
	_, err = p.Discover(t.Context(), res)
	require.NoError(t, err)

	// The Hub API returns newest-first for the bare field; -last_updated is oldest.
	require.Contains(t, gotQuery, "ordering=last_updated")
	require.NotContains(t, gotQuery, "ordering=-last_updated")
}

func TestDiscoverHubDeepPaginates(t *testing.T) {
	t.Parallel()

	const page1 = `{"next":"https://hub.docker.com/v2/repositories/library/nginx/tags?page=2","results":[
		{"name":"1.1.0"},{"name":"1.0.0"}
	]}`
	const page2 = `{"next":null,"results":[{"name":"0.9.0"}]}`

	var pages []string
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		page := req.URL.Query().Get("page")
		pages = append(pages, page)
		if page == "2" {
			return jsonResponse(req, page2), nil
		}
		return jsonResponse(req, page1), nil
	})
	p := docker.New(docker.WithTransport(transport), anon())

	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "nginx"}))
	require.NoError(t, err)
	candidates, err := p.Discover(provider.WithDeep(t.Context(), true), res)
	require.NoError(t, err)

	require.Len(t, candidates, 3)
	require.Equal(t, []string{"", "2"}, pages, "a deep lookup follows the next URL")
}

func TestDiscoverRegistryDeepPaginates(t *testing.T) {
	t.Parallel()

	var tagsCalls int
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			return jsonResponse(req, `{"token":"abc"}`), nil

		case strings.Contains(req.URL.Path, "/tags/list"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			tagsCalls++
			if req.URL.Query().Get("last") == "" {
				resp := jsonResponse(req, `{"tags":["1.0.0","1.1.0"]}`)
				resp.Header.Set("Link", `</v2/team/img/tags/list?n=100&last=1.1.0>; rel="next"`)
				return resp, nil
			}
			return jsonResponse(req, `{"tags":["1.2.0"]}`), nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})
	p := docker.New(docker.WithTransport(transport), anon())

	res, err := p.Resource(directiveOf(
		directive.KV{Key: "repository", Value: "team/img"},
		directive.KV{Key: "registry", Value: "registry.example.com"},
	))
	require.NoError(t, err)

	var truncated []string
	ctx := provider.WithTruncationSink(provider.WithDeep(t.Context(), true),
		func(r string) { truncated = append(truncated, r) })
	candidates, err := p.Discover(ctx, res)
	require.NoError(t, err)

	require.Len(t, candidates, 3)
	require.Equal(t, 2, tagsCalls, "a deep lookup follows the Link header to the next page")
	require.Empty(t, truncated, "a deep lookup exhausts the pages, so nothing is truncated")
}

func TestDiscoverRegistryShallowNotesTruncation(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			return jsonResponse(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/tags/list"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			resp := jsonResponse(req, `{"tags":["1.0.0","1.1.0"]}`)
			resp.Header.Set("Link", `</v2/team/img/tags/list?n=100&last=1.1.0>; rel="next"`)
			return resp, nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})
	p := docker.New(docker.WithTransport(transport), anon())

	res, err := p.Resource(directiveOf(
		directive.KV{Key: "repository", Value: "team/img"},
		directive.KV{Key: "registry", Value: "registry.example.com"},
	))
	require.NoError(t, err)

	var truncated []string
	ctx := provider.WithTruncationSink(
		t.Context(),
		func(r string) { truncated = append(truncated, r) },
	)
	_, err = p.Discover(ctx, res) // shallow: a next page exists but is not fetched
	require.NoError(t, err)
	require.Equal(t, []string{"registry.example.com/team/img"}, truncated,
		"a shallow lookup with more pages notes the truncation")
}

// challengeResponse is a 401 advertising a bearer-token realm, the way a
// registry answers an unauthenticated tags request.
func challengeResponse(req *http.Request) *http.Response {
	header := http.Header{}
	header.Set(
		"WWW-Authenticate",
		`Bearer realm="https://auth.example.com/token",service="example.com",scope="repository:team/img:pull,push"`,
	)
	return &http.Response{
		StatusCode: http.StatusUnauthorized,
		Header:     header,
		Body:       http.NoBody,
		Request:    req,
	}
}
