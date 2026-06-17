package github

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"
	"github.com/gechr/cusp/internal/constant"
	"github.com/gechr/cusp/internal/directive"
	"github.com/gechr/cusp/internal/httpcache"
	"github.com/gechr/cusp/internal/provider"
	"github.com/gechr/cusp/internal/ratelimit"
)

// host is the GitHub host go-gh resolves credentials for and sends requests to.
const host = "github.com"

// Directive keys and the values the source key accepts.
const (
	keyRepo   = "repo"
	keySource = "source"

	sourceTags     = "tags"
	sourceReleases = "releases"
)

// rateHeaders describes GitHub's rate-limit headers for the ratelimit transport.
var rateHeaders = ratelimit.Headers{
	Remaining:  "X-RateLimit-Remaining",
	Reset:      "X-RateLimit-Reset",
	ResetKind:  ratelimit.ResetEpoch,
	RetryAfter: "Retry-After",
}

func init() { provider.Register(New()) }

// Provider resolves versions from GitHub tags or releases. It holds a single
// lazily-built REST client so one cache and one rate-limit budget are shared
// across every marker in a run.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached, rate-limited default

	once    sync.Once
	rest    *api.RESTClient
	restErr error
}

// Option configures a [Provider].
type Option func(*Provider)

// WithTransport overrides the HTTP transport, for tests.
func WithTransport(rt http.RoundTripper) Option {
	return func(p *Provider) { p.transport = rt }
}

// New returns the GitHub provider.
func New(opts ...Option) *Provider {
	p := &Provider{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name identifies the provider.
func (p *Provider) Name() string { return constant.ProviderGithub }

// Keys reports the directive keys GitHub accepts, in canonical order.
func (p *Provider) Keys() []provider.Key {
	return []provider.Key{
		{Name: keyRepo, Required: true},
		{Name: keySource},
	}
}

// Resource validates a directive into a GitHub resource.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	repo, ok := d.Get(keyRepo)
	if !ok {
		return nil, fmt.Errorf("github: %s is required", keyRepo)
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return nil, fmt.Errorf("github: %s must be owner/name, got %q", keyRepo, repo)
	}

	source := sourceTags
	if s, ok := d.Get(keySource); ok {
		if s != sourceTags && s != sourceReleases {
			return nil, fmt.Errorf(
				"github: %s must be %s or %s, got %q",
				keySource,
				sourceTags,
				sourceReleases,
				s,
			)
		}
		source = s
	}

	return resource{owner: owner, name: name, source: source}, nil
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(r provider.Resource) string {
	res, ok := r.(resource)
	if !ok {
		return constant.ProviderGithub
	}
	return fmt.Sprintf("%s/%s/%s (%s)", host, res.owner, res.name, res.source)
}

// resource is GitHub's validated descriptor.
type resource struct {
	owner  string
	name   string
	source string
}

// client lazily builds the REST client, resolving the gh-compatible token and
// wrapping the transport with caching and rate-limit handling. It is built once
// and reused, so the cache and rate-limit state are shared across the run.
func (p *Provider) client() (*api.RESTClient, error) {
	p.once.Do(func() {
		transport := p.transport
		if transport == nil {
			transport = httpcache.New(
				httpcache.WithTransport(ratelimit.New(nil, rateHeaders)),
			).Transport
		}
		token, _ := auth.TokenForHost(host)
		p.rest, p.restErr = api.NewRESTClient(api.ClientOptions{
			Host:      host,
			AuthToken: token,
			Transport: transport,
		})
	})
	return p.rest, p.restErr
}
