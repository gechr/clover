package gitlab

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/ratelimit"
	"github.com/gechr/clover/internal/token"
)

// tokenEnv is the namespaced environment variable that overrides every other
// credential source. GITLAB_TOKEN is read too (the ecosystem default glab uses),
// but only after the clover-namespaced var and stored token, so a project's CI
// token cannot silently shadow an explicit clover credential.
const tokenEnv = "CLOVER_GITLAB_TOKEN"

// fallbackEnv is the ecosystem-standard token variable, consulted last.
const fallbackEnv = "GITLAB_TOKEN"

// hostEnv binds the host-independent PAT to a single host. Because a PAT is sent
// to whichever host a marker names, a marker-controlled host= could otherwise
// redirect the token to an attacker; the PAT is attached only when the marker's
// host matches this (default gitlab.com).
const hostEnv = "CLOVER_GITLAB_HOST"

// defaultHost is the host the provider targets when host is omitted: gitlab.com,
// whose API lives at /api/v4. A self-managed GitLab host serves the same API
// under https://<host>/api/v4.
const defaultHost = "gitlab.com"

// errAnonymous reports that no GitLab credentials were found, so requests fall
// back to anonymous (rate-limited) access. It is informational, not fatal:
// public reads still work.
var errAnonymous = errors.New("no GitLab credentials; using anonymous access")

// Directive keys and the values the source key accepts.
const (
	keyRepository = constant.DirectiveRepository
	keyHost       = constant.DirectiveHost
	keySource     = "source"

	sourceTags     = "tags"
	sourceReleases = "releases"
)

// rateHeaders describes GitLab.com's rate-limit headers for the ratelimit
// transport. GitLab uses the RateLimit-* convention with an epoch reset, unlike
// GitHub's X-RateLimit-* spelling.
var rateHeaders = ratelimit.Headers{
	Remaining:  "RateLimit-Remaining",
	Reset:      "RateLimit-Reset",
	ResetKind:  ratelimit.ResetEpoch,
	RetryAfter: "Retry-After",
}

// Provider resolves versions from a GitLab project's tags or releases. The host
// is a per-marker value (gitlab.com or a self-managed instance), so the REST
// client is host-agnostic - each request carries its own absolute URL and its own
// per-host token - while one shared, cached, rate-limited transport covers every
// marker in a run. The tags REST endpoint accepts order_by=version&sort=desc, so
// the listing is genuinely newest-first without the GraphQL detour GitHub needs.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached, rate-limited default
	tokenOpt  string            // injected host-bound PAT, for tests; bypasses the env chain
	store     tokenStore        // reads the clover-minted token; nil falls through the chain

	rest forge.RESTClient
}

// tokenStore is the read side of the token store the credential chain consults.
type tokenStore interface {
	Get(host string) (string, bool)
}

// Option configures a [Provider].
type Option func(*Provider)

// WithTransport overrides the HTTP transport, for tests. It also pins credential
// resolution to the injected PAT and explicit store (see credential), so a test
// never reaches the network and its auth path stays deterministic.
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

// New returns the GitLab provider, wiring the token store the credential chain
// reads from. A store that cannot be located (no config dir) is left nil, so the
// chain simply skips that rung. The default keychain store is wired only on the
// real transport: a test transport keeps auth explicit (via WithToken/WithStore),
// so the machine's stored token never leaks into a test's auth path.
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

	transport := p.transport
	if transport == nil {
		transport = httpcache.New(
			httpcache.WithTransport(ratelimit.New(nil, rateHeaders)),
		).Transport
	}
	p.rest = forge.NewRESTClient(&http.Client{Transport: transport}, "application/json")
	return p
}

// Name identifies the provider.
func (p *Provider) Name() string { return constant.ProviderGitlab }

// RecencyOrdered marks the listing as newest-first, so a shallow lookup always
// holds the latest version; --deep is hinted only when a constrained marker finds
// no candidate while more pages remained.
func (p *Provider) RecencyOrdered() {}

// Keys reports the directive keys GitLab accepts, in canonical order.
func (p *Provider) Keys() []provider.Key {
	return []provider.Key{
		{Name: keyRepository, Required: true},
		{Name: keyHost},
		{Name: keySource},
	}
}

