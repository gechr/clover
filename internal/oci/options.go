package oci

import (
	"net/http"

	"github.com/gechr/clover/internal/ratelimit"
	"github.com/google/go-containerregistry/pkg/authn"
)

// Option configures a [Client].
type Option func(*Client)

// WithErrorContext sets the prefix for the client's errors and the hint appended
// to auth and rate-limit failures, so guidance lands in the consumer's voice.
func WithErrorContext(label, hint string) Option {
	return func(c *Client) {
		c.label = label
		c.authHint = hint
	}
}

// WithKeychain overrides the credential keychain.
func WithKeychain(kc authn.Keychain) Option { return func(c *Client) { c.keychain = kc } }

// WithRateHeaders overrides the rate-limit header names.
func WithRateHeaders(h ratelimit.Headers) Option { return func(c *Client) { c.rateHeaders = h } }

// WithTokenEnv names the environment variable that, when set, supplies a ready
// bearer token overriding every other credential source.
func WithTokenEnv(env string) Option { return func(c *Client) { c.tokenEnv = env } }

// WithTransport overrides the HTTP transport, for tests. A nil transport leaves
// the cached, rate-limited default in place.
func WithTransport(rt http.RoundTripper) Option {
	return func(c *Client) {
		if rt != nil {
			c.transport = rt
		}
	}
}
