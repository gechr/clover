package helm

import (
	"fmt"
	"net/http"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/oci"
	"github.com/gechr/clover/internal/provider"
	"github.com/google/go-containerregistry/pkg/authn"
)

// tokenEnv is the namespaced environment variable holding a ready bearer token
// for an OCI chart registry, overriding every other credential source.
const tokenEnv = "CLOVER_HELM_TOKEN" //nolint:gosec // env var name, not a credential

// authHint guides the user to authenticate, surfaced on a denied or rate-limited
// response from an OCI chart registry.
const authHint = "run `helm registry login` (or `docker login`), or set CLOVER_HELM_TOKEN, " +
	"for higher rate limits and private charts"

// Directive keys helm accepts.
const (
	keyRegistry = constant.DirectiveRegistry
	keyChart    = constant.DirectiveChart
)

// Provider resolves Helm chart versions. A classic https:// repository is read
// from its index.yaml; an oci:// registry lists the chart's tags through the
// shared OCI client. The OCI protocol and the classic index fetch share one
// cached, rate-limited HTTP client across a run.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached, rate-limited default
	keychain  authn.Keychain    // resolves registry login credentials; nil uses the default keychain

	client *oci.Client
}

// New returns the Helm provider, defaulting to the keychain that reads the
// user's existing docker/helm login so clover piggybacks on credentials already
// stored for OCI chart registries.
func New(opts ...Option) *Provider {
	p := &Provider{keychain: authn.DefaultKeychain}
	for _, opt := range opts {
		opt(p)
	}
	p.client = oci.New(
		oci.WithTransport(p.transport),
		oci.WithKeychain(p.keychain),
		oci.WithTokenEnv(tokenEnv),
		oci.WithErrorContext("helm", authHint),
	)
	return p
}

// Name identifies the provider.
func (p *Provider) Name() string { return constant.ProviderHelm }

// Dated marks the listing as date-bearing: the classic index carries release
// dates. OCI tags do not, and fall to the post-discovery date check.
func (p *Provider) Dated() {}

// Keys reports the directive keys helm accepts, in canonical order.
func (p *Provider) Keys() []provider.Key {
	return []provider.Key{
		{Name: keyRegistry, Required: true},
		{Name: keyChart, Required: true},
	}
}

// Resource validates a directive into a Helm resource.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	registry, ok := d.Get(keyRegistry)
	if !ok {
		return nil, fmt.Errorf("helm: %q is required", keyRegistry)
	}
	chart, ok := d.Get(keyChart)
	if !ok {
		return nil, fmt.Errorf("helm: %q is required", keyChart)
	}
	return newReference(registry, chart)
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(r provider.Resource) string {
	ref, ok := r.(reference)
	if !ok {
		return constant.ProviderHelm
	}
	return ref.String()
}
