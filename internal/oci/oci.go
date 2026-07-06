package oci

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/ratelimit"
	"github.com/google/go-containerregistry/pkg/authn"
)

// pageSize bounds one page of tag discovery. A registry orders tags lexically,
// so a repository with more than this many tags may have its newest beyond the
// first page; a deep lookup follows the Link header to reach it.
const pageSize = 100

// defaultRateHeaders describes the standard registry rate-limit headers. The
// windowed values some registries return (e.g. "100;w=21600") do not parse as a
// bare integer, so the remaining count is simply treated as unknown; the
// Retry-After on a 429 is still honoured.
var defaultRateHeaders = ratelimit.Headers{
	Remaining:  "RateLimit-Remaining",
	Reset:      "RateLimit-Reset",
	ResetKind:  ratelimit.ResetDelta,
	RetryAfter: "Retry-After",
}

// Repo identifies a repository within a registry. Host serves the /v2 API;
// AuthHost is where credentials are keyed (it diverges from Host only for Docker
// Hub, whose registry is registry-1.docker.io but whose login is stored under
// index.docker.io) and defaults to Host when empty; Repository is the path
// within the registry and the basis for the pull scope. Platform, when set
// (os/arch), selects that platform's manifest digest from a multi-arch index
// instead of the index digest.
type Repo struct {
	Host       string
	AuthHost   string
	Repository string
	Platform   string
}

// authHost returns the host whose credentials apply, defaulting to Host.
func (r Repo) authHost() string {
	if r.AuthHost != "" {
		return r.AuthHost
	}
	return r.Host
}

// Client talks the OCI registry v2 protocol. It holds a single lazily-built HTTP
// client so one cache and one rate-limit budget are shared across every lookup a
// run makes. The zero value is not usable; build one with [New].
type Client struct {
	transport   http.RoundTripper // overridable for tests; nil uses the cached, rate-limited default
	keychain    authn.Keychain    // resolves login credentials; nil uses the default keychain
	tokenEnv    string            // env var holding a ready bearer token that overrides the keychain
	rateHeaders ratelimit.Headers
	label       string // error prefix, e.g. "docker" or "helm"
	authHint    string // appended to auth/rate-limit errors

	once   sync.Once
	client *http.Client

	tokenMu    sync.Mutex
	tokens     map[tokenKey]cachedToken
	repoTokens map[repoTokenKey]tokenKey

	digestMu sync.Mutex
	digests  map[digestKey]string
}

// Option configures a [Client].
type Option func(*Client)

// WithTransport overrides the HTTP transport, for tests. A nil transport leaves
// the cached, rate-limited default in place.
func WithTransport(rt http.RoundTripper) Option {
	return func(c *Client) {
		if rt != nil {
			c.transport = rt
		}
	}
}

// WithKeychain overrides the credential keychain.
func WithKeychain(kc authn.Keychain) Option { return func(c *Client) { c.keychain = kc } }

// WithTokenEnv names the environment variable that, when set, supplies a ready
// bearer token overriding every other credential source.
func WithTokenEnv(env string) Option { return func(c *Client) { c.tokenEnv = env } }

// WithRateHeaders overrides the rate-limit header names.
func WithRateHeaders(h ratelimit.Headers) Option { return func(c *Client) { c.rateHeaders = h } }

// WithErrorContext sets the prefix for the client's errors and the hint appended
// to auth and rate-limit failures, so guidance lands in the consumer's voice.
func WithErrorContext(label, hint string) Option {
	return func(c *Client) {
		c.label = label
		c.authHint = hint
	}
}

// New returns a registry client, defaulting to the keychain that reads the
// user's existing docker login so clover piggybacks on credentials docker
// already stores.
func New(opts ...Option) *Client {
	c := &Client{
		keychain:    authn.DefaultKeychain,
		rateHeaders: defaultRateHeaders,
		label:       "oci",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// HTTPClient lazily builds the shared HTTP client, wrapping the transport with
// caching and rate-limit handling so the cache and budget are shared run-wide.
// It is exported so a consumer's non-registry calls (a Hub web API, a Helm
// index.yaml) reuse the same cache and rate-limit budget.
func (c *Client) HTTPClient() *http.Client {
	c.once.Do(func() {
		if c.transport != nil {
			c.client = &http.Client{Transport: c.transport}
			return
		}
		c.client = httpcache.New(
			httpcache.WithTransport(ratelimit.New(httpcache.NewBaseTransport(), c.rateHeaders)),
		)
	})
	return c.client
}

// Get issues a GET, attaching a bearer token when one is given.
func (c *Client) Get(ctx context.Context, url, bearer string) (*http.Response, error) {
	return c.send(ctx, http.MethodGet, url, bearer, "")
}

// send issues a request, attaching an Accept header and bearer token when given.
func (c *Client) send(
	ctx context.Context,
	method, url, bearer, accept string,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: build request: %w", c.label, err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %s %s: %w", c.label, method, url, err)
	}
	return resp, nil
}

// StatusErr builds the error for a non-OK registry response. An auth or
// rate-limit status (401/403/429) appends the hint, so the guidance lands at the
// moment it is actionable for the registry actually in use.
func (c *Client) StatusErr(action string, resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		return fmt.Errorf("%s: %s: %s (%s)", c.label, action, resp.Status, c.authHint)
	default:
		return fmt.Errorf("%s: %s: %s", c.label, action, resp.Status)
	}
}

// ResolveAuth resolves credentials for a registry host: first the configured
// token env var (a ready bearer token), then the docker login the keychain
// holds. It returns nil for anonymous access.
func (c *Client) ResolveAuth(host string) *authn.AuthConfig {
	if c.tokenEnv != "" {
		if tok := os.Getenv(c.tokenEnv); tok != "" {
			return &authn.AuthConfig{RegistryToken: tok}
		}
	}
	authr, err := c.keychain.Resolve(registryResource(host))
	if err != nil {
		return nil
	}
	cfg, err := authr.Authorization()
	if err != nil || cfg == nil || *cfg == (authn.AuthConfig{}) {
		return nil
	}
	return cfg
}

// registryResource adapts a registry host to authn.Resource, the target the
// keychain resolves credentials for.
type registryResource string

func (r registryResource) String() string      { return string(r) }
func (r registryResource) RegistryStr() string { return string(r) }
