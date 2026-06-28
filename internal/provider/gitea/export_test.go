package gitea

import (
	"context"
	"net/http"
)

// Test-only re-exports of unexported symbols, so the external gitea_test package
// can exercise the OAuth helpers directly.

// Creds aliases the persisted-credential type for tests.
type Creds = creds

var (
	ExchangeCode    = exchangeCode
	RefreshCreds    = refreshCreds
	CallbackHandler = callbackHandler
)

// LoginWith drives the loopback login flow with an injected HTTP client and
// store, for an end-to-end test that needs no network or keychain.
func LoginWith(
	ctx context.Context,
	host, clientID string,
	prompt func(authURL string),
	client *http.Client,
	store tokenStore,
) error {
	return login(ctx, loginConfig{
		host:     host,
		clientID: clientID,
		prompt:   prompt,
		client:   client,
		store:    store,
	})
}

// Expired exposes creds.expired for tests.
func Expired(c Creds) bool { return c.expired() }
