package hashicorp_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/hashicorp"
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
func resourceFor(t *testing.T, p *hashicorp.Provider, pairs ...directive.KV) provider.Resource {
	t.Helper()
	res, err := p.Resource(directiveOf(pairs...))
	require.NoError(t, err)
	return res
}

func TestDiscover(t *testing.T) {
	t.Parallel()

	const body = `[
		{"version": "1.10.0", "is_prerelease": false, "license_class": "oss", "timestamp_created": "2025-02-01T10:00:00Z"},
		{"version": "1.10.0-rc1", "is_prerelease": true, "license_class": "oss", "timestamp_created": "2025-01-20T10:00:00Z"},
		{"version": "1.9.8", "is_prerelease": false, "license_class": "oss", "timestamp_created": "2025-01-10T10:00:00Z"}
	]`

	p := hashicorp.New(hashicorp.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, body), nil
		}),
	))

	candidates, err := p.Discover(
		t.Context(),
		resourceFor(t, p, directive.KV{Key: "product", Value: "terraform"}),
	)
	require.NoError(t, err)
	require.Len(t, candidates, 3)

	require.Equal(t, "1.10.0", candidates[0].Version)
	require.NotNil(t, candidates[0].Semver)
	require.False(t, candidates[0].Prerelease)
	require.Equal(t, "1.10.0", candidates[0].Ref)
	require.Equal(t, time.Date(2025, 2, 1, 10, 0, 0, 0, time.UTC), candidates[0].PublishedAt)

	// A prerelease surfaces with the flag set; the framework decides whether to
	// exclude it, the provider does not drop it.
	require.Equal(t, "1.10.0-rc1", candidates[1].Version)
	require.True(t, candidates[1].Prerelease)
}

func TestDiscoverEdition(t *testing.T) {
	t.Parallel()

	const body = `[
		{"version": "2.0.3+ent.hsm.fips1403", "license_class": "enterprise", "timestamp_created": "2025-02-01T13:00:00Z"},
		{"version": "2.0.3+ent.hsm", "license_class": "enterprise", "timestamp_created": "2025-02-01T12:00:00Z"},
		{"version": "2.0.3+ent.fips1403", "license_class": "enterprise", "timestamp_created": "2025-02-01T11:00:00Z"},
		{"version": "2.0.3+ent", "license_class": "enterprise", "timestamp_created": "2025-02-01T10:00:00Z"},
		{"version": "2.0.3", "license_class": "oss", "timestamp_created": "2025-02-01T09:00:00Z"},
		{"version": "2.0.2", "license_class": "", "timestamp_created": "2025-01-15T10:00:00Z"}
	]`

	newProvider := func() *hashicorp.Provider {
		return hashicorp.New(hashicorp.WithTransport(
			roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return jsonResponse(req, body), nil
			}),
		))
	}
	discover := func(t *testing.T, pairs ...directive.KV) []model.Candidate {
		t.Helper()
		p := newProvider()
		c, err := p.Discover(t.Context(), resourceFor(t, p, pairs...))
		require.NoError(t, err)
		return c
	}

	t.Run("oss is the default; an empty class counts as oss", func(t *testing.T) {
		t.Parallel()
		got := discover(t, directive.KV{Key: "product", Value: "vault"})
		require.Equal(t, []string{"2.0.3", "2.0.2"}, versions(got))
	})

	t.Run("enterprise renders bare semver, collapsing flavors", func(t *testing.T) {
		t.Parallel()
		got := discover(t,
			directive.KV{Key: "product", Value: "vault"},
			directive.KV{Key: "enterprise", Value: "true"},
		)
		require.Equal(t, []string{"2.0.3"}, versions(got))
		require.NotNil(t, got[0].Semver)
	})

	t.Run("build selects an exact flavor and renders the full suffix", func(t *testing.T) {
		t.Parallel()
		got := discover(t,
			directive.KV{Key: "product", Value: "vault"},
			directive.KV{Key: "build", Value: "ent.hsm.fips1403"},
		)
		require.Equal(t, []string{"2.0.3+ent.hsm.fips1403"}, versions(got))
		require.NotNil(t, got[0].Semver)
	})

	t.Run("build=ent matches only the plain enterprise flavor", func(t *testing.T) {
		t.Parallel()
		got := discover(t,
			directive.KV{Key: "product", Value: "vault"},
			directive.KV{Key: "build", Value: "ent"},
		)
		require.Equal(t, []string{"2.0.3+ent"}, versions(got))
	})

	t.Run("unknown build yields no candidates", func(t *testing.T) {
		t.Parallel()
		got := discover(t,
			directive.KV{Key: "product", Value: "vault"},
			directive.KV{Key: "build", Value: "ent.nope"},
		)
		require.Empty(t, got)
	})
}

