package python_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/python"
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
func resourceFor(t *testing.T, p *python.Provider) provider.Resource {
	t.Helper()
	res, err := p.Resource(directiveOf())
	require.NoError(t, err)
	return res
}

// newProvider returns a provider whose transport serves the given API body.
func newProvider(body string) *python.Provider {
	return python.New(python.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, body), nil
		}),
	))
}

// versions extracts the version strings from candidates, in order.
func versions(candidates []model.Candidate) []string {
	return xslices.Map(candidates, func(c model.Candidate) string {
		return c.Version
	})
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

const twoStable = `[
	{"name": "Python 3.14.6", "slug": "python-3146", "is_published": true, "pre_release": false, "release_date": "2026-06-10T13:13:18Z"},
	{"name": "Python 3.13.14", "slug": "python-31314", "is_published": true, "pre_release": false, "release_date": "2025-01-01T00:00:00Z"}
]`

const withPre = `[
	{"name": "Python 3.15.0b3", "slug": "python-3150b3", "is_published": true, "pre_release": true, "release_date": "2026-06-23T13:25:25Z"},
	{"name": "Python 3.14.6", "slug": "python-3146", "is_published": true, "pre_release": false, "release_date": "2026-06-10T13:13:18Z"},
	{"name": "Python 3.14.5", "slug": "python-3145", "is_published": true, "pre_release": false, "release_date": "2026-05-10T12:24:58Z"}
]`

// TestDiscoverStripsPythonPrefix covers the core transform: the "Python " prefix
// is dropped, Version is the parsed canonical semver, and Ref keeps the bare form.
func TestDiscoverStripsPythonPrefix(t *testing.T) {
	t.Parallel()

	p := newProvider(twoStable)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"3.14.6", "3.13.14"}, versions(got))
	require.Equal(t, "3.14.6", got[0].Ref)
	require.NotNil(t, got[0].Semver)
}

// TestDiscoverPrerelease covers the dashless prerelease: it is normalised to
// canonical semver and flagged from the API's pre_release field.
func TestDiscoverPrerelease(t *testing.T) {
	t.Parallel()

	p := newProvider(withPre)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, "3.15.0-b3", got[0].Version)
	require.Equal(t, "3.15.0b3", got[0].Ref)
	require.True(t, got[0].Prerelease)
}

// TestDiscoverPublishedAt covers cooldown's input: the release date is decoded
// onto the candidate.
func TestDiscoverPublishedAt(t *testing.T) {
	t.Parallel()

	p := newProvider(twoStable)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 6, 10, 13, 13, 18, 0, time.UTC), got[0].PublishedAt)
}

// TestDiscoverNullDate covers a null release_date: it decodes to a zero time
// (cooldown inert for that entry) rather than erroring the whole fetch.
func TestDiscoverNullDate(t *testing.T) {
	t.Parallel()

	const body = `[{"name": "Python 3.14.6", "slug": "python-3146", "is_published": true, "pre_release": false, "release_date": null}]`
	p := newProvider(body)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.True(t, got[0].PublishedAt.IsZero())
}

// TestDiscoverSkipsUnpublished covers the belt-and-braces guard: the request URL
// already filters on is_published, but a scheduled release must still be dropped
// if the endpoint ever stops honoring the query param.
func TestDiscoverSkipsUnpublished(t *testing.T) {
	t.Parallel()

	const body = `[
		{"name": "Python 3.15.0", "slug": "python-3150", "is_published": false, "pre_release": false, "release_date": "2026-10-01T00:00:00Z"},
		{"name": "Python 3.14.6", "slug": "python-3146", "is_published": true, "pre_release": false, "release_date": "2026-06-10T13:13:18Z"}
	]`
	p := newProvider(body)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"3.14.6"}, versions(got))
}

// TestDiscoverRequestURL pins the endpoint, including the is_published filter -
// a typo'd URL would otherwise pass every mocked test.
func TestDiscoverRequestURL(t *testing.T) {
	t.Parallel()

	var got string
	p := python.New(python.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			got = req.URL.String()
			return jsonResponse(req, "[]"), nil
		}),
	))
	_, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, "https://www.python.org/api/v2/downloads/release/?is_published=true", got)
}

// TestSelectionExcludesPrereleaseByDefault covers the prerelease gate.
func TestSelectionExcludesPrereleaseByDefault(t *testing.T) {
	t.Parallel()

	p := newProvider(withPre)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	current, err := version.Parse("3.14.5")
	require.NoError(t, err)

	chosen, ok := version.Select(current, got, attrs)
	require.True(t, ok)
	require.Equal(t, "3.14.6", chosen.Version)
}

// TestSelectionPrereleaseOptIn covers the opt-in: with prereleases allowed the
// beta is eligible and, being the highest, is selected.
func TestSelectionPrereleaseOptIn(t *testing.T) {
	t.Parallel()

	p := newProvider(withPre)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	current, err := version.Parse("3.14.6")
	require.NoError(t, err)

	chosen, ok := version.Select(current, got, attrs, version.WithPrerelease(true))
	require.True(t, ok)
	require.Equal(t, "3.15.0-b3", chosen.Version)
}

// TestSelectionCooldown covers date-driven cooldown: with a cooldown in force the
// too-fresh newest release is held back and the newest older one is selected.
func TestSelectionCooldown(t *testing.T) {
	t.Parallel()

	p := newProvider(twoStable)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	current, err := version.Parse("3.13.0")
	require.NoError(t, err)

	// 3.14.6 published 2026-06-10; a now 5 days later with a 30-day cooldown makes
	// it too fresh, so 3.13.14 (an older line) is the newest eligible upgrade.
	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	chosen, ok := version.Select(current, got, attrs,
		version.WithCooldown(30*24*time.Hour), version.WithNow(now))
	require.True(t, ok)
	require.Equal(t, "3.13.14", chosen.Version)
}

func TestDiscoverErrorStatus(t *testing.T) {
	t.Parallel()

	p := python.New(python.WithTransport(
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
	require.EqualError(t, err, "python: list releases: not found (404 Not Found)")
}

func TestDiscoverDecodeError(t *testing.T) {
	t.Parallel()

	p := newProvider("not json")
	_, err := p.Discover(t.Context(), resourceFor(t, p))
	require.Error(t, err)
}

func TestDiscoverInvalidResource(t *testing.T) {
	t.Parallel()

	_, err := python.New().Discover(t.Context(), "not-a-resource")
	require.EqualError(t, err, "python: invalid resource string")
}
