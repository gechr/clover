package pypi_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/pypi"
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
func resourceFor(t *testing.T, p *pypi.Provider, pairs ...directive.KV) provider.Resource {
	t.Helper()
	res, err := p.Resource(directiveOf(pairs...))
	require.NoError(t, err)
	return res
}

// newProvider returns a provider whose transport serves the given listing body.
func newProvider(body string) *pypi.Provider {
	return pypi.New(pypi.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, body), nil
		}),
	))
}

// versions extracts the version strings from candidates, in order.
func versions(candidates []model.Candidate) []string {
	return xslices.Map(candidates, func(c model.Candidate) string { return c.Version })
}

// discoverBody resolves candidates for the requests package against a literal
// listing body.
func discoverBody(t *testing.T, body string) []model.Candidate {
	t.Helper()
	p := newProvider(body)
	candidates, err := p.Discover(
		t.Context(),
		resourceFor(t, p, directive.KV{Key: "package", Value: "requests"}),
	)
	require.NoError(t, err)
	return candidates
}

// TestDiscoverSkipsUninstallableVersions covers the liveness gates: a version
// with no files and a version whose every file is yanked are both dropped,
// while a version with one yanked and one live file survives, dated by the
// live file alone.
func TestDiscoverSkipsUninstallableVersions(t *testing.T) {
	t.Parallel()

	const body = `{"releases": {
		"1.0.0": [],
		"1.1.0": [{"filename": "p-1.1.0.tar.gz", "yanked": true, "upload_time_iso_8601": "2026-01-01T00:00:00Z", "digests": {"sha256": "aa"}}],
		"1.2.0": [
			{"filename": "p-1.2.0.tar.gz", "yanked": true, "upload_time_iso_8601": "2026-02-01T00:00:00Z", "digests": {"sha256": "bb"}},
			{"filename": "p-1.2.0-py3-none-any.whl", "yanked": false, "upload_time_iso_8601": "2026-02-02T00:00:00Z", "digests": {"sha256": "cc"}}
		]
	}}`

	got := discoverBody(t, body)
	require.Equal(t, []string{"1.2.0"}, versions(got))
	require.Equal(t,
		time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
		got[0].PublishedAt,
		"the yanked file's earlier upload time does not date the version",
	)
	require.Equal(t,
		[]model.Asset{{Name: "p-1.2.0-py3-none-any.whl", Digest: "sha256:cc"}},
		got[0].Assets,
		"only the live file surfaces as an asset",
	)
}

// TestDiscoverSkipsUnparseableVersions covers the semver gate: a .dev suffix
// and an epoch are not orderable, so they are dropped, while a dashless PEP 440
// prerelease is normalized to canonical semver with the raw form kept on Ref.
func TestDiscoverSkipsUnparseableVersions(t *testing.T) {
	t.Parallel()

	const body = `{"releases": {
		"2.0.dev1": [{"filename": "p-2.0.dev1.tar.gz", "yanked": false, "upload_time_iso_8601": "2026-01-01T00:00:00Z", "digests": {"sha256": "aa"}}],
		"1!2.0": [{"filename": "p-2.0.tar.gz", "yanked": false, "upload_time_iso_8601": "2026-01-03T00:00:00Z", "digests": {"sha256": "cc"}}],
		"2.1rc1": [{"filename": "p-2.1rc1.tar.gz", "yanked": false, "upload_time_iso_8601": "2026-01-04T00:00:00Z", "digests": {"sha256": "dd"}}]
	}}`

	got := discoverBody(t, body)
	require.Equal(t, []string{"2.1.0-rc1"}, versions(got))
	require.Equal(t, "2.1rc1", got[0].Ref)
	require.NotNil(t, got[0].Semver)
	require.Equal(t, "rc1", got[0].Semver.Prerelease())
}

// TestDiscoverPostRelease covers PEP 440 post-release support: 1.0.post1 is
// discovered and ordered as an extra segment (1.0.1) - after its base release
// and before the next - while the candidate keeps the real PyPI spelling on
// both Version and Ref, so the line is rewritten to a version pip understands.
func TestDiscoverPostRelease(t *testing.T) {
	t.Parallel()

	const body = `{"releases": {
		"1.0": [{"filename": "p-1.0.tar.gz", "yanked": false, "upload_time_iso_8601": "2026-01-01T00:00:00Z", "digests": {"sha256": "aa"}}],
		"1.0.post1": [{"filename": "p-1.0.post1.tar.gz", "yanked": false, "upload_time_iso_8601": "2026-01-02T00:00:00Z", "digests": {"sha256": "bb"}}],
		"1.1": [{"filename": "p-1.1.tar.gz", "yanked": false, "upload_time_iso_8601": "2026-01-03T00:00:00Z", "digests": {"sha256": "cc"}}]
	}}`

	got := discoverBody(t, body)
	require.Equal(t, []string{"1.0.0", "1.0.post1", "1.1.0"}, versions(got))

	var post model.Candidate
	for _, c := range got {
		if c.Ref == "1.0.post1" {
			post = c
		}
	}
	require.Equal(t, "1.0.post1", post.Version, "the line keeps the real PyPI spelling")
	require.NotNil(t, post.Semver)
	require.Equal(t, "1.0.1", post.Semver.String(),
		"the post number orders as an extra segment, after 1.0 and before 1.1")
}

// TestDiscoverMissingDigestLeavesAssetUndigested covers a file record without
// a sha256: the asset surfaces with an empty digest rather than a bare
// "sha256:" prefix.
func TestDiscoverMissingDigestLeavesAssetUndigested(t *testing.T) {
	t.Parallel()

	const body = `{"releases": {
		"1.0.0": [{"filename": "p-1.0.0.tar.gz", "yanked": false, "upload_time_iso_8601": "2026-01-01T00:00:00Z", "digests": {}}]
	}}`

	got := discoverBody(t, body)
	require.Equal(t, []model.Asset{{Name: "p-1.0.0.tar.gz"}}, got[0].Assets)
}

func TestDiscoverError(t *testing.T) {
	t.Parallel()

	p := pypi.New(pypi.WithTransport(
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
		resourceFor(t, p, directive.KV{Key: "package", Value: "no-such-package"}),
	)
	require.Error(t, err)
}

func TestDiscoverInvalidResource(t *testing.T) {
	t.Parallel()

	_, err := pypi.New().Discover(t.Context(), "not-a-resource")
	require.EqualError(t, err, "pypi: invalid resource string")
}