func TestDiscoverDeepPaginates(t *testing.T) {
	t.Parallel()

	var afters []string
	p := hashicorp.New(hashicorp.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			after := req.URL.Query().Get("after")
			afters = append(afters, after)
			if after == "" {
				return jsonResponse(req, fullPage(0)), nil // 20 items -> more follow
			}
			return jsonResponse(req, shortPage()), nil // 2 items -> end
		}),
	))

	res := resourceFor(t, p, directive.KV{Key: "product", Value: "consul"})
	candidates, err := p.Discover(provider.WithDeep(t.Context(), true), res)
	require.NoError(t, err)
	require.Len(t, candidates, 22)
	// First request carries no cursor; the second pages from the last item's
	// timestamp on page one.
	require.Equal(t, []string{"", "2025-01-01T00:00:19Z"}, afters)
}

func TestDiscoverShallowReadsOnePage(t *testing.T) {
	t.Parallel()

	// The listing is newest-first, so a shallow lookup reads only the first page
	// and never paginates - the latest release is always on it.
	var requests int
	p := hashicorp.New(hashicorp.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			require.Empty(t, req.URL.Query().Get("after"))
			return jsonResponse(req, fullPage(0)), nil
		}),
	))

	res := resourceFor(t, p, directive.KV{Key: "product", Value: "nomad"})
	candidates, err := p.Discover(t.Context(), res)
	require.NoError(t, err)
	require.Equal(t, 1, requests)
	require.Len(t, candidates, 20)
}

// TestDiscoverShallowReportsNoTruncation locks the decision that a recency-ordered
// source emits no blanket --deep hint: even when the first page is full (more
// releases exist), the newest is always on it, so a shallow lookup must not feed
// the truncation sink. The --deep signal for a constraint pinned to an older
// stream comes instead from a no-candidate failure.
func TestDiscoverShallowReportsNoTruncation(t *testing.T) {
	t.Parallel()

	p := hashicorp.New(hashicorp.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, fullPage(0)), nil
		}),
	))

	var truncations int
	ctx := provider.WithTruncationSink(t.Context(), func(provider.Truncation) {
		truncations++
	})

	res := resourceFor(t, p, directive.KV{Key: "product", Value: "vault"})
	_, err := p.Discover(ctx, res)
	require.NoError(t, err)
	require.Zero(t, truncations)
}

func TestDiscoverError(t *testing.T) {
	t.Parallel()

	p := hashicorp.New(hashicorp.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Body:       io.NopCloser(strings.NewReader("not found")),
				Request:    req,
			}, nil
		}),
	))

	res := resourceFor(t, p, directive.KV{Key: "product", Value: "nope"})
	_, err := p.Discover(t.Context(), res)
	require.Error(t, err)
}

func TestURL(t *testing.T) {
	t.Parallel()

	p := hashicorp.New()
	res := resourceFor(t, p, directive.KV{Key: "product", Value: "terraform"})

	require.Equal(t,
		"https://releases.hashicorp.com/terraform/1.9.8",
		p.URL(res, model.Candidate{Version: "1.9.8"}),
	)
	require.Empty(t, p.URL(res, model.Candidate{}))
	require.Empty(t, p.URL("not-a-resource", model.Candidate{Version: "1.9.8"}))
}

// versions extracts the version strings from candidates, in order.
func versions(candidates []model.Candidate) []string {
	out := make([]string, len(candidates))
	for i, c := range candidates {
		out[i] = c.Version
	}
	return out
}

// fullPage renders a 20-item page (pageLimit) of synthetic releases, newest
// first, with timestamps descending from the given second offset so the last
// item's cursor is deterministic.
func fullPage(base int) string {
	items := make([]string, 20)
	for i := range items {
		items[i] = fmt.Sprintf(
			`{"version": "1.0.%d", "license_class": "oss", "timestamp_created": "2025-01-01T00:00:%02dZ"}`,
			base+i,
			base+i,
		)
	}
	return "[" + strings.Join(items, ",") + "]"
}

// shortPage renders a 2-item page, signalling the end of the archive.
func shortPage() string {
	return `[
		{"version": "0.9.1", "license_class": "oss", "timestamp_created": "2024-12-01T00:00:00Z"},
		{"version": "0.9.0", "license_class": "oss", "timestamp_created": "2024-11-01T00:00:00Z"}
	]`
}
