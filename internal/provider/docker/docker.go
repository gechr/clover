package docker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/ratelimit"
	"github.com/google/go-containerregistry/pkg/authn"
)

// tokenEnv is the namespaced environment variable that overrides every other
// credential source, deliberately not DOCKER_* which clash with other tools.
const tokenEnv = "CLOVER_DOCKER_TOKEN"

// errAnonymous reports that no Docker credentials were found, so requests fall
// back to anonymous (rate-limited) access. It is informational, not fatal:
// public images still resolve.
var errAnonymous = errors.New("no Docker credentials; using anonymous access")

// Directive keys docker accepts.
const (
	keyRepository = constant.DirectiveRepository
	keyRegistry   = constant.DirectiveRegistry
)

// rateHeaders describes the registry rate-limit headers for the ratelimit
// transport. Docker's windowed values (e.g. "100;w=21600") do not parse as a
// bare integer, so the remaining count is simply treated as unknown; the
// Retry-After on a 429 is still honoured.
var rateHeaders = ratelimit.Headers{
	Remaining:  "RateLimit-Remaining",
	Reset:      "RateLimit-Reset",
	ResetKind:  ratelimit.ResetDelta,
	RetryAfter: "Retry-After",
}

// Provider resolves versions from a container image registry's tags. It holds a
// single lazily-built HTTP client so one cache and one rate-limit budget are
// shared across every marker in a run.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached, rate-limited default
	keychain  authn.Keychain    // resolves docker login credentials; nil uses the default keychain

	once   sync.Once
	client *http.Client

	hubOnce sync.Once
	hubJWT  string
}

// Option configures a [Provider].
type Option func(*Provider)

// WithTransport overrides the HTTP transport, for tests.
func WithTransport(rt http.RoundTripper) Option {
	return func(p *Provider) { p.transport = rt }
}

// WithKeychain overrides the credential keychain, for tests.
func WithKeychain(kc authn.Keychain) Option {
	return func(p *Provider) { p.keychain = kc }
}

// New returns the Docker provider, defaulting to the keychain that reads the
// user's existing docker login (config.json plus any docker-credential-*
// helper), so clover piggybacks on credentials docker already stores.
func New(opts ...Option) *Provider {
	p := &Provider{keychain: authn.DefaultKeychain}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name identifies the provider.
func (p *Provider) Name() string { return constant.ProviderDocker }

// Keys reports the directive keys docker accepts, in canonical order.
func (p *Provider) Keys() []provider.Key {
	return []provider.Key{
		{Name: keyRepository, Required: true},
		{Name: keyRegistry},
	}
}

// Resource validates a directive into a Docker resource.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	repo, ok := d.Get(keyRepository)
	if !ok {
		return nil, fmt.Errorf("docker: %s is required", keyRepository)
	}
	registry, _ := d.Get(keyRegistry)
	return newReference(registry, repo)
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(r provider.Resource) string {
	ref, ok := r.(reference)
	if !ok {
		return constant.ProviderDocker
	}
	return ref.String()
}

// Authenticate reports whether a credential is available, checking the env
// override and the docker login stored for Docker Hub (the common case). It
// performs no network call and never blocks on a prompt. Absence is reported as
// errAnonymous rather than a hard failure, since anonymous reads still work
// (just rate-limited).
func (p *Provider) Authenticate(context.Context) error {
	if p.resolveAuth(hubAuthHost) != nil {
		return nil
	}
	return errAnonymous
}

// AuthHint returns how to authenticate when no credential is found.
func (p *Provider) AuthHint() string {
	return "run `docker login` (or the registry's), or set CLOVER_DOCKER_TOKEN, " +
		"for higher rate limits and private images"
}

// resolveAuth resolves credentials for a registry host, first the
// CLOVER_DOCKER_TOKEN env var (a ready bearer token), then the docker login the
// keychain holds. It returns nil for anonymous access.
func (p *Provider) resolveAuth(host string) *authn.AuthConfig {
	if tok := os.Getenv(tokenEnv); tok != "" {
		return &authn.AuthConfig{RegistryToken: tok}
	}
	authr, err := p.keychain.Resolve(registryResource(host))
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

// httpClient lazily builds the shared HTTP client, wrapping the transport with
// caching and rate-limit handling so the cache and budget are shared run-wide.
func (p *Provider) httpClient() *http.Client {
	p.once.Do(func() {
		if p.transport != nil {
			p.client = &http.Client{Transport: p.transport}
			return
		}
		p.client = httpcache.New(
			httpcache.WithTransport(ratelimit.New(nil, rateHeaders)),
		)
	})
	return p.client
}

// do issues a GET, attaching a bearer token when one is given.
func (p *Provider) do(ctx context.Context, url, bearer string) (*http.Response, error) {
	return p.send(ctx, http.MethodGet, url, bearer, "")
}

// send issues a request, attaching an Accept header and bearer token when given.
func (p *Provider) send(
	ctx context.Context,
	method, url, bearer, accept string,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("docker: build request: %w", err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker: %s %s: %w", method, url, err)
	}
	return resp, nil
}
