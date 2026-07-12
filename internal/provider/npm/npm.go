package npm

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/provider"
)

// Directive keys npm accepts.
const (
	keyDistTag  = constant.DirectiveDistTag
	keyPackage  = constant.DirectivePackage
	keyRegistry = constant.DirectiveRegistry
)

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
		{Name: keyDistTag},
		{Name: keyRegistry},
	}
}

// Resource validates a directive into an npm resource.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	pkg, ok := d.Get(keyPackage)
	if !ok || pkg == "" {
		return nil, fmt.Errorf("npm: %q is required", keyPackage)
	}
	tag, err := distTag(d)
	if err != nil {
		return nil, err
	}
	reg, err := registryBase(d)
	if err != nil {
		return nil, err
	}
	return resource{pkg: pkg, distTag: tag, registry: reg}, nil
}

// distTag resolves the optional dist-tag key: absent means every version is a
// candidate; an explicit value names the registry channel pointer to track.
func distTag(d directive.Directive) (string, error) {
	tag, ok := d.Get(keyDistTag)
	if !ok {
		return "", nil
	}
	switch {
	case tag == "":
		return "", fmt.Errorf("npm: %q must not be empty", keyDistTag)
	case strings.ContainsAny(tag, " \t"):
		return "", fmt.Errorf("npm: %q must not contain whitespace, got %q", keyDistTag, tag)
	}
	return tag, nil
}

// registryBase resolves the optional registry key: absent means the public npm
// registry; an explicit value must be an http(s) base URL, trailing slash
// tolerated.
func registryBase(d directive.Directive) (string, error) {
	reg, ok := d.Get(keyRegistry)
	if !ok {
		return registryURL, nil
	}
	base := strings.TrimSuffix(strings.TrimSpace(reg), "/")
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf(
			"npm: %q must start with https:// or http://, got %q",
			keyRegistry,
			reg,
		)
	}
	if u.Host == "" {
		return "", fmt.Errorf("npm: %s %q has no registry host", keyRegistry, reg)
	}
	return base, nil
}

// Describe returns a human-readable label for a resource: the npm web host for
// the public registry, the custom registry's scheme-stripped base otherwise. A
// tracked dist-tag is appended in npm's own package@tag spelling.
func (p *Provider) Describe(r provider.Resource) string {
	res, ok := r.(resource)
	if !ok {
		return constant.ProviderNpm
	}
	base := host
	if res.registry != registryURL {
		base = strings.TrimPrefix(strings.TrimPrefix(res.registry, "https://"), "http://")
	}
	label := base + "/" + res.pkg
	if res.distTag != "" {
		label += "@" + res.distTag
	}
	return label
}

// resource is a validated npm descriptor: which package to track, exactly as
// published (a scoped name keeps its @scope/ prefix), the dist-tag narrowing
// the candidates to one channel (empty tracks them all), and the registry base
// URL to resolve against.
type resource struct {
	pkg      string
	distTag  string
	registry string
}
