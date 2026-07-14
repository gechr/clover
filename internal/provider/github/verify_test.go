package github_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/github"
	"github.com/stretchr/testify/require"
)

// verifyResource builds the owner/name resource the verify helpers act on.
func verifyResource(t *testing.T, p *github.Provider) provider.Resource {
	t.Helper()
	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "owner/name"}))
	require.NoError(t, err)
	return res
}

func TestVerifyHelpers(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch path := req.URL.Path; {
		case strings.Contains(path, "/branches"):
			return jsonResponse(req, `[
				{"name": "main", "commit": {"sha": "aaa"}},
				{"name": "release/v2", "commit": {"sha": "bbb"}}
			]`), nil
		case strings.Contains(path, "/compare/"):
			return jsonResponse(req, `{"total_commits": 0}`), nil
		default: // repos/{owner}/{name}
			return jsonResponse(req, `{"default_branch": "main"}`), nil
		}
	})
	p := github.New(github.WithTransport(transport), github.WithToken("tok"))
	res := verifyResource(t, p)

	def, err := p.DefaultBranch(t.Context(), res)
	require.NoError(t, err)
	require.Equal(t, "main", def)

	branches, err := p.Branches(t.Context(), res)
	require.NoError(t, err)
	require.Equal(t, []provider.Branch{
		{Name: "main", Tip: "aaa"},
		{Name: "release/v2", Tip: "bbb"},
	}, branches)

	// Zero commits ahead of the branch means the branch contains the commit.
	reachable, err := p.Reachable(t.Context(), res, "main", "aaa")
	require.NoError(t, err)
	require.True(t, reachable)
}

func TestReachableAheadOfBranch(t *testing.T) {
	t.Parallel()

	// A compare with commits ahead of the base means the branch does not
	// contain the commit.
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, `{"total_commits": 1, "commits": [{}]}`), nil
	})
	p := github.New(github.WithTransport(transport))
	res := verifyResource(t, p)

	reachable, err := p.Reachable(t.Context(), res, "main", "abc123")
	require.NoError(t, err)
	require.False(t, reachable)
}

func TestReachableUnknownCommit(t *testing.T) {
	t.Parallel()

	// The compare endpoint 404s for an object the repository does not contain,
	// a definitive negative rather than an API error.
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     "404 Not Found",
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"message": "Not Found"}`)),
			Request:    req,
		}, nil
	})
	p := github.New(github.WithTransport(transport))
	res := verifyResource(t, p)

	reachable, err := p.Reachable(t.Context(), res, "main", "abc123")
	require.NoError(t, err)
	require.False(t, reachable, "an unknown object is not on any branch")
}

func TestCredentialed(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, "{}"), nil
	})

	with := github.New(github.WithTransport(transport), github.WithToken("tok"))
	require.True(t, with.Credentialed(verifyResource(t, with)))

	without := github.New(github.WithTransport(transport))
	require.False(t, without.Credentialed(verifyResource(t, without)))
}
