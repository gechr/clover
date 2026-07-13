package swift_test

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
	"github.com/gechr/clover/internal/provider/swift"
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
func resourceFor(t *testing.T, p *swift.Provider) provider.Resource {
	t.Helper()
	res, err := p.Resource(directiveOf())
	require.NoError(t, err)
	return res
}

// newProvider returns a provider whose transport serves the given index body.
func newProvider(body string) *swift.Provider {
	return swift.New(swift.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, body), nil
		}),
	))
}

// versions extracts the candidate version strings in discovery order, which
// follows the index's own order.
func versions(candidates []model.Candidate) []string {
	return xslices.Map(candidates, func(c model.Candidate) string {
		return c.Version
	})
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

// attrs adapts a candidate for version selection, mirroring the pipeline.
func attrs(c model.Candidate) version.Attrs {
	return version.Attrs{
		Tag:         c.Version,
		Semver:      c.Semver,
		Prerelease:  c.Prerelease,
		PublishedAt: c.PublishedAt,
	}
}

const twoReleases = `[
	{"name": "6.3.2", "tag": "swift-6.3.2-RELEASE", "xcode": "Xcode 26.5", "xcode_release": true, "date": "2026-05-11", "platforms": [{"name": "Ubuntu 24.04", "platform": "Linux", "docker": "6.3.2-noble", "archs": ["x86_64", "aarch64"]}]},
	{"name": "6.3.3", "tag": "swift-6.3.3-RELEASE", "xcode": "Xcode 26.6", "xcode_release": true, "date": "2026-06-29", "platforms": [{"name": "Static SDK", "platform": "static-sdk", "version": "0.1.0", "checksum": "87c3eaf908e67c0e13a84367119e12273cec1d2cd3d81f7d74bb36722d6b607b", "archs": ["x86_64", "arm64"]}]}
]`

// TestDiscoverVersionAndRef covers the core transform: the bare name is the
// version and the release tag is the upstream ref.
func TestDiscoverVersionAndRef(t *testing.T) {
	t.Parallel()

	p := newProvider(twoReleases)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"6.3.2", "6.3.3"}, versions(got))

	c := candidateFor(t, got, "6.3.3")
	require.Equal(t, "swift-6.3.3-RELEASE", c.Ref)
	require.NotNil(t, c.Semver)
	require.Equal(t, "6.3.3", c.Semver.String())
}

// TestDiscoverTwoComponentName covers the older naming scheme: a two-component
// name (5.10) still parses, so it orders like any other release.
func TestDiscoverTwoComponentName(t *testing.T) {
	t.Parallel()

	const body = `[{"name": "5.10", "tag": "swift-5.10-RELEASE", "xcode": "Xcode 15.3", "xcode_release": true, "date": "2024-03-05", "platforms": []}]`
	p := newProvider(body)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	c := candidateFor(t, got, "5.10")
	require.Equal(t, "swift-5.10-RELEASE", c.Ref)
	require.NotNil(t, c.Semver)
	require.Equal(t, "5.10.0", c.Semver.String())
}

// TestDiscoverPublishedAt covers cooldown's input: the date-only value decodes to
// the last second of that UTC day so a release is not treated as old too early.
func TestDiscoverPublishedAt(t *testing.T) {
	t.Parallel()

	p := newProvider(twoReleases)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	c := candidateFor(t, got, "6.3.3")
	require.Equal(t, time.Date(2026, 6, 29, 23, 59, 59, 0, time.UTC), c.PublishedAt)
}

// TestDiscoverSkipsBlankName covers the defensive guard: an entry with no name
// carries no version to track, so it is dropped rather than erroring the fetch.
func TestDiscoverSkipsBlankName(t *testing.T) {
	t.Parallel()

	const body = `[
		{"name": "", "tag": "swift-broken", "date": "2026-06-29", "platforms": []},
		{"name": "6.3.3", "tag": "swift-6.3.3-RELEASE", "date": "2026-06-29", "platforms": []}
	]`
	p := newProvider(body)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"6.3.3"}, versions(got))
}

// TestDiscoverMissingTag covers the ref fallback: an entry without a tag keeps
// the bare name as its ref so the Linker still resolves.
func TestDiscoverMissingTag(t *testing.T) {
	t.Parallel()

	const body = `[{"name": "6.3.3", "date": "2026-06-29", "platforms": []}]`
	p := newProvider(body)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, "6.3.3", candidateFor(t, got, "6.3.3").Ref)
}

// TestDiscoverAssets covers asset construction: only the SDK platform entries
// carry a checksum, each becoming an asset named by its stable platform key with
// the inline sha256 digest; the toolchain entries yield none.
func TestDiscoverAssets(t *testing.T) {
	t.Parallel()

	p := newProvider(twoReleases)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	require.Empty(t, candidateFor(t, got, "6.3.2").Assets)
	require.Equal(t, []model.Asset{{
		Name:   "static-sdk",
		Digest: "sha256:87c3eaf908e67c0e13a84367119e12273cec1d2cd3d81f7d74bb36722d6b607b",
	}}, candidateFor(t, got, "6.3.3").Assets)
}

// TestFreeDigestResolves confirms a follower can source a sha256 from the asset
// digest with no download, selecting the artifact by its stable platform key -
// the point of populating assets at discovery.
func TestFreeDigestResolves(t *testing.T) {
	t.Parallel()

	p := newProvider(twoReleases)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	sum, err := checksum.Resolve(t.Context(), nil, checksum.Request{
		Source:  constant.Sha256Digest,
		Assets:  candidateFor(t, got, "6.3.3").Assets,
		Pattern: "static-sdk",
	})
	require.NoError(t, err)
	require.Equal(t, "87c3eaf908e67c0e13a84367119e12273cec1d2cd3d81f7d74bb36722d6b607b", sum)
}

// TestSelectionCooldown covers date-driven cooldown: with a cooldown in force the
// too-fresh newest release is held back and the newest older one is selected.
func TestSelectionCooldown(t *testing.T) {
	t.Parallel()

	p := newProvider(twoReleases)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	current, err := version.Parse("6.3.1")
	require.NoError(t, err)

	// 6.3.3 published 2026-06-29; a now 2 days later with a 30-day cooldown makes
	// it too fresh, so 6.3.2 is the newest eligible upgrade.
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	chosen, ok := version.Select(current, got, attrs,
		version.WithCooldown(30*24*time.Hour), version.WithNow(now))
	require.True(t, ok)
	require.Equal(t, "6.3.2", chosen.Version)
}

func TestDiscoverErrorStatus(t *testing.T) {
	t.Parallel()

	p := swift.New(swift.WithTransport(
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
	require.EqualError(t, err, "swift: list releases: not found (404 Not Found)")
}

func TestDiscoverDecodeError(t *testing.T) {
	t.Parallel()

	p := newProvider("not json")
	_, err := p.Discover(t.Context(), resourceFor(t, p))
	require.Error(t, err)
}

func TestDiscoverInvalidResource(t *testing.T) {
	t.Parallel()

	_, err := swift.New().Discover(t.Context(), "not-a-resource")
	require.EqualError(t, err, "swift: invalid resource string")
}
