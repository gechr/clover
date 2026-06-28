package gitlab_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/provider/gitlab"
	"github.com/stretchr/testify/require"
)

// stubStore is a tokenStore returning a fixed value.
type stubStore struct {
	token string
	ok    bool
}

func (s stubStore) Get(string) (string, bool) { return s.token, s.ok }

// TestAuthenticateWithStoredToken confirms a stored token satisfies
// Authenticate. A test transport pins the credential to the explicit store, so it
// is hermetic regardless of the machine's keychain or environment.
func TestAuthenticateWithStoredToken(t *testing.T) {
	t.Parallel()

	p := gitlab.New(
		gitlab.WithStore(stubStore{token: "stored", ok: true}),
		gitlab.WithTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, "[]"), nil
		})),
	)
	require.NoError(t, p.Authenticate(context.Background()))
}

// TestAuthenticateAnonymous confirms an absent credential reports the anonymous
// fallback rather than a hard failure.
func TestAuthenticateAnonymous(t *testing.T) {
	t.Parallel()

	p := gitlab.New(
		gitlab.WithStore(stubStore{ok: false}),
		gitlab.WithTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, "[]"), nil
		})),
	)
	require.Error(t, p.Authenticate(context.Background()))
}

// TestAuthorizationHeaderIsBearer locks the auth header to Authorization: Bearer,
// the one form GitLab accepts for both an OAuth token (device flow) and a personal
// access token. PRIVATE-TOKEN works only for PATs, so a stored OAuth token sent
// that way 401s - the bug this test guards against.
func TestAuthorizationHeaderIsBearer(t *testing.T) {
	t.Parallel()

	var auth string
	p := gitlab.New(
		gitlab.WithStore(stubStore{token: "oauth-token", ok: true}),
		gitlab.WithTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			auth = req.Header.Get("Authorization")
			require.Empty(t, req.Header.Get("Private-Token"))
			return jsonResponse(req, "[]"), nil
		})),
	)

	_, err := p.Discover(t.Context(), resourceFor(t, p,
		directive.KV{Key: "repository", Value: "group/project"},
	))
	require.NoError(t, err)
	require.Equal(t, "Bearer oauth-token", auth)
}

// TestExpiredTokenFallsBackToAnonymous covers an expired or revoked stored token:
// the first credentialed request 401s, and the provider retries once without the
// credential so a public read still succeeds rather than failing outright.
func TestExpiredTokenFallsBackToAnonymous(t *testing.T) {
	t.Parallel()

	var attempts int
	p := gitlab.New(
		gitlab.WithStore(stubStore{token: "expired", ok: true}),
		gitlab.WithTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			if req.Header.Get("Authorization") != "" {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Status:     "401 Unauthorized",
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader(`{"message":"401 Unauthorized"}`)),
					Request:    req,
				}, nil
			}
			return jsonResponse(req, `[{"name":"v1.0.0","commit":{"id":"abc"}}]`), nil
		})),
	)

	got, err := p.Discover(t.Context(), resourceFor(t, p,
		directive.KV{Key: "repository", Value: "group/project"},
	))
	require.NoError(t, err)
	require.Equal(t, []string{"v1.0.0"}, versions(got))
	require.Equal(t, 2, attempts) // credentialed 401, then anonymous retry
}

// TestPATBoundToHost covers the exfiltration guard: the host-independent PAT is
// sent to the default host but withheld from a marker that names a different
// host, so a marker-controlled host= cannot redirect the token to an attacker.
func TestPATBoundToHost(t *testing.T) {
	t.Parallel()

	var auth string
	capture := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		auth = req.Header.Get("Authorization")
		return jsonResponse(req, "[]"), nil
	})

	def := gitlab.New(gitlab.WithToken("secret"), gitlab.WithTransport(capture))
	_, err := def.Discover(t.Context(), resourceFor(t, def,
		directive.KV{Key: "repository", Value: "group/project"},
	))
	require.NoError(t, err)
	require.Equal(t, "Bearer secret", auth, "PAT is sent to the default host")

	auth = ""
	foreign := gitlab.New(gitlab.WithToken("secret"), gitlab.WithTransport(capture))
	_, err = foreign.Discover(t.Context(), resourceFor(t, foreign,
		directive.KV{Key: "repository", Value: "group/project"},
		directive.KV{Key: "host", Value: "gitlab.example.com"},
	))
	require.NoError(t, err)
	require.Empty(t, auth, "PAT must not be sent to a non-default host")
}

func TestAuthHint(t *testing.T) {
	t.Parallel()
	require.Equal(t,
		"for higher rate limits and private projects, "+
			"run `clover login gitlab` or set `CLOVER_GITLAB_TOKEN`",
		gitlab.New().AuthHint(),
	)
}
