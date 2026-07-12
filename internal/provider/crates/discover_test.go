package crates_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/crates"
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
func resourceFor(t *testing.T, p *crates.Provider, pairs ...directive.KV) provider.Resource {
	t.Helper()
	res, err := p.Resource(directiveOf(pairs...))
	require.NoError(t, err)
	return res
}

// newProvider returns a provider whose transport serves the given listing body,
// built with the given options.
func newProvider(body string, opts ...crates.Option) *crates.Provider {
	opts = append(opts, crates.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, body), nil
		}),
	))
	return crates.New(opts...)
}

// versions extracts the version strings from candidates, in order.
func versions(candidates []model.Candidate) []string {
	return xslices.Map(candidates, func(c model.Candidate) string { return c.Version })
}

// discoverBody resolves candidates for the serde crate against a literal
// listing body.
func discoverBody(t *testing.T, body string) []model.Candidate {
	t.Helper()
	p := newProvider(body)
	candidates, err := p.Discover(
		t.Context(),
		resourceFor(t, p, directive.KV{Key: "package", Value: "serde"}),
	)
	require.NoError(t, err)
	return candidates
}

// TestDiscoverSkipsYankedVersions covers the liveness gate: a yanked version is
// withdrawn from the registry, so it never surfaces as a candidate.
func TestDiscoverSkipsYankedVersions(t *testing.T) {
	t.Parallel()

	const body = `{"versions": [
		{"num": "1.1.0", "created_at": "2026-02-01T00:00:00Z", "yanked": true, "checksum": "aa", "dl_path": "/api/v1/crates/serde/1.1.0/download"},
		{"num": "1.0.0", "created_at": "2026-01-01T00:00:00Z", "yanked": false, "checksum": "bb", "dl_path": "/api/v1/crates/serde/1.0.0/download"}
	]}`

	got := discoverBody(t, body)
	require.Equal(t, []string{"1.0.0"}, versions(got))
	require.Equal(t,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		got[0].PublishedAt,
	)
	require.Equal(t,
		[]model.Asset{{
			Name:   "serde-1.0.0.crate",
			Digest: "sha256:bb",
			URL:    "https://crates.io/api/v1/crates/serde/1.0.0/download",
		}},
		got[0].Assets,
	)
}

// TestDiscoverSkipsUnparseableVersions covers the semver gate: cargo enforces
// semver on publish, but an unparseable record is dropped rather than surfaced.
func TestDiscoverSkipsUnparseableVersions(t *testing.T) {
	t.Parallel()

	const body = `{"versions": [
		{"num": "not-a-version", "created_at": "2026-01-01T00:00:00Z", "yanked": false, "checksum": "aa", "dl_path": ""},
		{"num": "1.0.0-rc.1", "created_at": "2026-01-02T00:00:00Z", "yanked": false, "checksum": "bb", "dl_path": ""}
	]}`

	got := discoverBody(t, body)
	require.Equal(t, []string{"1.0.0-rc.1"}, versions(got))
	require.Equal(t, "1.0.0-rc.1", got[0].Ref)
	require.NotNil(t, got[0].Semver)
	require.Equal(t, "rc.1", got[0].Semver.Prerelease())
}

// TestDiscoverMissingChecksumLeavesAssetUndigested covers a version record
// without a checksum: the asset surfaces with an empty digest rather than a
// bare "sha256:" prefix, and an empty dl_path leaves the URL empty rather than
// pointing at the bare origin.
func TestDiscoverMissingChecksumLeavesAssetUndigested(t *testing.T) {
	t.Parallel()

	const body = `{"versions": [
		{"num": "1.0.0", "created_at": "2026-01-01T00:00:00Z", "yanked": false, "checksum": "", "dl_path": ""}
	]}`

	got := discoverBody(t, body)
	require.Equal(t, []model.Asset{{Name: "serde-1.0.0.crate"}}, got[0].Assets)
}

// TestDiscoverUserAgent locks the crawling-policy header: every request
// identifies clover, with the binary's version woven in when it is known.
func TestDiscoverUserAgent(t *testing.T) {
	t.Parallel()

	const body = `{"versions": []}`
	var agents []string
	record := crates.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			agents = append(agents, req.Header.Get("User-Agent"))
			return jsonResponse(req, body), nil
		}),
	)

	for _, p := range []*crates.Provider{
		crates.New(record),
		crates.New(record, crates.WithVersion("1.2.3")),
	} {
		_, err := p.Discover(
			t.Context(),
			resourceFor(t, p, directive.KV{Key: "package", Value: "serde"}),
		)
		require.NoError(t, err)
	}
	require.Equal(t, []string{
		"Clover (+https://github.com/gechr/clover)",
		"Clover v1.2.3 (+https://github.com/gechr/clover)",
	}, agents)
}

func TestDiscoverError(t *testing.T) {
	t.Parallel()

	p := crates.New(crates.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Body:       io.NopCloser(strings.NewReader("not found")),
				Request:    req,
			}, nil
		}),
	))

	_, err := p.Discover(
		t.Context(),
		resourceFor(t, p, directive.KV{Key: "package", Value: "no-such-crate"}),
	)
	require.Error(t, err)
}

func TestDiscoverInvalidResource(t *testing.T) {
	t.Parallel()

	_, err := crates.New().Discover(t.Context(), "not-a-resource")
	require.EqualError(t, err, "crates: invalid resource string")
}
