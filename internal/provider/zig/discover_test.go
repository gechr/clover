package zig_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gechr/clover/internal/checksum"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/zig"
	"github.com/gechr/clover/internal/version"
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

// resourceFor builds a validated resource.
func resourceFor(t *testing.T, p *zig.Provider) provider.Resource {
	t.Helper()
	res, err := p.Resource(directiveOf())
	require.NoError(t, err)
	return res
}

// newProvider returns a provider whose transport serves the given index body.
func newProvider(body string) *zig.Provider {
	return zig.New(zig.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, body), nil
		}),
	))
}

// sortedVersions extracts the candidate version strings and sorts them, since the
// index is a map and Discover returns candidates in nondeterministic order.
func sortedVersions(candidates []model.Candidate) []string {
	out := xslices.Map(candidates, func(c model.Candidate) string {
		return c.Version
	})
	xslices.SortNatural(out)
	return out
}

// candidateFor returns the discovered candidate for a version, failing if absent.
func candidateFor(t *testing.T, candidates []model.Candidate, want string) model.Candidate {
	t.Helper()
	for _, c := range candidates {
		if c.Version == want {
			return c
		}
	}
	require.FailNowf(t, "version not found", "no candidate for %q", want)
	return model.Candidate{}
}

// assetFor returns the asset with the given platform name, failing if absent.
func assetFor(t *testing.T, assets []model.Asset, name string) model.Asset {
	t.Helper()
	for _, a := range assets {
		if a.Name == name {
			return a
		}
	}
	require.FailNowf(t, "asset not found", "no asset named %q", name)
	return model.Asset{}
}

// attrs adapts a candidate for version selection, mirroring the pipeline.
func attrs(c model.Candidate) version.Attrs {
	return version.Attrs{
		Tag:         c.Version,
		Semver:      c.Semver,
		Prerelease:  c.Prerelease,
		PublishedAt: c.PublishedAt,
	}
}

const twoStable = `{
	"0.16.0": {"version": "0.16.0", "date": "2026-04-13", "docs": "https://ziglang.org/documentation/0.16.0/", "x86_64-linux": {"tarball": "https://ziglang.org/download/0.16.0/zig-x86_64-linux-0.16.0.tar.xz", "shasum": "aaaa3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393aaaa", "size": "55478392"}},
	"0.15.2": {"version": "0.15.2", "date": "2025-10-11", "x86_64-linux": {"tarball": "https://ziglang.org/download/0.15.2/zig-x86_64-linux-0.15.2.tar.xz", "shasum": "bbbb3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393bbbb", "size": "53733924"}}
}`

// TestDiscoverVersionFromKey covers the core transform: the version comes from
// the map key (clean semver), which is both Version and Ref.
func TestDiscoverVersionFromKey(t *testing.T) {
	t.Parallel()

	p := newProvider(twoStable)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"0.15.2", "0.16.0"}, sortedVersions(got))

	c := candidateFor(t, got, "0.16.0")
	require.Equal(t, "0.16.0", c.Ref)
	require.NotNil(t, c.Semver)
	require.Equal(t, "0.16.0", c.Semver.String())
}

// TestDiscoverSkipsMaster covers the nightly pointer: the master key is a moving
// -dev build, not a release, so it is dropped.
func TestDiscoverSkipsMaster(t *testing.T) {
	t.Parallel()

	const body = `{
		"master": {"version": "0.17.0-dev.1275+59a628c6d", "date": "2026-07-07", "x86_64-linux": {"tarball": "https://ziglang.org/builds/zig-x86_64-linux-0.17.0-dev.1275+59a628c6d.tar.xz", "shasum": "cccc", "size": "1"}},
		"0.16.0": {"version": "0.16.0", "date": "2026-04-13"}
	}`
	p := newProvider(body)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"0.16.0"}, sortedVersions(got))
}

// TestDiscoverPublishedAt covers cooldown's input: the date-only value decodes to
// the last second of that UTC day so a release is not treated as old too early.
func TestDiscoverPublishedAt(t *testing.T) {
	t.Parallel()

	p := newProvider(twoStable)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	c := candidateFor(t, got, "0.16.0")
	require.Equal(t, time.Date(2026, 4, 13, 23, 59, 59, 0, time.UTC), c.PublishedAt)
}

