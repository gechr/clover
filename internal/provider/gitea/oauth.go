package gitea

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// teaClientID is Gitea's built-in public "tea" OAuth application, registered on
// every stock instance (models/auth/oauth2.go BuiltinApplications) with a
// loopback redirect and no client secret. Embedding it lets `clover login gitea`
// work against any default instance without the user registering an app; an
// instance that disabled the built-ins needs an explicit --client-id.
const teaClientID = "d57cb8c4-630c-4168-8324-ec79935e18d4"

// oauthScope is the narrowest scope that reads a repository's tags and releases.
const oauthScope = "read:repository"

// expirySkew refreshes a token slightly before its stated expiry, so a request
// is never sent with an access token that lapses in flight.
const expirySkew = 30 * time.Second

// creds is the persisted login: the bearer access token, the rotating refresh
// token, its expiry, and the client id it was minted with (refresh reuses it).
// It is JSON-encoded into the token store under the forge host.
type creds struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	ClientID     string    `json:"client_id"`
}

// expired reports whether the access token has reached its expiry (minus skew). A
// zero expiry means the server gave no lifetime, so the token is treated as
// non-expiring rather than perpetually stale.
func (c creds) expired() bool {
	return !c.Expiry.IsZero() && time.Now().After(c.Expiry.Add(-expirySkew))
}

// authorizeURL and tokenURL are Gitea's OAuth endpoints on a host.
func authorizeURL(host string) string { return "https://" + host + "/login/oauth/authorize" }
func tokenURL(host string) string     { return "https://" + host + "/login/oauth/access_token" }

// exchangeCode trades an authorization code for tokens, proving possession of the
// PKCE verifier. No client secret is sent: the built-in app is a public client.
func exchangeCode(
	ctx context.Context,
	client *http.Client,
	host, clientID, code, redirectURI, verifier string,
) (creds, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	return postToken(ctx, client, host, clientID, form)
}

// refreshCreds exchanges a refresh token for a fresh access token, reusing the
// client id the credential was minted with. Gitea rotates the refresh token, so
// the caller must persist the result.
func refreshCreds(ctx context.Context, client *http.Client, host string, c creds) (creds, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {c.ClientID},
		"refresh_token": {c.RefreshToken},
	}
	return postToken(ctx, client, host, c.ClientID, form)
}

// postToken POSTs an OAuth token request and decodes the response into creds,
// stamping the expiry from expires_in.
func postToken(
	ctx context.Context,
	client *http.Client,
	host, clientID string,
	form url.Values,
) (creds, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, tokenURL(host), strings.NewReader(form.Encode()),
	)
	if err != nil {
		return creds{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return creds{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		msg, _ := io.ReadAll(resp.Body)
		return creds{}, fmt.Errorf(
			"gitea: token request: %s (%s)",
			strings.TrimSpace(string(msg)),
			resp.Status,
		)
	}

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return creds{}, fmt.Errorf("gitea: decode token response: %w", err)
	}
	if body.AccessToken == "" {
		return creds{}, fmt.Errorf("gitea: token response carried no access token")
	}

	var expiry time.Time
	if body.ExpiresIn > 0 {
		expiry = time.Now().Add(time.Duration(body.ExpiresIn) * time.Second)
	}
	return creds{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		Expiry:       expiry,
		ClientID:     clientID,
	}, nil
}
