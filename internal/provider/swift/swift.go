package swift

import (
	"image/color"
	"net/http"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/provider"
)

// Provider resolves Swift toolchain versions from swift.org's public,
// unauthenticated release index. The index is a single JSON listing of every
// release, so the provider fetches it once and lets the framework own selection;
// it lists candidates and tags each with the publication date and SDK checksums
// the index returns for free, which cooldown and followers consume.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached default

	client *http.Client
}

// New returns the Swift provider. The release index is public and publishes no
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
func (p *Provider) Name() string { return constant.ProviderSwift }

// Color is the provider's brand color. See [provider.Provider.Color].
func (p *Provider) Color(dark bool) color.Color {
	return provider.Adapt(dark, "#D15E47", "#F24A0D")
}

// Dated marks the listing as date-bearing: every release carries a publication
// date, so cooldown applies.
func (p *Provider) Dated() {}

// Keys reports the directive keys the provider accepts. swift.org needs none of
// its own: the whole index arrives in one fetch with no scoping options.
func (p *Provider) Keys() []provider.Key { return nil }

// Resource validates a directive into a Swift resource. swift.org takes no
// provider-specific keys, so every directive resolves to the same descriptor.
func (p *Provider) Resource(_ directive.Directive) (provider.Resource, error) {
	return resource{}, nil
}

// Identify names the Swift toolchain and its home page.
func (p *Provider) Identify(r provider.Resource) (string, string) {
	if _, ok := r.(resource); !ok {
		return "", ""
	}
	return host, constant.SchemeHTTPS + host
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(r provider.Resource) string {
	if _, ok := r.(resource); !ok {
		return constant.ProviderSwift
	}
	return host
}

// resource is the validated Swift descriptor. It carries no fields today: the
// release index has one shape and no scoping options, but the type keeps
// discovery's contract uniform with the other providers.
type resource struct{}
