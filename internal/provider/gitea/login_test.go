package gitea_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/provider/gitea"
	"github.com/stretchr/testify/require"
)

// TestLoginEndToEnd drives the whole loopback flow: the prompt acts as the
// browser, hitting the real loopback callback with a code and the matching state;
// the token exchange is mocked. It proves the authorize URL carries PKCE/scope,
// the redirect URI sent to exchange is the exact one the browser was sent to (the
// mismatch a regression would introduce), and the minted creds land in the store.
func TestLoginEndToEnd(t *testing.T) {
	t.Parallel()

	store := &memStore{m: map[string]string{}}

	var exchangedRedirect, exchangedVerifier, exchangedCode string
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			form, _ := url.ParseQuery(string(body))
			exchangedRedirect = form.Get("redirect_uri")
			exchangedVerifier = form.Get("code_verifier")
			exchangedCode = form.Get("code")
			return jsonResponse(
				req,
				`{"access_token":"acc","refresh_token":"ref","expires_in":3600}`,
				"",
			), nil
		}),
	}

	var authURLSeen string
	prompt := func(authURL string) {
		authURLSeen = authURL
		u, err := url.Parse(authURL)
		require.NoError(t, err)
		q := u.Query()
		// Act as the browser: the buffered callback channel lets this be synchronous.
		resp, err := http.Get(q.Get("redirect_uri") + "?code=thecode&state=" + q.Get("state"))
		require.NoError(t, err)
		_ = resp.Body.Close()
	}

	require.NoError(t, gitea.LoginWith(t.Context(), "", "", prompt, client, store))

	u, err := url.Parse(authURLSeen)
	require.NoError(t, err)
	require.Equal(t, "https://codeberg.org/login/oauth/authorize", u.Scheme+"://"+u.Host+u.Path)
	require.Equal(t, "code", u.Query().Get("response_type"))
	require.Equal(t, "S256", u.Query().Get("code_challenge_method"))
	require.Equal(t, "read:repository", u.Query().Get("scope"))
	require.NotEmpty(t, u.Query().Get("code_challenge"))

	// The redirect URI used for the exchange must equal the one the browser hit.
	require.Equal(t, u.Query().Get("redirect_uri"), exchangedRedirect)
	require.Equal(t, "thecode", exchangedCode)
	require.NotEmpty(t, exchangedVerifier)

	var stored struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal([]byte(store.m["codeberg.org"]), &stored))
	require.Equal(t, "acc", stored.AccessToken)
}

// TestCallbackHandler covers the loopback callback: a matching state with a code
// is captured and answered 200; a mismatched state, an error param, or a missing
// code is rejected 400 and reported on the error channel.
func TestCallbackHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		query    string
		wantCode string
		wantErr  bool
		status   int
	}{
		{"valid", "?state=s&code=abc", "abc", false, http.StatusOK},
		{"state mismatch", "?state=evil&code=abc", "", true, http.StatusBadRequest},
		{"error param", "?state=s&error=access_denied", "", true, http.StatusBadRequest},
		{"missing code", "?state=s", "", true, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			codeCh := make(chan string, 1)
			errCh := make(chan error, 1)
			h := gitea.CallbackHandler("s", codeCh, errCh)

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+tt.query, nil))

			require.Equal(t, tt.status, rec.Code)
			if tt.wantErr {
				require.Len(t, errCh, 1)
				require.Empty(t, codeCh)
				return
			}
			require.Equal(t, tt.wantCode, <-codeCh)
			require.Empty(t, errCh)
		})
	}
}

// memStore is an in-memory token store for the credential-resolution tests.
type memStore struct{ m map[string]string }

func (s *memStore) Get(host string) (string, bool) { v, ok := s.m[host]; return v, ok }
func (s *memStore) Set(host, token string) error   { s.m[host] = token; return nil }

// TestStoredTokenSendsBearer covers a minted login (stored under the host) being
// sent as an OAuth Bearer credential, distinct from a PAT's `token` scheme.
func TestStoredTokenSendsBearer(t *testing.T) {
	t.Parallel()

	store := &memStore{m: map[string]string{
		"codeberg.org": `{"access_token":"acc","refresh_token":"ref","expiry":"2999-01-01T00:00:00Z","client_id":"cid"}`,
	}}

	var auth string
	p := gitea.New(
		gitea.WithStore(store),
		gitea.WithTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			auth = req.Header.Get("Authorization")
			return jsonResponse(req, `[]`, ""), nil
		})),
	)

	_, err := p.Discover(
		t.Context(),
		resourceFor(t, p, directive.KV{Key: "repository", Value: "forgejo/forgejo"}),
	)
	require.NoError(t, err)
	require.Equal(t, "Bearer acc", auth)
}

// TestExpiredTokenRefreshes covers an expired stored token being refreshed before
// use: the refresh endpoint mints a new token, discovery uses it, and the rotated
// credential is written back to the store.
func TestExpiredTokenRefreshes(t *testing.T) {
	t.Parallel()

	store := &memStore{m: map[string]string{
		"codeberg.org": `{"access_token":"old","refresh_token":"ref","expiry":"2000-01-01T00:00:00Z","client_id":"cid"}`,
	}}

	var auth string
	p := gitea.New(
		gitea.WithStore(store),
		gitea.WithTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/login/oauth/access_token") {
				return jsonResponse(
					req,
					`{"access_token":"new","refresh_token":"ref2","expires_in":3600}`,
					"",
				), nil
			}
			auth = req.Header.Get("Authorization")
			return jsonResponse(req, `[]`, ""), nil
		})),
	)

	_, err := p.Discover(
		t.Context(),
		resourceFor(t, p, directive.KV{Key: "repository", Value: "forgejo/forgejo"}),
	)
	require.NoError(t, err)
	require.Equal(t, "Bearer new", auth)

	var stored struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal([]byte(store.m["codeberg.org"]), &stored))
	require.Equal(t, "new", stored.AccessToken)
	require.Equal(t, "ref2", stored.RefreshToken)
}

// TestExpiredTokenRefreshFailsToAnonymous covers a failed refresh degrading to
// anonymous access rather than erroring the whole discovery.
func TestExpiredTokenRefreshFailsToAnonymous(t *testing.T) {
	t.Parallel()

	store := &memStore{m: map[string]string{
		"codeberg.org": `{"access_token":"old","refresh_token":"ref","expiry":"2000-01-01T00:00:00Z","client_id":"cid"}`,
	}}

	var auth string
	p := gitea.New(
		gitea.WithStore(store),
		gitea.WithTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/login/oauth/access_token") {
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Status:     "400 Bad Request",
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_grant"}`)),
					Request:    req,
				}, nil
			}
			auth = req.Header.Get("Authorization")
			return jsonResponse(req, `[]`, ""), nil
		})),
	)

	_, err := p.Discover(
		t.Context(),
		resourceFor(t, p, directive.KV{Key: "repository", Value: "forgejo/forgejo"}),
	)
	require.NoError(t, err)
	require.Empty(t, auth)
}