// Resource validates a directive into a GitLab resource. The repository is the
// project's full path: at least namespace/project, with nested groups allowed
// (group/subgroup/project), unlike GitHub's strict owner/name.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	repo, ok := d.Get(keyRepository)
	if !ok {
		return nil, fmt.Errorf("gitlab: %q is required", keyRepository)
	}
	if !strings.Contains(repo, "/") {
		return nil, fmt.Errorf(
			"gitlab: %q must be a namespace/project path, got %q",
			keyRepository,
			repo,
		)
	}
	if slices.Contains(strings.Split(repo, "/"), "") {
		return nil, fmt.Errorf("gitlab: %q has an empty path segment: %q", keyRepository, repo)
	}

	host := defaultHost
	if h, ok := d.Get(keyHost); ok {
		nh, valid := forge.NormalizeHost(h)
		if !valid {
			return nil, fmt.Errorf("gitlab: %q must be a valid host, got %q", keyHost, h)
		}
		host = nh
	}

	source := sourceTags
	if s, ok := d.Get(keySource); ok {
		if s != sourceTags && s != sourceReleases {
			return nil, fmt.Errorf(
				"gitlab: %q must be %s or %s, got %q",
				keySource,
				sourceTags,
				sourceReleases,
				s,
			)
		}
		source = s
	}

	// asset= filters on release asset names, which only releases publish; a tag
	// candidate has none, so the filter would always fail later. Reject it up front,
	// mirroring the github provider.
	if _, ok := d.Get(constant.RuleAsset); ok && source != sourceReleases {
		return nil, fmt.Errorf(
			"gitlab: %q requires %q to be %q",
			constant.RuleAsset,
			keySource,
			sourceReleases,
		)
	}

	return resource{host: host, repository: repo, source: source}, nil
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(r provider.Resource) string {
	res, ok := r.(resource)
	if !ok {
		return constant.ProviderGitlab
	}
	return fmt.Sprintf("%s/%s (%s)", res.host, res.repository, res.source)
}

// Authenticate reports whether a credential is available from any source in the
// chain. It does not verify the token over the network - only that one is
// present - and never blocks on a prompt. Absence is reported as errAnonymous
// rather than a hard failure, since anonymous reads still work (just
// rate-limited).
func (p *Provider) Authenticate(context.Context) error {
	if p.credential(defaultHost) != "" {
		return nil
	}
	return errAnonymous
}

// AuthHint returns how to authenticate when no credential is found.
func (p *Provider) AuthHint() string {
	return "for higher rate limits and private projects, " +
		"run `clover login gitlab` or set `CLOVER_GITLAB_TOKEN`"
}

// credential resolves the access token for a host, first non-empty wins: the
// host-bound CLOVER_GITLAB_TOKEN env var (sent only to the host it is bound to,
// so a marker-controlled host= cannot redirect it), then the clover-minted token
// stored under the host, then the ecosystem GITLAB_TOKEN (also host-bound, since
// it names no host of its own). An empty result means anonymous access; cmp.Or
// skips empty values, so a stale empty stored token never shadows GITLAB_TOKEN.
func (p *Provider) credential(host string) string {
	var stored string
	if p.store != nil {
		stored, _ = p.store.Get(host)
	}
	bound := host == p.patHost()
	// A test transport pins the environment: only the injected PAT (host-bound)
	// and the explicit store are read, never ambient env vars, so a test stays
	// hermetic and its auth path is deterministic.
	if p.transport != nil {
		if bound && p.tokenOpt != "" {
			return p.tokenOpt
		}
		return stored
	}
	var env, fallback string
	if bound {
		env, fallback = os.Getenv(tokenEnv), os.Getenv(fallbackEnv)
	}
	return cmp.Or(env, stored, fallback)
}

// patHost is the single host the host-independent PAT may be sent to:
// CLOVER_GITLAB_HOST, defaulting to gitlab.com. A test transport pins it to the
// default, ignoring ambient env.
func (p *Provider) patHost() string {
	return forge.PATHost(hostEnv, defaultHost, p.transport != nil)
}

// resource is GitLab's validated descriptor: the forge host, the project path,
// and whether to list its tags or releases.
type resource struct {
	host       string
	repository string
	source     string
}

// projectID is the URL-encoded project path the REST API addresses a project by,
// with each path separator escaped to %2F (group%2Fsubgroup%2Fproject).
func (res resource) projectID() string {
	segments := strings.Split(res.repository, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "%2F")
}
