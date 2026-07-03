package gitea

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/token"
)

// tokenEnv is the namespaced environment variable that overrides every other
// credential source. GITEA_TOKEN is read too (the ecosystem default the tea CLI
// uses), but only after the clover-namespaced var, so a project's CI token cannot
// silently shadow an explicit clover credential.
const tokenEnv = "CLOVER_GITEA_TOKEN" //nolint:gosec // env var name, not a credential

// fallbackEnv is the ecosystem-standard token variable, consulted last.
const fallbackEnv = "GITEA_TOKEN"

// hostEnv binds the host-independent PAT to a single host. Because a PAT is sent
// to whichever host a marker names, a marker-controlled host= could otherwise
// redirect the token to an attacker; the PAT is attached only when the marker's
// host matches this (default codeberg.org).
const hostEnv = "CLOVER_GITEA_HOST"

// defaultHost is the forge the provider targets when host is omitted. Codeberg is
// the flagship public Forgejo instance, so it is the natural default for a
// provider whose whole point is forge-agnostic discovery.
const defaultHost = "codeberg.org"

// errAnonymous reports that no Gitea credentials were found, so requests fall
// back to anonymous (rate-limited) access. It is informational, not fatal: public
// reads still work.
var errAnonymous = errors.New("no Gitea credentials; using anonymous access")

// Directive keys the provider accepts beyond the forge-shared source key.
const (
	keyRepository = constant.DirectiveRepository
	keyHost       = constant.DirectiveHost
)

// Provider resolves versions from a Gitea/Forgejo project's tags or releases over
// a single cached REST client shared across a run. The host is per-marker (the
// API is the same on every instance), so the client is host-agnostic, each
// request carries its own absolute URL, and the credential is resolved per host -
// an env-var PAT is host-independent, but a token minted by `clover login` is
// stored under the host it authenticated.
type Provider struct {
	transport http.RoundTripper // overridable for tests; nil uses the cached default
	tokenOpt  string            // injected PAT, for tests; bypasses the env chain
	store     tokenStore        // reads/refreshes the clover-minted login; nil skips that rung

	rest      forge.RESTClient
	refreshMu sync.Map // host -> *sync.Mutex, serializing token refresh per host
}

// tokenStore is the read/write side of the token store: read a stored login, and
// write back the rotated token after a refresh.
type tokenStore interface {
	Get(host string) (string, bool)
	Set(host, token string) error
}

// Option configures a [Provider].
type Option func(*Provider)

// WithTransport overrides the HTTP transport, for tests. It also pins credential
// resolution away from ambient env vars (see staticCredential), so a test never
// reaches the network and its auth path stays deterministic.
func WithTransport(rt http.RoundTripper) Option {
	return func(p *Provider) { p.transport = rt }
}

// WithToken injects a PAT credential directly, for tests exercising the
// authenticated path without reading the machine's environment.
func WithToken(tok string) Option {
	return func(p *Provider) { p.tokenOpt = tok }
}

// WithStore sets the token store the credential chain reads a minted login from,
// for tests.
func WithStore(s tokenStore) Option {
	return func(p *Provider) { p.store = s }
}

// New returns the Gitea provider. A token comes from CLOVER_GITEA_TOKEN, a login
// minted by `clover login gitea` (stored per host), or anonymous access applies.
// The default keychain store is wired only on the real transport: a test
// transport keeps auth explicit (WithToken/WithStore), so the machine's stored
// token never leaks into a test.
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
		transport = httpcache.New().Transport
	}
	p.rest = forge.NewRESTClient(&http.Client{Transport: transport}, "application/json")
	return p
}

// Name identifies the provider.
func (p *Provider) Name() string { return constant.ProviderGitea }

// Keys reports the directive keys Gitea accepts, in canonical order.
func (p *Provider) Keys() []provider.Key {
	return []provider.Key{
		{Name: keyRepository, Required: true},
		{Name: keyHost},
		{Name: forge.KeySource},
	}
}

// Resource validates a directive into a Gitea resource. The repository is a
// strict owner/name: Gitea and Forgejo organize repos under a flat owner, with no
// nested subgroups, unlike GitLab.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	owner, name, err := forge.OwnerName(constant.ProviderGitea, d)
	if err != nil {
		return nil, err
	}

	host, err := forge.Host(constant.ProviderGitea, d, defaultHost)
	if err != nil {
		return nil, err
	}

	source, err := forge.Source(constant.ProviderGitea, d)
	if err != nil {
		return nil, err
	}

	if err := forge.RequireReleasesForAsset(constant.ProviderGitea, d, source); err != nil {
		return nil, err
	}

	return resource{host: host, owner: owner, name: name, source: source}, nil
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(r provider.Resource) string {
	res, ok := r.(resource)
	if !ok {
		return constant.ProviderGitea
	}
	return fmt.Sprintf("%s/%s/%s (%s)", res.host, res.owner, res.name, res.source)
}

