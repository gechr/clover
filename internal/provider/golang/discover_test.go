package golang_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/checksum"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/golang"
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
func resourceFor(t *testing.T, p *golang.Provider) provider.Resource {
	t.Helper()
	res, err := p.Resource(directiveOf())
	require.NoError(t, err)
	return res
}

// newProvider returns a provider whose transport serves the given index body.
func newProvider(body string) *golang.Provider {
	return golang.New(golang.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, body), nil
		}),
	))
}

// versions extracts the version strings from candidates, in order.
func versions(candidates []model.Candidate) []string {
	return xslices.Map(candidates, func(c model.Candidate) string { return c.Version })
}

// attrs adapts a candidate for version selection, mirroring the pipeline.
func attrs(c model.Candidate) version.Attrs {
	names := xslices.Map(c.Assets, func(a model.Asset) string { return a.Name })
	return version.Attrs{Tag: c.Version, Semver: c.Semver, Assets: names}
}

const twoStable = `[
	{"version": "go1.26.5", "stable": true, "files": [
		{"filename": "go1.26.5.linux-amd64.tar.gz", "os": "linux", "arch": "amd64", "kind": "archive", "sha256": "aaaa3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393aaaa"}
	]},
	{"version": "go1.26.4", "stable": true, "files": [
		{"filename": "go1.26.4.linux-amd64.tar.gz", "os": "linux", "arch": "amd64", "kind": "archive", "sha256": "bbbb3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393bbbb"}
	]}
]`

// TestDiscoverStripsGoPrefix covers the core transform: the "go" prefix is
// dropped so Version is clean semver and Semver parses, while Ref retains the
// prefixed upstream form for links.
func TestDiscoverStripsGoPrefix(t *testing.T) {
	t.Parallel()

	p := newProvider(twoStable)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"1.26.5", "1.26.4"}, versions(got))

	require.Equal(t, "go1.26.5", got[0].Ref)
	require.NotNil(t, got[0].Semver)
	require.Equal(t, "1.26.5", got[0].Semver.String())
}

// TestDiscoverNoPublishedDate documents that the download index carries no
// per-release date, so candidates leave PublishedAt zero. This is why cooldown
// is unsupported for the provider: the pipeline cannot measure a version's age.
func TestDiscoverNoPublishedDate(t *testing.T) {
	t.Parallel()

	p := newProvider(twoStable)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	for _, c := range got {
		require.True(t, c.PublishedAt.IsZero(), c.Version)
	}
}

// TestDiscoverAssets covers asset construction: every file with a checksum
// becomes an asset carrying the sha256 digest and derived download URL.
func TestDiscoverAssets(t *testing.T) {
	t.Parallel()

	p := newProvider(twoStable)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []model.Asset{{
		Name:   "go1.26.5.linux-amd64.tar.gz",
		Digest: "sha256:aaaa3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393aaaa",
		URL:    "https://go.dev/dl/go1.26.5.linux-amd64.tar.gz",
	}}, got[0].Assets)
}

// TestFreeDigestResolves confirms a follower can source a sha256 from the asset
// digest with no download - the point of populating assets at discovery.
func TestFreeDigestResolves(t *testing.T) {
	t.Parallel()

	p := newProvider(twoStable)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	sum, err := checksum.Resolve(t.Context(), nil, checksum.Request{
		Source:  constant.Sha256Digest,
		Assets:  got[0].Assets,
		Pattern: "go1.26.5.linux-amd64.tar.gz",
	})
	require.NoError(t, err)
	require.Equal(t, "aaaa3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393aaaa", sum)
}

// TestSelectionExcludesPrereleaseByDefault covers the dashless prerelease gate:
// the rc parses as a prerelease and is not selected unless prereleases are
// allowed.
func TestSelectionExcludesPrereleaseByDefault(t *testing.T) {
	t.Parallel()

	p := newProvider(withRC)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	current, err := version.Parse("1.26.4")
	require.NoError(t, err)

	chosen, ok := version.Select(current, got, attrs)
	require.True(t, ok)
	require.Equal(t, "1.26.5", chosen.Version)
}

// TestSelectionPrereleaseOptIn covers the opt-in: with prereleases allowed the
// rc becomes eligible and, being the highest version, is selected.
func TestSelectionPrereleaseOptIn(t *testing.T) {
	t.Parallel()

	p := newProvider(withRC)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	current, err := version.Parse("1.26.5")
	require.NoError(t, err)

	chosen, ok := version.Select(current, got, attrs, version.WithPrerelease(true))
	require.True(t, ok)
	// The dashless go.dev prerelease is normalised to canonical semver.
	require.Equal(t, "1.27.0-rc1", chosen.Version)
}

const withRC = `[
	{"version": "go1.27rc1", "stable": false, "files": []},
	{"version": "go1.26.5", "stable": true, "files": []},
	{"version": "go1.26.4", "stable": true, "files": []}
]`

// TestDiscoverSkipsUnstableStableShaped covers the stable-flag guard: an entry
// the index marks unstable whose version parses as stable semver would slip past
// the prerelease gate, so it is dropped. The dashless rc stays: its version
// parses as a prerelease, which the gate already handles.
func TestDiscoverSkipsUnstableStableShaped(t *testing.T) {
	t.Parallel()

	const body = `[
		{"version": "go1.28", "stable": false, "files": []},
		{"version": "go1.27rc1", "stable": false, "files": []},
		{"version": "go1.26.5", "stable": true, "files": []}
	]`

	p := newProvider(body)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"1.27.0-rc1", "1.26.5"}, versions(got))
}

// TestDiscoverNoChecksumNoAssets covers a release whose files all lack a
// checksum: no assets surface, rather than assets with empty digests.
func TestDiscoverNoChecksumNoAssets(t *testing.T) {
	t.Parallel()

	const body = `[
		{"version": "go1.26.5", "stable": true, "files": [
			{"filename": "go1.26.5.linux-amd64.tar.gz", "sha256": ""}
		]}
	]`

	p := newProvider(body)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Empty(t, got[0].Assets)
}

// TestDiscoverSkipsBlankVersions covers an index entry with no version: it is
// dropped rather than surfaced as an empty candidate.
func TestDiscoverSkipsBlankVersions(t *testing.T) {
	t.Parallel()

	const body = `[
		{"version": "", "stable": true, "files": []},
		{"version": "go1.26.5", "stable": true, "files": []}
	]`

	p := newProvider(body)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"1.26.5"}, versions(got))
}

func TestDiscoverErrorStatus(t *testing.T) {
	t.Parallel()

	p := golang.New(golang.WithTransport(
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
	require.EqualError(t, err, "go: list releases: not found (404 Not Found)")
}

func TestDiscoverDecodeError(t *testing.T) {
	t.Parallel()

	p := newProvider("not json")
	_, err := p.Discover(t.Context(), resourceFor(t, p))
	require.Error(t, err)
}

func TestDiscoverInvalidResource(t *testing.T) {
	t.Parallel()

	_, err := golang.New().Discover(t.Context(), "not-a-resource")
	require.EqualError(t, err, "go: invalid resource string")
}
