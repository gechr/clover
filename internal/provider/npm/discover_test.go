package npm_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/npm"
	xslices "github.com/gechr/x/slices"
	"github.com/stretchr/testify/require"
)

func jsonResponse(req *http.Request, body string) *http.Response {
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

// resourceFor builds a validated resource for the given directive pairs.
func resourceFor(t *testing.T, p *npm.Provider, pairs ...directive.KV) provider.Resource {
	t.Helper()
	res, err := p.Resource(directiveOf(pairs...))
	require.NoError(t, err)
	return res
}

// newProvider returns a provider whose transport serves the given packument
// body for every request.
func newProvider(body string) *npm.Provider {
	return npm.New(npm.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, body), nil
		}),
	))
}

// versions extracts the version strings from candidates, sorted: the packument
// keys its versions map in publish order and Go randomizes map iteration, so
// tests compare the set, never the order.
func versions(candidates []model.Candidate) []string {
	vs := xslices.Map(candidates, func(c model.Candidate) string { return c.Version })
	xslices.SortNatural(vs)
	return vs
}

// TestDiscoverSkipsBlankVersions covers a packument entry keyed by an empty
// version: it is dropped rather than surfaced as an empty candidate.
func TestDiscoverSkipsBlankVersions(t *testing.T) {
	t.Parallel()

	const body = `{
		"versions": {"": {}, "1.3.0": {}},
		"time": {"1.3.0": "2018-04-09T01:10:45.796Z"}
	}`

	p := newProvider(body)
	candidates, err := p.Discover(
		t.Context(),
		resourceFor(t, p, directive.KV{Key: "package", Value: "left-pad"}),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"1.3.0"}, versions(candidates))
}

// TestDiscoverUndatedVersion covers a version absent from the time map: its
// PublishedAt stays zero, so cooldown goes inert rather than trusting a wrong
// date.
func TestDiscoverUndatedVersion(t *testing.T) {
	t.Parallel()

	const body = `{
		"versions": {"1.3.0": {}},
		"time": {"created": "2014-03-14T09:09:20.762Z"}
	}`

	p := newProvider(body)
	candidates, err := p.Discover(
		t.Context(),
		resourceFor(t, p, directive.KV{Key: "package", Value: "left-pad"}),
	)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.True(t, candidates[0].PublishedAt.IsZero())
	require.Empty(t, candidates[0].Assets)
}

// TestDiscoverEscapesScopedPackagePath covers the packument URL for a scoped
// package: the whole name is path-escaped, yielding the @scope%2Fname form the
// registry documents.
func TestDiscoverEscapesScopedPackagePath(t *testing.T) {
	t.Parallel()

	var requested string
	p := npm.New(npm.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requested = req.URL.String()
			return jsonResponse(req, `{"versions": {}, "time": {}}`), nil
		}),
	))

	_, err := p.Discover(
		t.Context(),
		resourceFor(t, p, directive.KV{Key: "package", Value: "@vue/reactivity"}),
	)
	require.NoError(t, err)
	require.Equal(t, "https://registry.npmjs.org/@vue%2Freactivity", requested)
}

// TestDiscoverCustomRegistry covers the registry key: the packument URL is built
// on the custom base (trailing slash trimmed), with the same scoped-name
// escaping as the public registry.
func TestDiscoverCustomRegistry(t *testing.T) {
	t.Parallel()

	var requested string
	p := npm.New(npm.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requested = req.URL.String()
			return jsonResponse(req, `{"versions": {}, "time": {}}`), nil
		}),
	))

	_, err := p.Discover(
		t.Context(),
		resourceFor(t, p,
			directive.KV{Key: "package", Value: "@vue/reactivity"},
			directive.KV{Key: "registry", Value: "https://npm.internal.corp/"},
		),
	)
	require.NoError(t, err)
	require.Equal(t, "https://npm.internal.corp/@vue%2Freactivity", requested)
}

// TestDiscoverSkipsDeprecated covers the deprecated gate on a mixed packument:
// a version carrying a deprecation message is dropped by default and restored
// by deprecated=true, while the empty-string and false shapes some registries
// emit after un-deprecation stay active.
func TestDiscoverSkipsDeprecated(t *testing.T) {
	t.Parallel()

	const body = `{
		"versions": {
			"1.0.0": {"deprecated": "use 2.x"},
			"2.0.0": {},
			"3.0.0": {"deprecated": ""},
			"4.0.0": {"deprecated": false}
		},
		"time": {}
	}`

	p := newProvider(body)
	got, err := p.Discover(
		t.Context(),
		resourceFor(t, p, directive.KV{Key: "package", Value: "left-pad"}),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"2.0.0", "3.0.0", "4.0.0"}, versions(got))

	all, err := p.Discover(
		t.Context(),
		resourceFor(t, p,
			directive.KV{Key: "package", Value: "left-pad"},
			directive.KV{Key: "deprecated", Value: "true"},
		),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"1.0.0", "2.0.0", "3.0.0", "4.0.0"}, versions(all))
}

// TestDiscoverDistTagDeprecated covers a dist-tag naming a deprecated version:
// the pointer yields no candidates by default and deprecated=true restores it.
func TestDiscoverDistTagDeprecated(t *testing.T) {
	t.Parallel()

	const body = `{
		"dist-tags": {"latest": "1.0.0"},
		"versions": {"1.0.0": {"deprecated": "abandoned"}},
		"time": {}
	}`

	p := newProvider(body)
	got, err := p.Discover(
		t.Context(),
		resourceFor(t, p,
			directive.KV{Key: "package", Value: "left-pad"},
			directive.KV{Key: "dist-tag", Value: "latest"},
		),
	)
	require.NoError(t, err)
	require.Empty(t, got)

	kept, err := p.Discover(
		t.Context(),
		resourceFor(t, p,
			directive.KV{Key: "package", Value: "left-pad"},
			directive.KV{Key: "dist-tag", Value: "latest"},
			directive.KV{Key: "deprecated", Value: "true"},
		),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"1.0.0"}, versions(kept))
}

func TestDiscoverError(t *testing.T) {
	t.Parallel()

	p := npm.New(npm.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Body:       io.NopCloser(strings.NewReader(`{"error":"Not found"}`)),
				Request:    req,
			}, nil
		}),
	))

	_, err := p.Discover(
		t.Context(),
		resourceFor(t, p, directive.KV{Key: "package", Value: "no-such-package"}),
	)
	require.EqualError(t,
		err,
		`npm: get package no-such-package: {"error":"Not found"} (404 Not Found)`,
	)
}

func TestDiscoverInvalidResource(t *testing.T) {
	t.Parallel()

	_, err := npm.New().Discover(t.Context(), "not-a-resource")
	require.EqualError(t, err, "npm: invalid resource string")
}
