package node

import (
	"fmt"
	"image/color"
	"net/http"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/provider"
)

// keyLTS is the only directive key node accepts.
const keyLTS = constant.DirectiveLTS

// Provider resolves Node.js runtime versions from nodejs.org's public,
// unauthenticated release index. The index is a single newest-first JSON listing
// of every release, so the provider fetches it once and lets the framework own
// selection; it only lists candidates and tags each with the release date the
// index returns for free.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached default

	client *http.Client
}

// New returns the Node.js provider. The release index is public and publishes no
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
func (p *Provider) Name() string { return constant.ProviderNode }

// Color is the provider's brand color. See [provider.Provider.Color].
func (p *Provider) Color(dark bool) color.Color {
	return provider.Adapt(dark, "#3C873A", "#6CC24A")
}

// Dated marks the listing as date-bearing: every release carries a publication
// date, so cooldown applies.
func (p *Provider) Dated() {}

// Keys reports the directive keys node accepts, in canonical order.
func (p *Provider) Keys() []provider.Key {
	return []provider.Key{
		{Name: keyLTS},
	}
}

// Resource validates a directive into a Node.js resource.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	lts, err := d.Bool(keyLTS)
	if err != nil {
		return nil, fmt.Errorf("node: %w", err)
	}
	return resource{lts: lts}, nil
}

// Identify names the Node.js runtime and its home page.
func (p *Provider) Identify(r provider.Resource) (string, string) {
	if _, ok := r.(resource); !ok {
		return "", ""
	}
	return host, "https://" + host
}

// Describe returns a human-readable label for a resource, noting when it is
// scoped to the LTS lines.
func (p *Provider) Describe(r provider.Resource) string {
	res, ok := r.(resource)
	if !ok {
		return constant.ProviderNode
	}
	label := host
	if res.lts {
		label += " (LTS)"
	}
	return label
}

// resource is a validated Node.js descriptor: whether to track only the
// long-term-support release lines, or all releases.
type resource struct {
	lts bool
}
