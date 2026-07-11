package pypi

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/provider"
)

// keyPackage is the only directive key pypi accepts.
const keyPackage = constant.DirectivePackage

// packageName is the PEP 508 project-name grammar: alphanumeric runs joined by
// single or repeated separators, starting and ending alphanumeric.
var packageName = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)

// separators collapses a run of PEP 503 name separators for normalization.
var separators = regexp.MustCompile(`[-_.]+`)

// Provider resolves Python package versions from PyPI's public, unauthenticated
// JSON API. One fetch returns a package's whole release history, so the
// provider lists candidates and lets the framework own selection, tagging each
// with the upload time the API returns for free.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached default

	client *http.Client
}

// New returns the PyPI provider. The JSON API is public and publishes no
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
func (p *Provider) Name() string { return constant.ProviderPypi }

// Dated marks the listing as date-bearing: every file carries an upload time,
// so cooldown applies.
func (p *Provider) Dated() {}

// Keys reports the directive keys pypi accepts, in canonical order.
func (p *Provider) Keys() []provider.Key {
	return []provider.Key{
		{Name: keyPackage, Required: true},
	}
}

// Resource validates a directive into a PyPI resource. The package name is
// normalized per PEP 503 (lowercased, separator runs collapsed to a dash), the
// form every PyPI URL accepts regardless of how the project spells its name.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	name, ok := d.Get(keyPackage)
	if !ok {
		return nil, fmt.Errorf("pypi: %q is required", keyPackage)
	}
	if !packageName.MatchString(name) {
		return nil, fmt.Errorf("pypi: %q is not a valid package name, got %q", keyPackage, name)
	}
	return resource{name: normalize(name)}, nil
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(r provider.Resource) string {
	res, ok := r.(resource)
	if !ok {
		return constant.ProviderPypi
	}
	return host + "/" + res.name
}

// normalize canonicalizes a package name per PEP 503: lowercase, with every
// run of dots, dashes, and underscores collapsed to a single dash.
func normalize(name string) string {
	return separators.ReplaceAllString(strings.ToLower(name), "-")
}

// resource is a validated PyPI descriptor: the normalized package name.
type resource struct {
	name string
}
