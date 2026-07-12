package gitlab_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/gitlab"
	"github.com/stretchr/testify/require"
)

// fixture returns the verbatim API capture stored under testdata.
func fixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(body)
}

func TestVerifyHelpers(t *testing.T) {
	t.Parallel()

	// Captures from gitlab-org/gitlab-runner: two release branches, and the
	// merge base of main and one of main's own commits (the commit itself).
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch path := req.URL.Path; {
		case strings.Contains(path, "/repository/branches"):
			return jsonResponse(req, fixture(t, "branches.json")), nil
		case strings.Contains(path, "/repository/merge_base"):
			return jsonResponse(req, fixture(t, "merge-base-ancestor.json")), nil
		default: // projects/{id}
			return jsonResponse(req, `{"default_branch": "main"}`), nil
		}
	})
	p := gitlab.New(gitlab.WithTransport(transport))
	res := resourceFor(t, p, directive.KV{Key: "repository", Value: "gitlab-org/gitlab-runner"})

	def, err := p.DefaultBranch(t.Context(), res)
	require.NoError(t, err)
	require.Equal(t, "main", def)

	branches, err := p.Branches(t.Context(), res)
	require.NoError(t, err)
	require.Equal(t, []provider.Branch{
		{Name: "0-0-stable", Tip: "f23a72db4bcc895f3340f3ba4a7e9aeef33a6a76"},
		{Name: "0-6-stable", Tip: "3227f0aa5be1d64d2ec694bd3758e0d43e92b36b"},
	}, branches)

	// The merge base equals the commit exactly when the branch contains it.
	reachable, err := p.Reachable(
		t.Context(), res, "main", "fb8d1bcf27374392c09c4b205c425fa214685b14")
	require.NoError(t, err)
	require.True(t, reachable)

	reachable, err = p.Reachable(
		t.Context(), res, "main", "f23a72db4bcc895f3340f3ba4a7e9aeef33a6a76")
	require.NoError(t, err)
	require.False(t, reachable, "a diverged commit's merge base is an older ancestor")
}

func TestReachableNoCommonHistory(t *testing.T) {
	t.Parallel()

	// merge_base 404s when the refs share no common ancestor, a definitive
	// negative rather than an API error.
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     "404 Not Found",
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"message": "404 Not Found"}`)),
			Request:    req,
		}, nil
	})
	p := gitlab.New(gitlab.WithTransport(transport))
	res := resourceFor(t, p, directive.KV{Key: "repository", Value: "owner/name"})

	reachable, err := p.Reachable(t.Context(), res, "main", "abc123")
	require.NoError(t, err)
	require.False(t, reachable, "no shared history means the commit is not on the branch")
}

func TestCredentialed(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, "{}"), nil
	})

	with := gitlab.New(gitlab.WithTransport(transport), gitlab.WithToken("tok"))
	require.True(
		t,
		with.Credentialed(
			resourceFor(t, with, directive.KV{Key: "repository", Value: "owner/name"}),
		),
	)

	without := gitlab.New(gitlab.WithTransport(transport))
	require.False(
		t,
		without.Credentialed(
			resourceFor(t, without, directive.KV{Key: "repository", Value: "owner/name"}),
		),
	)
}
