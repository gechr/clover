package github_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/github"
	"github.com/stretchr/testify/require"
)

func TestVerifyHelpers(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch path := req.URL.Path; {
		case strings.Contains(path, "/commits/"):
			return jsonResponse(req, `{"sha": "abc123"}`), nil
		case strings.Contains(path, "/branches"):
			return jsonResponse(req, `[{"name":"main","commit":{"sha":"tip1"}},`+
				`{"name":"release-1.2","commit":{"sha":"tip2"}}]`), nil
		case strings.Contains(path, "/compare/"):
			return jsonResponse(req, `{"status": "behind"}`), nil
		default: // repos/{owner}/{name}
			return jsonResponse(req, `{"default_branch": "main"}`), nil
		}
	})
	p := github.New(github.WithTransport(transport))
	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "owner/name"}))
	require.NoError(t, err)

	// Commit resolves a tag to its (peeled) commit SHA.
	sha, err := p.Commit(t.Context(), res, "v1.2.0")
	require.NoError(t, err)
	require.Equal(t, "abc123", sha)

	def, err := p.DefaultBranch(t.Context(), res)
	require.NoError(t, err)
	require.Equal(t, "main", def)

	branches, err := p.Branches(t.Context(), res)
	require.NoError(t, err)
	require.Equal(t, []provider.Branch{
		{Name: "main", Tip: "tip1"},
		{Name: "release-1.2", Tip: "tip2"},
	}, branches)

	// "behind" means the commit is an ancestor of the branch tip, so it is reachable.
	reachable, err := p.Reachable(t.Context(), res, "main", "abc123")
	require.NoError(t, err)
	require.True(t, reachable)
}

func TestReachableRejectsDivergedStatus(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, `{"status": "diverged"}`), nil
	})
	p := github.New(github.WithTransport(transport))
	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "owner/name"}))
	require.NoError(t, err)

	reachable, err := p.Reachable(t.Context(), res, "main", "abc123")
	require.NoError(t, err)
	require.False(t, reachable, "a diverged commit is not on the branch")
}
