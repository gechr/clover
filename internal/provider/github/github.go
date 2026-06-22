package github

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/ratelimit"
	"github.com/gechr/clover/internal/token"
)

// tokenEnv is the namespaced environment variable that overrides every other
// credential source, deliberately not GITHUB_TOKEN/GH_TOKEN which clash with
// other tools.
const tokenEnv = "CLOVER_GITHUB_TOKEN" //nolint:gosec // env var name, not a credential

// errAnonymous reports that no GitHub credentials were found, so requests fall
// back to anonymous (rate-limited) access. It is informational, not fatal:
// public reads still work.
var errAnonymous = errors.New("no GitHub credentials; using anonymous access")

// host is the GitHub host go-gh resolves credentials for and sends requests to.
const host = "github.com"

// Directive keys and the values the source key accepts.
const (
	keyRepository = constant.DirectiveRepository
	keySource     = "source"

	sourceTags     = "tags"
	sourceReleases = "releases"
)

// rateHeaders describes GitHub's rate-limit headers for the ratelimit transport.
var rateHeaders = ratelimit.Headers{
	Remaining:  "X-RateLimit-Remaining",
	Reset:      "X-RateLimit-Reset",
	ResetKind:  ratelimit.ResetEpoch,
	RetryAfter: "Retry-After",
}

// Provider resolves versions from GitHub tags or releases. It holds a single
// lazily-built REST client so one cache and one rate-limit budget are shared
// across every marker in a run.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached, rate-limited default
	store     tokenStore        // reads the clover-minted token; nil falls through the chain

	once    sync.Once
	rest    *api.RESTClient
	restErr error
}

// tokenStore is the read side of the token store the credential chain consults.
type tokenStore interface {
	Get(host string) (string, bool)
}

// Option configures a [Provider].
type Option func(*Provider)

// WithTransport overrides the HTTP transport, for tests.
func WithTransport(rt http.RoundTripper) Option {
	return func(p *Provider) { p.transport = rt }
}

// WithStore sets the token store the credential chain reads the clover-minted
// token from, for tests.
func WithStore(s tokenStore) Option {
	return func(p *Provider) { p.store = s }
}

// New returns the GitHub provider, wiring the token store the credential chain
// reads from. A store that cannot be located (no config dir) is left nil, so the
// chain simply skips that rung.
func New(opts ...Option) *Provider {
	p := &Provider{}
	if store, err := token.New(); err == nil {
		p.store = store
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name identifies the provider.
func (p *Provider) Name() string { return constant.ProviderGithub }

// Keys reports the directive keys GitHub accepts, in canonical order.
func (p *Provider) Keys() []provider.Key {
	return []provider.Key{
		{Name: keyRepository, Required: true},
		{Name: keySource},
	}
}

// Resource validates a directive into a GitHub resource.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	repo, ok := d.Get(keyRepository)
	if !ok {
		return nil, fmt.Errorf("github: %s is required", keyRepository)
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return nil, fmt.Errorf("github: %s must be owner/name, got %q", keyRepository, repo)
	}

	source := sourceTags
	if s, ok := d.Get(keySource); ok {
		if s != sourceTags && s != sourceReleases {
			return nil, fmt.Errorf(
				"github: %s must be %s or %s, got %q",
				keySource,
				sourceTags,
				sourceReleases,
				s,
			)
		}
		source = s
	}

	// asset= filters on release asset filenames, which only releases publish.
	if _, ok := d.Get(constant.RuleAsset); ok && source != sourceReleases {
		return nil, fmt.Errorf(
			"github: %s= requires %s=%s",
			constant.RuleAsset,
			keySource,
			sourceReleases,
		)
	}

	return resource{owner: owner, name: name, source: source}, nil
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(r provider.Resource) string {
	res, ok := r.(resource)
	if !ok {
		return constant.ProviderGithub
	}
	return fmt.Sprintf("%s/%s/%s (%s)", host, res.owner, res.name, res.source)
}

// Authenticate reports whether a credential is available from any source in the
// chain. It does not verify the token over the network - only that one is
// present - and never blocks on a prompt. Absence is reported as errAnonymous
// rather than a hard failure, since anonymous reads still work (just
// rate-limited).
func (p *Provider) Authenticate(context.Context) error {
	if p.credential() != "" {
		return nil
	}
	return errAnonymous
}

// AuthHint returns how to authenticate when no credential is found.
func (p *Provider) AuthHint() string {
	return "run `clover login github` or `gh auth login`, " +
		"or set CLOVER_GITHUB_TOKEN, for higher rate limits and private repositories"
}

// credential resolves the access token from the source chain, first non-empty
// wins: the CLOVER_GITHUB_TOKEN env var, then the clover-minted token, then a
// gh-compatible token. An empty result means anonymous access. cmp.Or skips
// empty values, so a stale empty stored token never shadows the gh token.
func (p *Provider) credential() string {
	var stored string
	if p.store != nil {
		stored, _ = p.store.Get(host)
	}
	gh, _ := auth.TokenForHost(host)
	return cmp.Or(os.Getenv(tokenEnv), stored, gh)
}

// resource is GitHub's validated descriptor.
type resource struct {
	owner  string
	name   string
	source string
}

// client lazily builds the REST client, resolving the gh-compatible token and
// wrapping the transport with caching and rate-limit handling. It is built once
// and reused, so the cache and rate-limit state are shared across the run.
func (p *Provider) client() (*api.RESTClient, error) {
	p.once.Do(func() {
		transport := p.transport
		if transport == nil {
			transport = httpcache.New(
				httpcache.WithTransport(ratelimit.New(nil, rateHeaders)),
			).Transport
		}
		p.rest, p.restErr = api.NewRESTClient(api.ClientOptions{
			Host:      host,
			AuthToken: p.credential(),
			Transport: transport,
		})
	})
	return p.rest, p.restErr
}
