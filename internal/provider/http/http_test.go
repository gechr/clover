package http_test

import (
	"io"
	nethttp "net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/http"
	"github.com/stretchr/testify/require"
)

const testURL = "https://example.com/releases"

func directiveOf(pairs ...directive.KV) directive.Directive {
	return directive.Directive{Pairs: pairs}
}

func kv(key, value string) directive.KV { return directive.KV{Key: key, Value: value} }

// roundTripFunc adapts a function to an http.RoundTripper.
type roundTripFunc func(*nethttp.Request) (*nethttp.Response, error)

func (f roundTripFunc) RoundTrip(req *nethttp.Request) (*nethttp.Response, error) { return f(req) }

// response builds a 200 response carrying body.
func response(req *nethttp.Request, body string) *nethttp.Response {
	return &nethttp.Response{
		StatusCode: nethttp.StatusOK,
		Status:     "200 OK",
		Header:     nethttp.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

// newProvider returns a provider whose transport serves body for every request.
func newProvider(body string) *http.Provider {
	return http.New(http.WithTransport(
		roundTripFunc(func(req *nethttp.Request) (*nethttp.Response, error) {
			return response(req, body), nil
		}),
	))
}

// resourceFor builds a validated resource for the given directive pairs.
func resourceFor(t *testing.T, p *http.Provider, pairs ...directive.KV) provider.Resource {
	t.Helper()
	res, err := p.Resource(directiveOf(pairs...))
	require.NoError(t, err)
	return res
}

// versions extracts the version strings from candidates, in order.
func versions(candidates []model.Candidate) []string {
	out := make([]string, len(candidates))
	for i, c := range candidates {
		out[i] = c.Version
	}
	return out
}

func TestNameAndKeys(t *testing.T) {
	t.Parallel()

	p := http.New()
	require.Equal(t, "http", p.Name())

	keys := p.Keys()
	require.Len(t, keys, 4)
	require.Equal(t, "url", keys[0].Name)
	require.True(t, keys[0].Required)
	require.Equal(t, "jq", keys[1].Name)
	require.False(t, keys[1].Required)
	require.Equal(t, "extract", keys[2].Name)
	require.False(t, keys[2].Required)
	require.Equal(t, "user-agent", keys[3].Name)
	require.False(t, keys[3].Required)
}

func TestResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pairs   []directive.KV
		wantErr bool
	}{
		{
			name:  "valid jq",
			pairs: []directive.KV{kv("url", testURL), kv("jq", ".[].tag_name")},
		},
		{
			name:  "valid extract regex",
			pairs: []directive.KV{kv("url", testURL), kv("extract", `/v(\d+\.\d+\.\d+)/`)},
		},
		{
			name:  "valid extract version glob",
			pairs: []directive.KV{kv("url", testURL), kv("extract", "app-<version>.tar.gz")},
		},
		{
			name: "user-agent override",
			pairs: []directive.KV{
				kv("url", testURL),
				kv("jq", ".version"),
				kv("user-agent", "me/1.0"),
			},
		},
		{
			name:    "missing url",
			pairs:   []directive.KV{kv("jq", ".version")},
			wantErr: true,
		},
		{
			name:    "empty extract",
			pairs:   []directive.KV{kv("url", testURL), kv("extract", "")},
			wantErr: true,
		},
		{
			name:    "tokenless extract glob",
			pairs:   []directive.KV{kv("url", testURL), kv("extract", "latest.txt")},
			wantErr: true,
		},
		{
			name: "component-token extract glob",
			pairs: []directive.KV{
				kv("url", testURL),
				kv("extract", "app-v<major>.<minor>.<patch>.tar.gz"),
			},
			wantErr: true,
		},
		{
			name: "url with credentials",
			pairs: []directive.KV{
				kv("url", "https://user:pass@example.com/x"),
				kv("jq", ".version"),
			},
			wantErr: true,
		},
		{
			name:    "url with empty hostname",
			pairs:   []directive.KV{kv("url", "https://:443/path"), kv("jq", ".version")},
			wantErr: true,
		},
		{
			name:    "no extraction key",
			pairs:   []directive.KV{kv("url", testURL)},
			wantErr: true,
		},
		{
			name: "both extraction keys",
			pairs: []directive.KV{
				kv("url", testURL),
				kv("jq", ".version"),
				kv("extract", "/(.*)/"),
			},
			wantErr: true,
		},
		{
			name:    "malformed jq",
			pairs:   []directive.KV{kv("url", testURL), kv("jq", ".[")},
			wantErr: true,
		},
		{
			name:    "malformed extract regex",
			pairs:   []directive.KV{kv("url", testURL), kv("extract", "/[/")},
			wantErr: true,
		},
		{
			name:    "non-http url",
			pairs:   []directive.KV{kv("url", "ftp://example.com/x"), kv("jq", ".version")},
			wantErr: true,
		},
		{
			name:    "url without host",
			pairs:   []directive.KV{kv("url", "https:///x"), kv("jq", ".version")},
			wantErr: true,
		},
	}

	p := http.New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := p.Resource(directiveOf(tt.pairs...))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestDescribe(t *testing.T) {
	t.Parallel()

	p := http.New()
	res := resourceFor(t, p, kv("url", testURL), kv("jq", ".version"))
	require.Equal(t, "example.com", p.Describe(res))
	require.Equal(t, "http", p.Describe("not-a-resource"))
}

func TestDiscoverJQStream(t *testing.T) {
	t.Parallel()

	const body = `[{"tag_name": "v1.2.0"}, {"tag_name": "v1.3.0"}]`
	p := newProvider(body)
	candidates, err := p.Discover(
		t.Context(),
		resourceFor(t, p, kv("url", testURL), kv("jq", ".[].tag_name")),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"v1.2.0", "v1.3.0"}, versions(candidates))
}