// Authenticate reports whether a credential is available, without verifying it
// over the network or blocking on a prompt. It sees the host-independent PAT and a
// login stored under the default host; a login under a non-default host is keyed
// by host and only resolved at discovery, so this may under-report for those.
// Absence is reported as errAnonymous - informational, not fatal, since anonymous
// reads still work.
func (p *Provider) Authenticate(context.Context) error {
	if p.staticCredential() != "" {
		return nil
	}
	if p.store != nil {
		if _, ok := p.store.Get(defaultHost); ok {
			return nil
		}
	}
	return errAnonymous
}

// AuthHint returns how to authenticate when no credential is found.
func (p *Provider) AuthHint() string {
	return "for higher rate limits and private repositories, " +
		"run `clover login gitea` or set `CLOVER_GITEA_TOKEN`"
}

// auth resolves the credential for a host into a token and the Authorization
// scheme it uses. A PAT (env var or test) is sent as `token`, but only to the one
// host it is bound to - a marker-controlled host= must not redirect it. A login
// minted by `clover login` is an OAuth access token sent as `Bearer`, refreshed
// and re-persisted when it has expired. An empty token means anonymous access.
// authorization resolves the credential for a host into a ready Authorization
// header value; empty means anonymous access.
func (p *Provider) authorization(ctx context.Context, host string) string {
	token, scheme := p.auth(ctx, host)
	return forge.Authorization(scheme, token)
}

func (p *Provider) auth(ctx context.Context, host string) (string, string) {
	if pat := p.staticCredential(); pat != "" && host == p.patHost() {
		return pat, "token"
	}
	if p.store == nil {
		return "", ""
	}
	c, ok := p.storedCreds(host)
	if !ok {
		return "", ""
	}
	if !c.expired() {
		return c.AccessToken, "Bearer"
	}
	if c.RefreshToken == "" {
		return "", "" // unrenewable; fall back to anonymous
	}
	return p.refreshAndStore(ctx, host, c)
}

// refreshAndStore renews an expired credential under a per-host lock, so
// concurrent markers for the same host spend the rotating refresh token once. It
// re-reads the store after locking: a sibling goroutine may already have
// refreshed, in which case that fresh credential is used and no second refresh is
// spent.
func (p *Provider) refreshAndStore(
	ctx context.Context,
	host string,
	expired creds,
) (string, string) {
	mu := p.hostLock(host)
	mu.Lock()
	defer mu.Unlock()

	if c, ok := p.storedCreds(host); ok && !c.expired() {
		return c.AccessToken, "Bearer" // refreshed by another marker while we waited
	}
	refreshed, err := refreshCreds(ctx, p.rest.HTTPClient(), host, expired)
	if err != nil {
		return "", "" // refresh failed; fall back to anonymous
	}
	//nolint:gosec // persisting the minted credential is the point
	blob, err := json.Marshal(refreshed)
	if err == nil {
		_ = p.store.Set(host, string(blob))
	}
	return refreshed.AccessToken, "Bearer"
}

// storedCreds reads and decodes the login stored under host, if any.
func (p *Provider) storedCreds(host string) (creds, bool) {
	raw, ok := p.store.Get(host)
	if !ok {
		return creds{}, false
	}
	var c creds
	if err := json.Unmarshal([]byte(raw), &c); err != nil || c.AccessToken == "" {
		return creds{}, false
	}
	return c, true
}

// hostLock returns the refresh mutex for a host, creating it once.
func (p *Provider) hostLock(host string) *sync.Mutex {
	mu, _ := p.refreshMu.LoadOrStore(host, &sync.Mutex{})
	lock, _ := mu.(*sync.Mutex)
	return lock
}

// patHost is the single host a PAT may be sent to: CLOVER_GITEA_HOST, defaulting
// to codeberg.org. A test transport pins it to the default, ignoring ambient env.
func (p *Provider) patHost() string {
	return forge.PATHost(hostEnv, defaultHost, p.transport != nil)
}

// staticCredential resolves a host-independent PAT, first non-empty wins: an
// injected test token, then CLOVER_GITEA_TOKEN, then the ecosystem GITEA_TOKEN. A
// test transport pins resolution away from ambient env vars so a test stays
// hermetic.
func (p *Provider) staticCredential() string {
	if p.tokenOpt != "" {
		return p.tokenOpt
	}
	if p.transport != nil {
		return ""
	}
	return cmp.Or(os.Getenv(tokenEnv), os.Getenv(fallbackEnv))
}

// resource is Gitea's validated descriptor: the forge host, the owner/name repo,
// and whether to list its tags or releases.
type resource struct {
	host   string
	owner  string
	name   string
	source string
}
