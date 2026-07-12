package github_test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
		case strings.Contains(path, "/git/ref/tags/"):
			return jsonResponse(req, `{"object": {"type": "commit", "sha": "abc123"}}`), nil
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

	// Commit resolves a lightweight tag straight to the commit it points at.
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

func TestCommitPeelsAnnotatedTag(t *testing.T) {
	t.Parallel()

	// An annotated tag's ref points at a tag object; Commit must follow it to
	// the commit it wraps rather than report the tag object's own SHA.
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch path := req.URL.Path; {
		case strings.Contains(path, "/git/ref/tags/"):
			return jsonResponse(req, `{"object": {"type": "tag", "sha": "tagobj1"}}`), nil
		case strings.Contains(path, "/git/tags/tagobj1"):
			return jsonResponse(req, `{"object": {"sha": "abc123"}}`), nil
		default:
			return nil, fmt.Errorf("unexpected path %s", path)
		}
	})
	p := github.New(github.WithTransport(transport))
	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "owner/name"}))
	require.NoError(t, err)

	sha, err := p.Commit(t.Context(), res, "v1.2.0")
	require.NoError(t, err)
	require.Equal(t, "abc123", sha)
}

// fixture returns the verbatim API capture stored under testdata.
func fixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(body)
}

// fixtureProvider serves each capture whose key is a substring of the request
// path, so one provider answers the multi-request signature lookups.
func fixtureProvider(t *testing.T, routes map[string]string) (*github.Provider, provider.Resource) {
	t.Helper()
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		for key, name := range routes {
			if strings.Contains(req.URL.Path, key) {
				return jsonResponse(req, fixture(t, name)), nil
			}
		}
		return nil, fmt.Errorf("no route for %s", req.URL.Path)
	})
	p := github.New(github.WithTransport(transport))
	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "owner/name"}))
	require.NoError(t, err)
	return p, res
}

func TestSignedTagAnnotated(t *testing.T) {
	t.Parallel()

	// A signed annotated tag carries its own verification (gechr/clover v0.3.9).
	p, res := fixtureProvider(t, map[string]string{
		"/git/ref/tags/": "tag-ref-annotated.json",
		"/git/tags/":     "tag-object-signed.json",
	})
	verified, reason, err := p.SignedTag(t.Context(), res, "v0.3.9")
	require.NoError(t, err)
	require.True(t, verified)
	require.Equal(t, "valid", reason)
}

func TestSignedTagLightweight(t *testing.T) {
	t.Parallel()

	// A lightweight tag defers to the pointed-at commit's verification
	// (actions/checkout v4.2.2).
	p, res := fixtureProvider(t, map[string]string{
		"/git/ref/tags/": "tag-ref-lightweight.json",
		"/commits/":      "commit-verified.json",
	})
	verified, reason, err := p.SignedTag(t.Context(), res, "v4.2.2")
	require.NoError(t, err)
	require.True(t, verified)
	require.Equal(t, "valid", reason)
}

func TestSignedTagUnsigned(t *testing.T) {
	t.Parallel()

	// An unsigned annotated tag reports the reason upstream gives
	// (kubernetes/kubernetes v1.30.0).
	p, res := fixtureProvider(t, map[string]string{
		"/git/ref/tags/": "tag-ref-annotated.json",
		"/git/tags/":     "tag-object-unsigned.json",
	})
	verified, reason, err := p.SignedTag(t.Context(), res, "v1.30.0")
	require.NoError(t, err)
	require.False(t, verified)
	require.Equal(t, "unsigned", reason)
}

func TestReachableNoCommonHistory(t *testing.T) {
	t.Parallel()

	// The compare endpoint 404s when the refs share no common ancestor - the
	// impostor-commit case - which is a definitive negative, not an API error.
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
	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "owner/name"}))
	require.NoError(t, err)

	reachable, err := p.Reachable(t.Context(), res, "main", "abc123")
	require.NoError(t, err)
	require.False(t, reachable, "no shared history means the commit is not on the branch")
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
