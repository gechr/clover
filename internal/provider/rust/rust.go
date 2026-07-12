package rust

import (
	"fmt"
	"image/color"
	"net/http"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/provider"
)

// keyChannel is the only directive key rust accepts.
const keyChannel = constant.DirectiveChannel

// Channel names, as rustup spells them. Stable and beta are trackable; nightly
// builds carry a date instead of a version, so there is nothing version-shaped
// to resolve and the channel is rejected up front.
const (
	channelStable  = "stable"
	channelBeta    = "beta"
	channelNightly = "nightly"
)

// Provider resolves Rust toolchain versions from static.rust-lang.org's public,
// unauthenticated manifest index. The index is a single chronological text
// listing of every channel manifest, so the provider fetches it once and lets
// the framework own selection; it lists candidates and tags each with the date
// of the directory its manifest was published under, which cooldown consumes.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached default

	client *http.Client
}

// New returns the Rust provider. The manifest index is public and publishes no
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
func (p *Provider) Name() string { return constant.ProviderRust }

// Color is the provider's brand color. See [provider.Provider.Color].
func (p *Provider) Color(dark bool) color.Color {
	return provider.Adapt(dark, "#8A3E18", "#B0562C")
}

// Dated marks the listing as date-bearing: every release carries the date its
// manifest was published under, so cooldown applies.
func (p *Provider) Dated() {}

// Keys reports the directive keys rust accepts, in canonical order.
func (p *Provider) Keys() []provider.Key {
	return []provider.Key{
		{Name: keyChannel},
	}
}

// Resource validates a directive into a Rust resource: which release channel's
// versions to list, stable when omitted. Nightly is called out separately - it
// is a real channel a user may plausibly ask for, but its builds are dated
// snapshots with no version of their own, so it cannot be tracked.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	channel, ok := d.Get(keyChannel)
	if !ok {
		channel = channelStable
	}
	switch channel {
	case channelStable, channelBeta:
	case channelNightly:
		return nil, fmt.Errorf(
			"rust: channel %q is not trackable: nightly builds are dated snapshots, not versions",
			channel,
		)
	default:
		return nil, fmt.Errorf(
			"rust: invalid channel %q (expected %q or %q)",
			channel, channelStable, channelBeta,
		)
	}
	return resource{channel: channel}, nil
}

// Identify names the Rust toolchain and its home page.
func (p *Provider) Identify(r provider.Resource) (string, string) {
	if _, ok := r.(resource); !ok {
		return "", ""
	}
	return host, "https://www." + host
}

// Describe returns a human-readable label for a resource, noting when it is
// scoped to the beta channel.
func (p *Provider) Describe(r provider.Resource) string {
	res, ok := r.(resource)
	if !ok {
		return constant.ProviderRust
	}
	label := host
	if res.channel == channelBeta {
		label += " (beta)"
	}
	return label
}

// resource is a validated Rust descriptor: which release channel's versions to
// list.
type resource struct {
	channel string
}
