package crates

import (
	"net/http"
)

// Option configures a [Provider].
type Option func(*Provider)

// WithTransport overrides the HTTP transport, for tests.
func WithTransport(rt http.RoundTripper) Option {
	return func(p *Provider) { p.transport = rt }
}

// WithVersion sets the clover version woven into the User-Agent. The
// composition root passes the running binary's version; an empty value keeps
// the versionless fallback.
func WithVersion(version string) Option {
	return func(p *Provider) { p.userAgent = userAgentFor(version) }
}
