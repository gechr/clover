package docker

import (
	"net/http"

	"github.com/google/go-containerregistry/pkg/authn"
)

// Option configures a [Provider].
type Option func(*Provider)

// WithKeychain overrides the credential keychain, for tests.
func WithKeychain(kc authn.Keychain) Option {
	return func(p *Provider) { p.keychain = kc }
}

// WithTransport overrides the HTTP transport, for tests.
func WithTransport(rt http.RoundTripper) Option {
	return func(p *Provider) { p.transport = rt }
}
