package hashicorp

import (
	"fmt"
	"net/http"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/provider"
)

// Directive keys hashicorp accepts.
const (
	keyProduct    = constant.DirectiveProduct
	keyEnterprise = constant.DirectiveEnterprise
	keyBuild      = constant.DirectiveBuild
)

// Provider resolves HashiCorp tool versions (Terraform, Vault, Consul, Nomad,
// ...) from HashiCorp's public, unauthenticated releases service. Discovery is a
// single JSON listing per product; the framework owns selection, so the provider
// only lists candidates and tags each with the metadata the API returns for free.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached default

	client *http.Client
}

// Option configures a [Provider].
type Option func(*Provider)

// WithTransport overrides the HTTP transport, for tests.
func WithTransport(rt http.RoundTripper) Option {
	return func(p *Provider) { p.transport = rt }
}

// New returns the HashiCorp provider. The releases service is public and
// publishes no rate-limit headers, so the client is a plain cached one with no
// ratelimit wrapper or credentials.
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
func (p *Provider) Name() string { return constant.ProviderHashicorp }

// Keys reports the directive keys hashicorp accepts, in canonical order.
func (p *Provider) Keys() []provider.Key {
	return []provider.Key{
		{Name: keyProduct, Required: true},
		{Name: keyEnterprise},
		{Name: keyBuild},
	}
}

// Resource validates a directive into a HashiCorp resource.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	product, ok := d.Get(keyProduct)
	if !ok {
		return nil, fmt.Errorf("hashicorp: %s is required", keyProduct)
	}
	enterprise, err := d.Bool(keyEnterprise)
	if err != nil {
		return nil, fmt.Errorf("hashicorp: %w", err)
	}
	build, _ := d.Get(keyBuild)
	return resource{product: product, enterprise: enterprise, build: build}, nil
}

// Describe returns a human-readable label for a resource, noting the build
// flavor or enterprise edition when one is selected.
func (p *Provider) Describe(r provider.Resource) string {
	res, ok := r.(resource)
	if !ok {
		return constant.ProviderHashicorp
	}
	label := host + "/" + res.product
	switch {
	case res.build != "":
		label += " (" + res.build + ")"
	case res.enterprise:
		label += " (enterprise)"
	}
	return label
}

// resource is a validated HashiCorp descriptor: which product to track and which
// edition. build selects a specific enterprise build flavor by its +metadata
// suffix (e.g. ent.hsm.fips1403), rendering the full version; enterprise selects
// enterprise releases but renders the bare semver. build takes precedence.
type resource struct {
	product    string
	enterprise bool
	build      string
}
