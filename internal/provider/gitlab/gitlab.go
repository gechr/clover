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

// errAnonymous reports that no GitLab credentials were found, so requests fall
// back to anonymous (rate-limited) access. It is informational, not fatal:
// public reads still work.
var errAnonymous = errors.New("no GitLab credentials; using anonymous access")

// host is the GitLab host requests target and credentials are stored under.
const host = "gitlab.com"

// Directive keys and the values the source key accepts.
const (
	keyRepository = constant.DirectiveRepository
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

// Provider resolves versions from a GitLab project's tags or releases. The tags
// REST endpoint accepts order_by=updated&sort=desc, so the listing is genuinely
// newest-first without the GraphQL detour GitHub needs - one cached, rate-limited
// REST client covers every marker in a run.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached, rate-limited default
	store     tokenStore        // reads the clover-minted token; nil falls through the chain

	rest *restClient
}

// tokenStore is the read side of the token store the credential chain consults.
type tokenStore interface {
	Get(host string) (string, bool)
}

// Option configures a [Provider].
type Option func(*Provider)

// WithTransport overrides the HTTP transport, for tests. It also pins credential
// resolution to the explicit store (see credential), so a test never reaches the
// network and its auth path stays deterministic.
func WithTransport(rt http.RoundTripper) Option {
	return func(p *Provider) { p.transport = rt }
}

// WithStore sets the token store the credential chain reads the clover-minted
// token from, for tests.
func WithStore(s tokenStore) Option {
	return func(p *Provider) { p.store = s }
}

// New returns the GitLab provider, wiring the token store the credential chain
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

	transport := p.transport
	if transport == nil {
		transport = httpcache.New(
			httpcache.WithTransport(ratelimit.New(nil, rateHeaders)),
		).Transport
	}
	p.rest = &restClient{
		httpClient: &http.Client{Transport: transport},
		token:      p.credential(),
	}
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

	return resource{repository: repo, source: source}, nil
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(r provider.Resource) string {
	res, ok := r.(resource)
	if !ok {
		return constant.ProviderGitlab
	}
	return fmt.Sprintf("%s/%s (%s)", host, res.repository, res.source)
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
	return "for higher rate limits and private projects, " +
		"run `clover login gitlab` or set `CLOVER_GITLAB_TOKEN`"
}

// credential resolves the access token from the source chain, first non-empty
// wins: the CLOVER_GITLAB_TOKEN env var, then the clover-minted token, then the
// ecosystem GITLAB_TOKEN. An empty result means anonymous access. cmp.Or skips
// empty values, so a stale empty stored token never shadows GITLAB_TOKEN.
func (p *Provider) credential() string {
	var stored string
	if p.store != nil {
		stored, _ = p.store.Get(host)
	}
	// A test transport pins the environment: only the explicit store is read,
	// never ambient env vars, so a test stays hermetic and its auth path is
	// deterministic.
	if p.transport != nil {
		return stored
	}
	return cmp.Or(os.Getenv(tokenEnv), stored, os.Getenv(fallbackEnv))
}

// resource is GitLab's validated descriptor: the project path and whether to
// list its tags or releases.
type resource struct {
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
