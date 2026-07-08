package github

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/ratelimit"
	"github.com/gechr/clover/internal/token"
)

// tokenEnv is the namespaced environment variable that overrides every other
// credential source, deliberately not GITHUB_TOKEN/GH_TOKEN which clash with
// other tools.
const tokenEnv = "CLOVER_GITHUB_TOKEN" //nolint:gosec // env var name, not a credential

// hostEnv binds the host-independent PAT to a single host. Because a PAT is sent
// to whichever host a marker names, a marker-controlled host= could otherwise
// redirect the token to an attacker; the PAT is attached only when the marker's
// host matches this (default github.com).
const hostEnv = "CLOVER_GITHUB_HOST"

// defaultHost is the host the provider targets when host is omitted: GitHub.com,
// whose API lives at api.github.com. A GitHub Enterprise Server host instead
// serves its API under https://<host>/api/v3 and /api/graphql.
const defaultHost = "github.com"

// errAnonymous reports that no GitHub credentials were found, so requests fall
// back to anonymous (rate-limited) access. It is informational, not fatal:
// public reads still work.
var errAnonymous = errors.New("no GitHub credentials; using anonymous access")

// Directive keys the provider accepts beyond the forge-shared source key.
const (
	keyRepository = constant.DirectiveRepository
	keyHost       = constant.DirectiveHost
)

// rateHeaders describes GitHub's rate-limit headers for the ratelimit transport.
var rateHeaders = ratelimit.Headers{
	Remaining:  "X-RateLimit-Remaining",
	Reset:      "X-RateLimit-Reset",
	ResetKind:  ratelimit.ResetEpoch,
	RetryAfter: "Retry-After",
}

// Provider resolves versions from GitHub tags or releases. The host is a
// per-marker value (github.com or a GitHub Enterprise Server instance), so the
// REST client is host-agnostic - each request carries its own absolute URL and
// its own per-host token - while one shared, cached, rate-limited transport
// covers every marker in a run. The REST client serves the anonymous path
// (GraphQL rejects unauthenticated requests); a GraphQL client, built per host
// since go-gh maps the host to its /api/graphql endpoint, lists tags in a real
// newest-first order when a credential is present.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached, rate-limited default
	tokenOpt  string            // injected host-bound PAT, for tests; bypasses the env chain
	store     tokenStore        // reads the clover-minted token; nil falls through the chain

	once     sync.Once
	resolved http.RoundTripper // the transport actually in use (test override or cached default)
	rest     forge.RESTClient
	gqlCache sync.Map // host -> *gqlEntry, building each host's GraphQL client once
}

// gqlEntry memoizes a host's GraphQL client (or the error building it) so a run
// builds each host's client at most once.
type gqlEntry struct {
	client *api.GraphQLClient
	err    error
}

// tokenStore is the read side of the token store the credential chain consults.
type tokenStore interface {
	Get(host string) (string, bool)
}

// Option configures a [Provider].
type Option func(*Provider)

// WithTransport overrides the HTTP transport, for tests. It also pins credential
// resolution to the explicit store (see credential), so a test never reaches the
// network and its auth path - GraphQL vs anonymous REST - is fully deterministic.
func WithTransport(rt http.RoundTripper) Option {
	return func(p *Provider) { p.transport = rt }
}

// WithToken injects a host-bound PAT credential directly, for tests exercising
// the authenticated path (and the exfil guard) without reading the machine's
// environment.
func WithToken(tok string) Option {
	return func(p *Provider) { p.tokenOpt = tok }
}

// WithStore sets the token store the credential chain reads the clover-minted
// token from, for tests.
func WithStore(s tokenStore) Option {
	return func(p *Provider) { p.store = s }
}

// New returns the GitHub provider, wiring the token store the credential chain
// reads from. A store that cannot be located (no config dir) is left nil, so the
// chain simply skips that rung. The default keychain store is wired only on the
// real transport: a test transport keeps auth explicit (via WithStore), so the
// machine's stored token never leaks into a test's auth path.
func New(opts ...Option) *Provider {
	p := &Provider{}
	for _, opt := range opts {
		opt(p)
	}
	if p.store == nil && p.transport == nil {
		if store, err := token.New(); err == nil {
			p.store = store
		}
	}
	return p
}

// Name identifies the provider.
func (p *Provider) Name() string { return constant.ProviderGithub }

// Dated marks the listing as date-bearing: releases carry a publication date.
// Bare tags do not, and fall to the post-discovery date check.
func (p *Provider) Dated() {}

// Keys reports the directive keys GitHub accepts, in canonical order.
func (p *Provider) Keys() []provider.Key {
	return []provider.Key{
		{Name: keyRepository, Required: true},
		{Name: keyHost},
		{Name: forge.KeySource},
	}
}

