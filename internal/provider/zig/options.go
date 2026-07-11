package zig

import (
	"net/http"
)

// Option configures a [Provider].
type Option func(*Provider)

// WithTransport overrides the HTTP transport, for tests.
func WithTransport(rt http.RoundTripper) Option {
	return func(p *Provider) { p.transport = rt }
}
