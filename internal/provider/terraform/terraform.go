package terraform

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/provider"
	xstrings "github.com/gechr/x/strings"
)

// Directive keys the registry provider accepts.
const (
	keySource = constant.DirectiveSource
	keyHost   = constant.DirectiveHost
)

// Registry is one public registry the provider registers under: the provider
// name, the default host, and the web page format a resolved version links to.
// The web page exists only on the public registry, so URL goes empty when host
// points elsewhere.
type Registry struct {
	name string
	host string
	web  string // fmt format for (namespace, name, version)
}

var (
	// Terraform is registered as provider=terraform, defaulting to HashiCorp's
	// public registry.
	Terraform = Registry{
		name: constant.ProviderTerraform,
		host: "registry.terraform.io",
		web:  "https://registry.terraform.io/providers/%s/%s/%s",
	}
	// OpenTofu is registered as provider=opentofu, defaulting to the public
	// OpenTofu registry. Its web pages live on the search UI and carry a
	// v-prefixed version path.
	OpenTofu = Registry{
		name: constant.ProviderOpentofu,
		host: "registry.opentofu.org",
		web:  "https://search.opentofu.org/provider/%s/%s/v%s",
	}
)

// Provider resolves provider versions from a Terraform-protocol registry. The
// registry is public and unauthenticated; the versions endpoint returns the
// whole version history in one response, so discovery is a single (cached)
// fetch per source plus the one-time service discovery document per host.
type Provider struct {
	registry  Registry
	transport http.RoundTripper // overridable for tests; nil uses the cached default

	client *http.Client
}

// New returns the provider for one registry. The public registries send no
// rate-limit headers, so the client is a plain cached one.
func New(registry Registry, opts ...Option) *Provider {
	p := &Provider{registry: registry}
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

// Name identifies the provider by its registry's registered name.
func (p *Provider) Name() string { return p.registry.name }

// Keys reports the directive keys the registry provider accepts, in canonical
// order.
func (p *Provider) Keys() []provider.Key {
	return []provider.Key{
		{Name: keySource, Required: true},
		{Name: keyHost},
	}
}

// Resource validates a directive into a registry resource: the source address
// split into namespace and name, and the registry host.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	source, _ := d.Get(keySource)
	namespace, name, ok := strings.Cut(source, "/")
	if !ok || xstrings.AnyEmpty(namespace, name) || strings.Contains(name, "/") {
		return nil, fmt.Errorf(
			"%s: %q must be namespace/name, got %q",
			p.registry.name,
			keySource,
			source,
		)
	}
	host, err := forge.Host(p.registry.name, d, p.registry.host)
	if err != nil {
		return nil, err
	}
	return resource{host: host, namespace: namespace, name: name}, nil
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(r provider.Resource) string {
	res, ok := r.(resource)
	if !ok {
		return p.registry.name
	}
	return res.host + "/" + res.namespace + "/" + res.name
}

// resource is a validated registry descriptor: the registry host and the
// source address it serves versions for.
type resource struct {
	host      string
	namespace string
	name      string
}
