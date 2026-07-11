package http

import (
	"fmt"
	nethttp "net/http"
	"net/url"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/jq"
	"github.com/gechr/clover/internal/pattern"
	"github.com/gechr/clover/internal/provider"
	"github.com/itchyny/gojq"
)

// Directive keys the http provider accepts: the endpoint to fetch, exactly one
// extraction key naming how to pull version candidate(s) from its response, and
// an optional User-Agent override.
const (
	keyURL       = constant.DirectiveURL
	keyJQ        = constant.DirectiveJQ
	keyExtract   = constant.DirectiveExtract
	keyUserAgent = constant.DirectiveUserAgent
)

// userAgentFor builds the default User-Agent identifying clover to endpoints that
// reject or throttle a bare Go client. The binary's version is injected at
// construction (it is unknown to a leaf package); an unresolved version drops the
// suffix. A directive's user-agent= overrides the result per marker.
func userAgentFor(version string) string {
	if version == "" {
		return "Clover"
	}
	return "Clover v" + version
}

// Provider resolves versions from an arbitrary HTTP endpoint. It performs one
// anonymous GET of the configured url and extracts candidate version strings
// from the response, leaving selection to the framework; it lists candidates and
// nothing more, so it carries no per-version metadata.
type Provider struct {
	transport nethttp.RoundTripper // overridable for tests; nil uses the cached default
	userAgent string               // default User-Agent; a marker's user-agent= overrides it

	client *nethttp.Client
}

// New returns the http provider. The endpoint is fetched anonymously through the
// shared cache, with no ratelimit wrapper or credentials.
func New(opts ...Option) *Provider {
	p := &Provider{userAgent: userAgentFor("")}
	for _, opt := range opts {
		opt(p)
	}
	var cacheOpts []httpcache.Option
	if p.transport != nil {
		cacheOpts = append(cacheOpts, httpcache.WithTransport(p.transport))
	}
	p.client = httpcache.New(cacheOpts...)
	return p
}

// Name identifies the provider.
func (p *Provider) Name() string { return constant.ProviderHTTP }

// Keys reports the directive keys http accepts, in canonical order. The two
// extraction keys are mutually exclusive; Resource enforces that exactly one is
// set, so neither carries the Required flag.
func (p *Provider) Keys() []provider.Key {
	return []provider.Key{
		{Name: keyURL, Required: true},
		{Name: keyJQ},
		{Name: keyExtract},
		{Name: keyUserAgent},
	}
}

// Resource validates a directive into an http resource: a required url and
// exactly one extraction key, whose expression is compiled here so a malformed
// jq or pattern fails validation (and lint) rather than mid-fetch.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	raw, ok := d.Get(keyURL)
	if !ok {
		return nil, fmt.Errorf("http: %q is required", keyURL)
	}
	if err := validateURL(raw); err != nil {
		return nil, err
	}

	jqExpr, hasJQ := d.Get(keyJQ)
	extractExpr, hasExtract := d.Get(keyExtract)
	if hasJQ == hasExtract {
		return nil, fmt.Errorf("http: set exactly one of %q or %q", keyJQ, keyExtract)
	}

	userAgent := p.userAgent
	if ua, ok := d.Get(keyUserAgent); ok && ua != "" {
		userAgent = ua
	}

	res := resource{url: raw, userAgent: userAgent}
	if hasJQ {
		code, err := compileJQ(jqExpr)
		if err != nil {
			return nil, err
		}
		res.kind, res.jq = extractJQ, code
		return res, nil
	}

	pat, err := compileExtract(extractExpr)
	if err != nil {
		return nil, err
	}
	res.kind, res.pattern = extractPattern, pat
	return res, nil
}

// Describe returns a human-readable label for a resource: the endpoint's host.
func (p *Provider) Describe(r provider.Resource) string {
	res, ok := r.(resource)
	if !ok {
		return constant.ProviderHTTP
	}
	if u, err := url.Parse(res.url); err == nil && u.Host != "" {
		return u.Host
	}
	return res.url
}

// validateURL rejects a url that is not a well-formed, anonymous http(s)
// endpoint, before it reaches the transport: a non-http scheme, a missing
// hostname (Host alone admits ":443"), or embedded credentials (the provider is
// anonymous by design, and userinfo would leak into error strings).
func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("http: invalid %q %q: %w", keyURL, raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("http: %q %q must be http or https", keyURL, raw)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("http: %q %q has no host", keyURL, raw)
	}
	if u.User != nil {
		return fmt.Errorf("http: %q %q must not embed credentials", keyURL, raw)
	}
	return nil
}

// compileExtract compiles an extract pattern and constrains the glob dialect to
// a single whole-version capture: a glob must carry exactly one <version> token
// and no component token (<major>/<minor>/<patch>), since the matcher takes only
// one capture per match and a fragmented version would yield a partial value. A
// /regex/ is unconstrained - its group 1 (or whole match) is the version.
func compileExtract(expr string) (*pattern.Pattern, error) {
	if expr == "" {
		return nil, fmt.Errorf("http: %q must not be empty", keyExtract)
	}
	pat, err := pattern.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("http: %q: %w", keyExtract, err)
	}
	if pat.Kind() != pattern.KindGlob {
		return pat, nil
	}

	hasVersion := false
	for _, t := range pat.Tokens() {
		//nolint:exhaustive // only the version-family tokens are constrained; others are valid context.
		switch t {
		case pattern.TokenVersion:
			hasVersion = true
		case pattern.TokenMajor,
			pattern.TokenMinor,
			pattern.TokenPatch,
			pattern.TokenMajorMinor,
			pattern.TokenMajorMinorPatch:
			return nil, fmt.Errorf(
				"http: %q glob must capture the whole version with <%s>, not the component <%s>",
				keyExtract, pattern.TokenVersion, t,
			)
		}
	}
	if !hasVersion {
		return nil, fmt.Errorf(
			"http: %q glob must contain a <%s> token (or use a /regex/)",
			keyExtract, pattern.TokenVersion,
		)
	}
	return pat, nil
}

// compileJQ compiles a jq program through the shared helper, so a bad expression
// is reported at validation time in the http provider's terms.
func compileJQ(expr string) (*gojq.Code, error) {
	code, err := jq.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("http: %q: %w", keyJQ, err)
	}
	return code, nil
}

// extractKind is how Discover turns the response body into version strings.
type extractKind int

const (
	extractJQ      extractKind = iota // run a jq program over a JSON body
	extractPattern                    // match a glob/regex over a text body
)

// resource is a validated http descriptor: the endpoint to fetch, the User-Agent
// to send, and the single compiled extractor that pulls version candidate(s) from
// its response.
type resource struct {
	url       string
	userAgent string
	kind      extractKind
	jq        *gojq.Code       // extractJQ
	pattern   *pattern.Pattern // extractPattern
}
