package terraform

import (
	"fmt"
	"image/color"
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
	name   string
	host   string
	web    string // provider page: fmt format for (namespace, name, version)
	module string // module page: fmt format for (namespace, name, target, version)
	light  string // brand color hex on a light terminal
	dark   string // brand color hex on a dark terminal
}

var (
	// Terraform is registered as provider=terraform, defaulting to HashiCorp's
	// public registry.
	Terraform = Registry{
		name:   constant.ProviderTerraform,
		host:   "registry.terraform.io",
		web:    "https://registry.terraform.io/providers/%s/%s/%s",
		module: "https://registry.terraform.io/modules/%s/%s/%s/%s",
		light:  "#90359C", // purple
		dark:   "#C078E8",
	}
	// OpenTofu is registered as provider=opentofu, defaulting to the public
	// OpenTofu registry. Its web pages live on the search UI and carry a
	// v-prefixed version path.
	OpenTofu = Registry{
		name:   constant.ProviderOpentofu,
		host:   "registry.opentofu.org",
		web:    "https://search.opentofu.org/provider/%s/%s/v%s",
		module: "https://search.opentofu.org/module/%s/%s/%s/v%s",
		light:  "#9E8410", // yellow
		dark:   "#F5E04A",
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
	for _, o := range opts {
		o(p)
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

// Color is the registry's brand color. See [provider.Provider.Color].
func (p *Provider) Color(dark bool) color.Color {
	return provider.Adapt(dark, p.registry.light, p.registry.dark)
}

// Keys reports the directive keys the registry provider accepts, in canonical
// order.
func (p *Provider) Keys() []provider.Key {
	return []provider.Key{
		{Name: keySource, Required: true},
		{Name: keyHost},
	}
}

// Resource validates a directive into a registry resource. The source address
// distinguishes what is being tracked by its shape, exactly as Terraform itself
// reads it: namespace/name is a provider, and namespace/name/target is a module
// for that target system (terraform-aws-modules/vpc/aws). The two live under
// different services of the same registry protocol.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	source, _ := d.Get(keySource)
	host, err := forge.Host(p.registry.name, d, p.registry.host)
	if err != nil {
		return nil, err
	}

	namespace, rest, ok := strings.Cut(source, "/")
	if !ok {
		return nil, p.sourceError(source)
	}
	name, target, isModule := strings.Cut(rest, "/")
	if xstrings.AnyEmpty(namespace, name) ||
		(isModule && (target == "" || strings.Contains(target, "/"))) {
		return nil, p.sourceError(source)
	}
	// A fully-qualified provider address (registry.terraform.io/hashicorp/aws)
	// has three segments too, and Terraform tells the two apart by the block the
	// source sits in - context a provider never sees. A registry namespace is
	// alphanumeric and hyphens, so a dot or port colon in the first segment is a
	// host and nothing else, which resolves the ambiguity without guessing.
	if isModule && strings.ContainsAny(namespace, ".:") {
		return nil, fmt.Errorf(
			"%s: %q names the registry host %q, which belongs in %q",
			p.registry.name,
			keySource,
			namespace,
			keyHost,
		)
	}
	return resource{host: host, namespace: namespace, name: name, target: target}, nil
}

// sourceError reports a source address that is neither a provider nor a module
// address.
func (p *Provider) sourceError(source string) error {
	return fmt.Errorf(
		"%s: %q must be namespace/name or namespace/name/target, got %q",
		p.registry.name,
		keySource,
		source,
	)
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(r provider.Resource) string {
	res, ok := r.(resource)
	if !ok {
		return p.registry.name
	}
	return res.host + "/" + res.address()
}

// resource is a validated registry descriptor: the registry host and the
// source address it serves versions for. A non-empty target makes it a module
// address, naming the system the module provisions for.
type resource struct {
	host      string
	namespace string
	name      string
	target    string
}

// module reports whether the resource addresses a module rather than a
// provider, which decides both the registry service it resolves through and the
// shape of its web page.
func (r resource) module() bool { return r.target != "" }

// address is the source address as written, without the host.
func (r resource) address() string {
	if r.module() {
		return r.namespace + "/" + r.name + "/" + r.target
	}
	return r.namespace + "/" + r.name
}
