package github

import "net/http"

// Option configures a [Provider].
type Option func(*Provider)

// WithStore sets the token store the credential chain reads the clover-minted
// token from, for tests.
func WithStore(s tokenStore) Option {
	return func(p *Provider) { p.store = s }
}

// WithToken injects a host-bound PAT credential directly, for tests exercising
// the authenticated path (and the exfil guard) without reading the machine's
// environment.
func WithToken(tok string) Option {
	return func(p *Provider) { p.tokenOpt = tok }
}

// WithTransport overrides the HTTP transport, for tests. It also pins credential
// resolution to the explicit store (see credential), so a test never reaches the
// network and its auth path - GraphQL vs anonymous REST - is fully deterministic.
func WithTransport(rt http.RoundTripper) Option {
	return func(p *Provider) { p.transport = rt }
}
