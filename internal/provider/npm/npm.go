package npm

import (
	"fmt"
	"net/http"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/provider"
)

// keyPackage is the only directive key npm accepts.
const keyPackage = constant.DirectivePackage

// Provider resolves npm package versions from the public npm registry. A
// package's packument holds its whole version history in one response, so the
// provider fetches it once and lets the framework own selection; it only lists
// candidates and tags each with the publication date the registry returns for
// free.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached default

	client *http.Client
}

// New returns the npm provider. The registry is public for reads and publishes
// no rate-limit headers, so the client is a plain cached one with no ratelimit
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
func (p *Provider) Name() string { return constant.ProviderNpm }

// Dated marks the listing as date-bearing: the packument's time map dates every
// version, so cooldown applies.
func (p *Provider) Dated() {}

// Keys reports the directive keys npm accepts, in canonical order.
func (p *Provider) Keys() []provider.Key {
	return []provider.Key{
		{Name: keyPackage, Required: true},
	}
}

// Resource validates a directive into an npm resource.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	pkg, ok := d.Get(keyPackage)
	if !ok || pkg == "" {
		return nil, fmt.Errorf("npm: %q is required", keyPackage)
	}
	return resource{pkg: pkg}, nil
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(r provider.Resource) string {
	res, ok := r.(resource)
	if !ok {
		return constant.ProviderNpm
	}
	return host + "/" + res.pkg
}

// resource is a validated npm descriptor: which package to track, exactly as
// published (a scoped name keeps its @scope/ prefix).
type resource struct {
	pkg string
}
