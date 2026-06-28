package gitea_test

import (
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/gechr/clover/internal/provider/gitea"
	"github.com/stretchr/testify/require"
)

func TestCredsExpired(t *testing.T) {
	t.Parallel()

	require.False(t, gitea.Expired(gitea.Creds{}), "zero expiry is treated as non-expiring")
	require.False(t, gitea.Expired(gitea.Creds{Expiry: time.Now().Add(time.Hour)}))
	require.True(t, gitea.Expired(gitea.Creds{Expiry: time.Now().Add(-time.Hour)}))
	require.True(t,
		gitea.Expired(gitea.Creds{Expiry: time.Now().Add(10 * time.Second)}),
		"within the skew window",
	)
}

func TestExchangeCode(t *testing.T) {
	t.Parallel()

	var gotForm url.Values
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "https://codeberg.org/login/oauth/access_token", req.URL.String())
			body, _ := io.ReadAll(req.Body)
			gotForm, _ = url.ParseQuery(string(body))
			return jsonResponse(
				req,
				`{"access_token":"acc","refresh_token":"ref","expires_in":3600}`,
				"",
			), nil
		}),
	}

	c, err := gitea.ExchangeCode(
		t.Context(), client, "codeberg.org", "cid", "thecode", "http://127.0.0.1:5000/", "verif",
	)
	require.NoError(t, err)
	require.Equal(t, "acc", c.AccessToken)
	require.Equal(t, "ref", c.RefreshToken)
	require.Equal(t, "cid", c.ClientID)
	require.False(t, c.Expiry.IsZero())

	require.Equal(t, "authorization_code", gotForm.Get("grant_type"))
	require.Equal(t, "thecode", gotForm.Get("code"))
	require.Equal(t, "verif", gotForm.Get("code_verifier"))
	require.Equal(t, "http://127.0.0.1:5000/", gotForm.Get("redirect_uri"))
}

func TestRefreshCreds(t *testing.T) {
	t.Parallel()

	var gotForm url.Values
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			gotForm, _ = url.ParseQuery(string(body))
			return jsonResponse(
				req,
				`{"access_token":"acc2","refresh_token":"ref2","expires_in":3600}`,
				"",
			), nil
		}),
	}

	c, err := gitea.RefreshCreds(
		t.Context(), client, "codeberg.org", gitea.Creds{RefreshToken: "ref", ClientID: "cid"},
	)
	require.NoError(t, err)
	require.Equal(t, "acc2", c.AccessToken)
	require.Equal(t, "ref2", c.RefreshToken)
	require.Equal(t, "refresh_token", gotForm.Get("grant_type"))
	require.Equal(t, "ref", gotForm.Get("refresh_token"))
	require.Equal(t, "cid", gotForm.Get("client_id"))
}

func TestPostTokenRejectsEmptyAccessToken(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, `{"refresh_token":"ref"}`, ""), nil
		}),
	}
	_, err := gitea.ExchangeCode(t.Context(), client, "codeberg.org", "cid", "c", "r", "v")
	require.EqualError(t, err, "gitea: token response carried no access token")
}
