package gitea_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/gitea"
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

	// Captures from forgejo/forgejo on codeberg.org: two branches, and the
	// compare of the default branch against one of its own commits (zero
	// commits ahead).
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch path := req.URL.Path; {
		case strings.Contains(path, "/branches"):
			return jsonResponse(req, fixture(t, "branches.json"), ""), nil
		case strings.Contains(path, "/compare/"):
			return jsonResponse(req, fixture(t, "compare-ancestor.json"), ""), nil
		default: // repos/{owner}/{name}
			return jsonResponse(req, `{"default_branch": "forgejo"}`, ""), nil
		}
	})
	p := gitea.New(gitea.WithTransport(transport))
	res := resourceFor(t, p)

	def, err := p.DefaultBranch(t.Context(), res)
	require.NoError(t, err)
	require.Equal(t, "forgejo", def)

	branches, err := p.Branches(t.Context(), res)
	require.NoError(t, err)
	require.Equal(t, []provider.Branch{
		{
			Name: "renovate/forgejo-github.com-urfave-cli-v3-3.x",
			Tip:  "52740963bf846364ef51ec7da2f90914aeec6851",
		},
		{
			Name: "renovate/forgejo-github.com-alecthomas-chroma-v2-2.x",
			Tip:  "e234bc6d01fcd89d37342ae12156e307d2bde7ff",
		},
	}, branches)

	// Zero commits ahead of the branch means the branch contains the commit.
	reachable, err := p.Reachable(
		t.Context(), res, "forgejo", "052b6ed8f508113c063bb2127e755e14872d6abf")
	require.NoError(t, err)
	require.True(t, reachable)
}

func TestReachableAheadOfBranch(t *testing.T) {
	t.Parallel()

	// A compare with commits ahead of the base means the branch does not
	// contain the commit.
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, `{"total_commits":1,"commits":[{}],"files":[]}`, ""), nil
	})
	p := gitea.New(gitea.WithTransport(transport))
	res := resourceFor(t, p)

	reachable, err := p.Reachable(t.Context(), res, "forgejo", "abc123")
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
			Body:       io.NopCloser(strings.NewReader(`{"message": "GetCommit"}`)),
			Request:    req,
		}, nil
	})
	p := gitea.New(gitea.WithTransport(transport))
	res := resourceFor(t, p)

	reachable, err := p.Reachable(t.Context(), res, "forgejo", "abc123")
	require.NoError(t, err)
	require.False(t, reachable, "an unknown object is not on any branch")
}

func TestCredentialed(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, "{}", ""), nil
	})

	with := gitea.New(gitea.WithTransport(transport), gitea.WithToken("tok"))
	require.True(t, with.Credentialed(resourceFor(t, with)))

	without := gitea.New(gitea.WithTransport(transport))
	require.False(t, without.Credentialed(resourceFor(t, without)))
}
