package crates

import (
	"fmt"
	"image/color"
	"net/http"
	"regexp"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/provider"
)

// keyPackage is the only directive key crates accepts.
const keyPackage = constant.DirectivePackage

// crateName is the crates.io name grammar: ASCII alphanumerics, dashes, and
// underscores, starting alphanumeric, at most 64 characters. Names are exact -
// unlike PyPI, the registry does not normalize case or separators.
var crateName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// userAgentFor builds the User-Agent identifying clover per the crates.io
// crawling policy, which asks every client for an identifiable agent with a
// contact URL. The binary's version is injected at construction (it is unknown
// to a leaf package); an unresolved version drops the suffix.
func userAgentFor(version string) string {
	const contact = " (+https://github.com/gechr/clover)"
	if version == "" {
		return "Clover" + contact
	}
	return "Clover v" + version + contact
}

// Provider resolves Rust crate versions from the crates.io registry API. One
// fetch returns a crate's whole version history, so the provider lists
// candidates and lets the framework own selection, tagging each with the
// publish time and .crate checksum the API returns for free.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached default
	userAgent string            // identifies clover per the crates.io crawling policy

	client *http.Client
}

// New returns the crates provider. The registry API is public and
// unauthenticated, so the client is a plain cached one with no ratelimit
// wrapper or credentials.
func New(opts ...Option) *Provider {
	p := &Provider{userAgent: userAgentFor("")}
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
func (p *Provider) Name() string { return constant.ProviderCrates }

// Color is the provider's brand color. See [provider.Provider.Color].
func (p *Provider) Color(dark bool) color.Color {
	return provider.Adapt(dark, "#7A5410", "#AE7C3C")
}

// Dated marks the listing as date-bearing: every version carries its publish
// time, so cooldown applies.
func (p *Provider) Dated() {}

// Keys reports the directive keys crates accepts, in canonical order.
func (p *Provider) Keys() []provider.Key {
	return []provider.Key{
		{Name: keyPackage, Required: true},
	}
}

// Resource validates a directive into a crates.io resource: the crate name,
// kept exactly as written because the registry looks crates up by their
// published spelling.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	name, ok := d.Get(keyPackage)
	if !ok {
		return nil, fmt.Errorf("crates: %q is required", keyPackage)
	}
	if !crateName.MatchString(name) {
		return nil, fmt.Errorf("crates: %q is not a valid crate name, got %q", keyPackage, name)
	}
	return resource{name: name}, nil
}

// Identify returns the crate name and its crates.io page.
func (p *Provider) Identify(r provider.Resource) (string, string) {
	res, ok := r.(resource)
	if !ok {
		return "", ""
	}
	return res.name, cratePath + res.name
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(r provider.Resource) string {
	res, ok := r.(resource)
	if !ok {
		return constant.ProviderCrates
	}
	return host + "/" + res.name
}

// resource is a validated crates.io descriptor: the exact crate name.
type resource struct {
	name string
}