func TestDiscoverJQSingleValue(t *testing.T) {
	t.Parallel()

	const body = `{"version": "1.4.2"}`
	p := newProvider(body)
	candidates, err := p.Discover(
		t.Context(),
		resourceFor(t, p, kv("url", testURL), kv("jq", ".version")),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"1.4.2"}, versions(candidates))
}

func TestDiscoverJQArrayResult(t *testing.T) {
	t.Parallel()

	const body = `{"versions": ["1.0.0", "2.0.0"]}`
	p := newProvider(body)
	candidates, err := p.Discover(
		t.Context(),
		resourceFor(t, p, kv("url", testURL), kv("jq", ".versions")),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"1.0.0", "2.0.0"}, versions(candidates))
}

// TestDiscoverJQSkipsNonStrings covers a result stream mixing strings with other
// types: only the strings become candidates.
func TestDiscoverJQSkipsNonStrings(t *testing.T) {
	t.Parallel()

	const body = `[{"tag_name": "v1.0.0"}, {"tag_name": 123}, {"tag_name": "v1.1.0"}]`
	p := newProvider(body)
	candidates, err := p.Discover(
		t.Context(),
		resourceFor(t, p, kv("url", testURL), kv("jq", ".[].tag_name")),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"v1.0.0", "v1.1.0"}, versions(candidates))
}

// TestDiscoverDedupes covers a response that repeats a version: it surfaces once,
// in first-seen order.
func TestDiscoverDedupes(t *testing.T) {
	t.Parallel()

	const body = `[{"t": "v1.0.0"}, {"t": "v1.0.0"}, {"t": "v2.0.0"}]`
	p := newProvider(body)
	candidates, err := p.Discover(
		t.Context(),
		resourceFor(t, p, kv("url", testURL), kv("jq", ".[].t")),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"v1.0.0", "v2.0.0"}, versions(candidates))
}

func TestDiscoverExtractRegex(t *testing.T) {
	t.Parallel()

	const body = "current release: v1.4.2 (stable)\n"
	p := newProvider(body)
	candidates, err := p.Discover(
		t.Context(),
		resourceFor(t, p, kv("url", testURL), kv("extract", `/v(\d+\.\d+\.\d+)/`)),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"1.4.2"}, versions(candidates))
}

// TestDiscoverExtractRegexNoGroup covers a /regex/ with no capture group: the
// whole match becomes the version.
func TestDiscoverExtractRegexNoGroup(t *testing.T) {
	t.Parallel()

	const body = "release 1.4.2"
	p := newProvider(body)
	candidates, err := p.Discover(
		t.Context(),
		resourceFor(t, p, kv("url", testURL), kv("extract", `/\d+\.\d+\.\d+/`)),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"1.4.2"}, versions(candidates))
}

// TestDiscoverExtractGlobToken covers the glob dialect: a <version> token in the
// pattern captures each matching run across the body.
func TestDiscoverExtractGlobToken(t *testing.T) {
	t.Parallel()

	const body = "node-v20.1.0-linux-x64.tar.xz\nnode-v20.2.0-linux-x64.tar.xz\n"
	p := newProvider(body)
	candidates, err := p.Discover(
		t.Context(),
		resourceFor(t, p, kv("url", testURL), kv("extract", "node-v<version>-linux-x64.tar.xz")),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"20.1.0", "20.2.0"}, versions(candidates))
}

