package zig

import (
	"net/http"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/provider"
)

// Provider resolves Zig toolchain versions from ziglang.org's public,
// unauthenticated download index. The index is a single JSON object listing every
// release, so the provider fetches it once and lets the framework own selection;
// it lists candidates and tags each with the archive checksums and publication
// date the index returns for free, which followers and cooldown consume.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached default

	client *http.Client
}

// New returns the Zig provider. The download index is public and publishes no
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
func (p *Provider) Name() string { return constant.ProviderZig }

// Dated marks the listing as date-bearing: a tagged release carries a publication
// date. An entry without one falls to the post-discovery date check.
func (p *Provider) Dated() {}

// Keys reports the directive keys the provider accepts. ziglang.org needs none of
// its own: the whole index arrives in one fetch, and per-platform checksum
// selection is a follower's job via its pattern, not a provider option.
func (p *Provider) Keys() []provider.Key { return nil }

// Resource validates a directive into a Zig resource. ziglang.org takes no
// provider-specific keys, so every directive resolves to the same descriptor.
func (p *Provider) Resource(_ directive.Directive) (provider.Resource, error) {
	return resource{}, nil
}

// Identify names the Zig toolchain and its home page.
func (p *Provider) Identify(r provider.Resource) (string, string) {
	if _, ok := r.(resource); !ok {
		return "", ""
	}
	return host, "https://" + host
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(r provider.Resource) string {
	if _, ok := r.(resource); !ok {
		return constant.ProviderZig
	}
	return host
}

// resource is the validated Zig descriptor. It carries no fields today: the
// download index has one shape and no scoping options, but the type keeps
// discovery's contract uniform with the other providers.
type resource struct{}