// TestDiscoverMissingDate covers an entry with no date field: it decodes to a
// zero time (cooldown inert for that entry) rather than erroring the whole fetch.
func TestDiscoverMissingDate(t *testing.T) {
	t.Parallel()

	const body = `{"0.16.0": {"version": "0.16.0"}}`
	p := newProvider(body)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.True(t, candidateFor(t, got, "0.16.0").PublishedAt.IsZero())
}

// TestDiscoverAssets covers asset construction: each platform sub-object becomes
// an asset named by the stable platform key (not the filename), carrying the
// inline sha256 digest and the tarball URL.
func TestDiscoverAssets(t *testing.T) {
	t.Parallel()

	p := newProvider(twoStable)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, model.Asset{
		Name:   "x86_64-linux",
		Digest: "sha256:aaaa3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393aaaa",
		URL:    "https://ziglang.org/download/0.16.0/zig-x86_64-linux-0.16.0.tar.xz",
	}, assetFor(t, candidateFor(t, got, "0.16.0").Assets, "x86_64-linux"))
}

// TestFreeDigestResolves confirms a follower can source a sha256 from the asset
// digest with no download, selecting the artifact by its stable platform key -
// the point of populating assets at discovery.
func TestFreeDigestResolves(t *testing.T) {
	t.Parallel()

	p := newProvider(twoStable)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	sum, err := checksum.Resolve(t.Context(), nil, checksum.Request{
		Source:  constant.Sha256Digest,
		Assets:  candidateFor(t, got, "0.16.0").Assets,
		Pattern: "x86_64-linux",
	})
	require.NoError(t, err)
	require.Equal(t, "aaaa3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393aaaa", sum)
}

// TestDiscoverOldFormat covers a legacy entry with no version field and the old
// os-arch filename: it is still discovered via its key, and the asset keeps the
// stable platform name while the URL carries the drifted filename.
func TestDiscoverOldFormat(t *testing.T) {
	t.Parallel()

	const body = `{
		"0.12.0": {"date": "2024-04-20", "notes": "https://ziglang.org/download/0.12.0/release-notes.html", "x86_64-linux": {"tarball": "https://ziglang.org/download/0.12.0/zig-linux-x86_64-0.12.0.tar.xz", "shasum": "c7ae866b8a76a568e2d5cfd31fe89cdb629bdd161fdd5018b29a4a0a17045cad", "size": "45480516"}}
	}`
	p := newProvider(body)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"0.12.0"}, sortedVersions(got))

	a := assetFor(t, candidateFor(t, got, "0.12.0").Assets, "x86_64-linux")
	require.Equal(t, "https://ziglang.org/download/0.12.0/zig-linux-x86_64-0.12.0.tar.xz", a.URL)
}

// TestSelectionCooldown covers date-driven cooldown: with a cooldown in force the
// too-fresh newest release is held back and the newest older one is selected.
func TestSelectionCooldown(t *testing.T) {
	t.Parallel()

	p := newProvider(twoStable)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	current, err := version.Parse("0.14.0")
	require.NoError(t, err)

	// 0.16.0 published 2026-04-13; a now 2 days later with a 30-day cooldown makes
	// it too fresh, so 0.15.2 (an older line) is the newest eligible upgrade.
	now := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	chosen, ok := version.Select(current, got, attrs,
		version.WithCooldown(30*24*time.Hour), version.WithNow(now))
	require.True(t, ok)
	require.Equal(t, "0.15.2", chosen.Version)
}

func TestDiscoverErrorStatus(t *testing.T) {
	t.Parallel()

	p := zig.New(zig.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Body:       io.NopCloser(strings.NewReader("not found")),
				Request:    req,
			}, nil
		}),
	))

	_, err := p.Discover(t.Context(), resourceFor(t, p))
	require.EqualError(t, err, "zig: list releases: not found (404 Not Found)")
}

func TestDiscoverDecodeError(t *testing.T) {
	t.Parallel()

	p := newProvider("not json")
	_, err := p.Discover(t.Context(), resourceFor(t, p))
	require.Error(t, err)
}

func TestDiscoverInvalidResource(t *testing.T) {
	t.Parallel()

	_, err := zig.New().Discover(t.Context(), "not-a-resource")
	require.EqualError(t, err, "zig: invalid resource string")
}