func TestDiscoverInvalidJSON(t *testing.T) {
	t.Parallel()

	p := newProvider("not json at all")
	_, err := p.Discover(t.Context(), resourceFor(t, p, kv("url", testURL), kv("jq", ".version")))
	require.Error(t, err)
}

func TestDiscoverHTTPError(t *testing.T) {
	t.Parallel()

	p := http.New(http.WithTransport(
		roundTripFunc(func(req *nethttp.Request) (*nethttp.Response, error) {
			return &nethttp.Response{
				StatusCode: nethttp.StatusNotFound,
				Status:     "404 Not Found",
				Header:     nethttp.Header{},
				Body:       io.NopCloser(strings.NewReader("not found")),
				Request:    req,
			}, nil
		}),
	))
	_, err := p.Discover(t.Context(), resourceFor(t, p, kv("url", testURL), kv("jq", ".version")))
	require.Error(t, err)
}

func TestDiscoverInvalidResource(t *testing.T) {
	t.Parallel()

	_, err := http.New().Discover(t.Context(), "not-a-resource")
	require.Error(t, err)
}

// TestDiscoverJQDropsNonStrings locks the silent-drop policy: a numeric array
// yields no candidates and no error, rather than coercing numbers to versions.
func TestDiscoverJQDropsNonStrings(t *testing.T) {
	t.Parallel()

	p := newProvider("[1, 2, 3]")
	candidates, err := p.Discover(
		t.Context(),
		resourceFor(t, p, kv("url", testURL), kv("jq", ".[]")),
	)
	require.NoError(t, err)
	require.Empty(t, candidates)
}

// TestDiscoverJQRuntimeError covers a program that compiles but errors at run
// time: iterating a JSON number surfaces the jq error.
func TestDiscoverJQRuntimeError(t *testing.T) {
	t.Parallel()

	p := newProvider("5")
	_, err := p.Discover(t.Context(), resourceFor(t, p, kv("url", testURL), kv("jq", ".[]")))
	require.Error(t, err)
}

// TestDiscoverExtractNoMatch covers a body the pattern never matches: no
// candidates, no error.
func TestDiscoverExtractNoMatch(t *testing.T) {
	t.Parallel()

	p := newProvider("no version on this page\n")
	candidates, err := p.Discover(
		t.Context(),
		resourceFor(t, p, kv("url", testURL), kv("extract", `/v(\d+\.\d+\.\d+)/`)),
	)
	require.NoError(t, err)
	require.Empty(t, candidates)
}

// TestDiscoverExceedsSizeLimit covers a body one byte past the cap: it errors
// rather than parsing a truncated prefix.
func TestDiscoverExceedsSizeLimit(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("a", (16<<20)+1) // one past maxSize
	p := newProvider(body)
	_, err := p.Discover(t.Context(), resourceFor(t, p, kv("url", testURL), kv("extract", "/a/")))
	require.Error(t, err)
}

// TestUserAgent covers the request's User-Agent: the versioned default, the bare
// product name when the version is unknown, and a per-marker override.
func TestUserAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []http.Option
		ua   string
		want string
	}{
		{name: "default unknown version", want: "Clover"},
		{
			name: "default with version",
			opts: []http.Option{http.WithVersion("1.2.3")},
			want: "Clover v1.2.3",
		},
		{name: "marker override", ua: "me/1.0", want: "me/1.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got string
			opts := append([]http.Option{http.WithTransport(
				roundTripFunc(func(req *nethttp.Request) (*nethttp.Response, error) {
					got = req.Header.Get("User-Agent")
					return response(req, "[]"), nil
				}),
			)}, tt.opts...)

			p := http.New(opts...)
			pairs := []directive.KV{kv("url", testURL), kv("jq", ".[]")}
			if tt.ua != "" {
				pairs = append(pairs, kv("user-agent", tt.ua))
			}
			_, err := p.Discover(t.Context(), resourceFor(t, p, pairs...))
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestNotRecencyOrderer locks the leaner design: a single fetch returns whatever
// the endpoint serves, so nothing is truncated and the provider does not claim
// the recency-ordered capability that only routes a truncation signal.
func TestNotRecencyOrderer(t *testing.T) {
	t.Parallel()

	_, ok := any(http.New()).(provider.RecencyOrderer)
	require.False(t, ok)
}
