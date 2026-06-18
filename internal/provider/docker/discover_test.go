package docker_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
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
