package docker

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/oci"
	"github.com/gechr/clover/internal/provider"
	"github.com/google/go-containerregistry/pkg/authn"
)

// tokenEnv is the namespaced environment variable that overrides every other
// credential source, deliberately not DOCKER_* which clash with other tools.
const tokenEnv = "CLOVER_DOCKER_TOKEN"

// authHint guides the user to authenticate, surfaced on a denied or rate-limited
// response - the usual cause, since anonymous access has the tightest limits.
// Docker auth is registry-scoped, so the hint lands on the actual failing
// request rather than as a coarse, provider-wide warning that cannot know which
// registry a run used.
const authHint = "run `docker login` (or the registry's), or set CLOVER_DOCKER_TOKEN, " +
	"for higher rate limits and private images"

// Directive keys docker accepts.
const (
	keyRepository = constant.DirectiveRepository
	keyRegistry   = constant.DirectiveRegistry
	keyPlatform   = constant.DirectivePlatform
)

// Provider resolves versions from a container image registry's tags. The OCI
// registry protocol is shared with other providers via an [oci.Client]; docker
// adds Docker Hub's richer web API on top, whose token is resolved once and
// shared across a run.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached, rate-limited default
	keychain  authn.Keychain    // resolves docker login credentials; nil uses the default keychain

	client *oci.Client

	hubMu       sync.Mutex
	hubJWT      string
	hubResolved bool // true once the Hub token is settled, so a transient login failure can retry
}

// New returns the Docker provider, defaulting to the keychain that reads the
// user's existing docker login (config.json plus any docker-credential-*
// helper), so clover piggybacks on credentials docker already stores.
func New(opts ...Option) *Provider {
	p := &Provider{keychain: authn.DefaultKeychain}
	for _, opt := range opts {
		opt(p)
	}
	p.client = oci.New(
		oci.WithTransport(p.transport),
		oci.WithKeychain(p.keychain),
		oci.WithTokenEnv(tokenEnv),
		oci.WithErrorContext("docker", authHint),
	)
	return p
}

// Name identifies the provider.
func (p *Provider) Name() string { return constant.ProviderDocker }

// Dated marks the listing as date-bearing: Docker Hub tags carry a last-updated
// date. Bare OCI registry tags do not, and fall to the post-discovery date check.
func (p *Provider) Dated() {}

// Keys reports the directive keys docker accepts, in canonical order.
func (p *Provider) Keys() []provider.Key {
	return []provider.Key{
		{Name: keyRepository, Required: true},
		{Name: keyRegistry},
		{Name: keyPlatform},
	}
}

// Resource validates a directive into a Docker resource.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	repo, ok := d.Get(keyRepository)
	if !ok {
		return nil, fmt.Errorf("docker: %q is required", keyRepository)
	}
	registry, _ := d.Get(keyRegistry)
	platform, _ := d.Get(keyPlatform)
	return newReference(registry, repo, platform)
}

// Identify returns the image reference and its registry landing page.
func (p *Provider) Identify(r provider.Resource) (string, string) {
	ref, ok := r.(reference)
	if !ok {
		return "", ""
	}
	return ref.String(), ref.url()
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(r provider.Resource) string {
	ref, ok := r.(reference)
	if !ok {
		return constant.ProviderDocker
	}
	return ref.String()
}
