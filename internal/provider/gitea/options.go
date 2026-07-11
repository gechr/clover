package gitea

import "net/http"

// Option configures a [Provider].
type Option func(*Provider)

// WithStore sets the token store the credential chain reads a minted login from,
// for tests.
func WithStore(s tokenStore) Option {
	return func(p *Provider) { p.store = s }
}

// WithToken injects a PAT credential directly, for tests exercising the
// authenticated path without reading the machine's environment.
func WithToken(tok string) Option {
	return func(p *Provider) { p.tokenOpt = tok }
}

// WithTransport overrides the HTTP transport, for tests. It also pins credential
// resolution away from ambient env vars (see staticCredential), so a test never
// reaches the network and its auth path stays deterministic.
func WithTransport(rt http.RoundTripper) Option {
	return func(p *Provider) { p.transport = rt }
}