// Resource validates a directive into a GitHub resource.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	owner, name, err := forge.OwnerName(constant.ProviderGithub, d)
	if err != nil {
		return nil, err
	}

	host, err := forge.Host(constant.ProviderGithub, d, defaultHost)
	if err != nil {
		return nil, err
	}

	source, err := forge.Source(constant.ProviderGithub, d)
	if err != nil {
		return nil, err
	}

	if err := forge.RequireReleasesForAsset(constant.ProviderGithub, d, source); err != nil {
		return nil, err
	}

	return resource{host: host, owner: owner, name: name, source: source}, nil
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(r provider.Resource) string {
	res, ok := r.(resource)
	if !ok {
		return constant.ProviderGithub
	}
	return fmt.Sprintf("%s/%s/%s (%s)", res.host, res.owner, res.name, res.source)
}

// Authenticate reports whether a credential is available for the default host,
// without verifying it over the network or blocking on a prompt. A login stored
// under a non-default host is keyed by host and only resolved at discovery, so
// this may under-report for those. Absence is reported as errAnonymous rather
// than a hard failure, since anonymous reads still work (just rate-limited).
func (p *Provider) Authenticate(context.Context) error {
	if p.credential(defaultHost) != "" {
		return nil
	}
	return errAnonymous
}

// AuthHint returns how to authenticate when no credential is found.
func (p *Provider) AuthHint() string {
	return "for higher rate limits and private repositories, " +
		"run `clover login github` or set `CLOVER_GITHUB_TOKEN`"
}

// credential resolves the access token for a host, first non-empty wins: the
// host-bound CLOVER_GITHUB_TOKEN env var (sent only to the host it is bound to,
// so a marker-controlled host= cannot redirect it), then the clover-minted token
// stored under the host, then a gh-compatible token (go-gh resolves these per
// host, reading the enterprise env vars and gh config for a GHES host). An empty
// result means anonymous access; cmp.Or skips empty values, so a stale empty
// stored token never shadows the gh token.
func (p *Provider) credential(host string) string {
	var stored string
	if p.store != nil {
		stored, _ = p.store.Get(host)
	}
	bound := host == p.patHost()
	// A test transport pins the environment: only the injected PAT (host-bound)
	// and the explicit store are read, never ambient gh config or env vars, so a
	// test stays hermetic and its auth path is deterministic.
	if p.transport != nil {
		if bound && p.tokenOpt != "" {
			return p.tokenOpt
		}
		return stored
	}
	var env string
	if bound {
		env = os.Getenv(tokenEnv)
	}
	gh, _ := auth.TokenForHost(host)
	return cmp.Or(env, stored, gh)
}

// patHost is the single host the host-independent PAT may be sent to:
// CLOVER_GITHUB_HOST, defaulting to github.com. A test transport pins it to the
// default, ignoring ambient env.
func (p *Provider) patHost() string {
	return forge.PATHost(hostEnv, defaultHost, p.transport != nil)
}

// resource is GitHub's validated descriptor: the forge host plus the owner/name
// repo and whether to list its tags or releases.
type resource struct {
	host   string
	owner  string
	name   string
	source string
}

// initClients lazily builds the shared transport and the host-agnostic REST
// client once per run. The transport is cached and rate-limited; the REST client
// holds no host or token, attaching both per request, so one client serves every
// marker whatever host it names.
func (p *Provider) initClients() {
	p.once.Do(func() {
		transport := p.transport
		if transport == nil {
			transport = httpcache.New(
				httpcache.WithTransport(ratelimit.New(httpcache.NewBaseTransport(), rateHeaders)),
			).Transport
		}
		p.resolved = transport
		p.rest = forge.NewRESTClient(
			&http.Client{Transport: transport},
			"application/vnd.github+json",
		)
	})
}

// client returns the lazily-built REST client. It is always available, so this
// never errors - anonymous access is valid (just rate-limited).
func (p *Provider) client() forge.RESTClient {
	p.initClients()
	return p.rest
}

// gqlClient returns the GraphQL client for a host, building it once per host over
// the shared transport. go-gh maps the host to its endpoint - api.github.com for
// github.com, https://<host>/api/graphql for a GHES host - so the same query
// shape serves every instance. Used for ordered tag listing on the authenticated
// path.
func (p *Provider) gqlClient(host string) (*api.GraphQLClient, error) {
	p.initClients()
	if e, ok := p.gqlCache.Load(host); ok {
		entry, _ := e.(*gqlEntry)
		return entry.client, entry.err
	}
	opts := api.ClientOptions{
		Host:      host,
		AuthToken: p.credential(host),
		Transport: p.resolved,
	}
	client, err := api.NewGraphQLClient(opts)
	actual, _ := p.gqlCache.LoadOrStore(host, &gqlEntry{client: client, err: err})
	entry, _ := actual.(*gqlEntry)
	return entry.client, entry.err
}

// authenticated reports whether a credential is available for a host, so tag
// listing can choose the GraphQL ordered path over anonymous REST.
func (p *Provider) authenticated(host string) bool { return p.credential(host) != "" }
