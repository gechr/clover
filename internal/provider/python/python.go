package python

import (
	"fmt"
	"net/http"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/provider"
)

// Provider resolves CPython versions from python.org's public, unauthenticated
// downloads API. The API is a single JSON array of every release, so the
// provider fetches it once and lets the framework own selection; it lists
// candidates and tags each with the publication date the API returns for free,
// which cooldown consumes.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached default

	client *http.Client
}

// New returns the Python provider. The downloads API is public and publishes no
// rate-limit headers, so the client is a plain cached one with no ratelimit
// wrapper or credentials.
func New(opts ...Option) *Provider {
	p := &Provider{}
	for _, opt := range opts {
		opt(p)
	}
	var cacheOpts []httpcache.Option
	if p.transport != nil {
		cacheOpts = append(cacheOpts, httpcache.WithTransport(p.transport))
	}
	p.client = httpcache.New(cacheOpts...)
	return p
}

// Name identifies the provider.
func (p *Provider) Name() string { return constant.ProviderPython }

// Dated marks the listing as date-bearing: every release carries a publication
// date, so cooldown applies.
func (p *Provider) Dated() {}

// Keys reports the directive keys the provider accepts. python.org needs none of
// its own: the whole listing arrives in one fetch and it publishes no
// per-platform options.
func (p *Provider) Keys() []provider.Key { return nil }

// Resource validates a directive into a Python resource. python.org takes no
// provider-specific keys, so every directive resolves to the same descriptor.
// It rejects asset= up front: the downloads API publishes no release assets,
// so the filter could never match and every selection would fail confusingly.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	if _, ok := d.Get(constant.RuleAsset); ok {
		return nil, fmt.Errorf(
			"python: %q is not supported, python.org publishes no release assets",
			constant.RuleAsset,
		)
	}
	return resource{}, nil
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(r provider.Resource) string {
	if _, ok := r.(resource); !ok {
		return constant.ProviderPython
	}
	return host
}

// resource is the validated Python descriptor. It carries no fields today: the
// downloads API has one shape and no scoping options, but the type keeps
// discovery's contract uniform with the other providers.
type resource struct{}
