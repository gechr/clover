package rust_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/rust"
	"github.com/gechr/clover/internal/version"
	xslices "github.com/gechr/x/slices"
	"github.com/stretchr/testify/require"
)

func textResponse(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

// resourceFor builds a validated resource from the given directive pairs.
func resourceFor(t *testing.T, p *rust.Provider, pairs ...directive.KV) provider.Resource {
	t.Helper()
	res, err := p.Resource(directiveOf(pairs...))
	require.NoError(t, err)
	return res
}

// betaChannel is the directive pair selecting the beta channel.
var betaChannel = directive.KV{Key: "channel", Value: "beta"}

// newProvider returns a provider whose transport serves the given index body.
func newProvider(body string) *rust.Provider {
	return rust.New(rust.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return textResponse(req, body), nil
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

const stableIndex = `static.rust-lang.org/dist/2016-04-12/channel-rust-stable.toml
static.rust-lang.org/dist/2016-04-12/channel-rust-1.8.0.toml
static.rust-lang.org/dist/2016-04-14/channel-rust-1.8.0.toml
static.rust-lang.org/dist/2023-12-28/channel-rust-1.75.toml
static.rust-lang.org/dist/2023-12-28/channel-rust-1.75.0.toml
static.rust-lang.org/dist/2026-07-09/channel-rust-1.97.0.toml
`

const betaIndex = `static.rust-lang.org/dist/2023-11-13/channel-rust-1.75-beta.toml
static.rust-lang.org/dist/2023-11-13/channel-rust-1.75.0-beta.toml
static.rust-lang.org/dist/2023-11-13/channel-rust-1.75.0-beta.1.toml
static.rust-lang.org/dist/2026-07-06/channel-rust-beta.toml
static.rust-lang.org/dist/2026-07-06/channel-rust-1.98.0-beta.1.toml
static.rust-lang.org/dist/2026-07-09/channel-rust-1.97.0.toml
`

// TestDiscoverStable covers the core transform: only the full X.Y.Z manifests
// become candidates - the channel pointer and the minor alias (1.75, which would
// parse to the same 1.75.0) are dropped - and a version re-published under a
// later directory keeps its first date.
func TestDiscoverStable(t *testing.T) {
	t.Parallel()

	p := newProvider(stableIndex)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"1.8.0", "1.75.0", "1.97.0"}, versions(got))
	require.Equal(t, "1.8.0", got[0].Ref)
	require.NotNil(t, got[0].Semver)
	require.Equal(t, time.Date(2016, 4, 12, 0, 0, 0, 0, time.UTC), got[0].PublishedAt)
}

// TestDiscoverBeta covers the beta channel: only the numbered X.Y.Z-beta.N
// snapshots become candidates - the moving 1.75-beta and 1.75.0-beta aliases
// (which would both parse to the same 1.75.0-beta) and the stable releases are
// dropped.
func TestDiscoverBeta(t *testing.T) {
	t.Parallel()

	p := newProvider(betaIndex)
	got, err := p.Discover(t.Context(), resourceFor(t, p, betaChannel))
	require.NoError(t, err)
	require.Equal(t, []string{"1.75.0-beta.1", "1.98.0-beta.1"}, versions(got))
	require.Equal(t, time.Date(2023, 11, 13, 0, 0, 0, 0, time.UTC), got[0].PublishedAt)
}

// TestDiscoverStableExcludesBetas covers the channel split from the other side:
// the default stable channel never lists a beta snapshot.
func TestDiscoverStableExcludesBetas(t *testing.T) {
	t.Parallel()

	p := newProvider(betaIndex)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"1.97.0"}, versions(got))
}

// TestDiscoverSkipsMalformedLines covers resilience: lines that are not
// version-named manifest paths - other hosts, other files, an impossible date -
// are skipped rather than surfaced or fatal.
func TestDiscoverSkipsMalformedLines(t *testing.T) {
	t.Parallel()

	const body = `example.com/dist/2026-07-09/channel-rust-1.96.0.toml
static.rust-lang.org/dist/2026-07-09/rust-1.96.0-x86_64-unknown-linux-gnu.tar.xz
static.rust-lang.org/dist/2026-13-40/channel-rust-1.96.0.toml
not a manifest line at all
static.rust-lang.org/dist/2026-07-09/channel-rust-1.97.0.toml
`
	p := newProvider(body)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"1.97.0"}, versions(got))
}

// TestSelectionBetaNeedsPrereleaseOptIn covers the framework interplay the docs
// promise: a beta snapshot is semver-prerelease, so without the prerelease
// opt-in nothing on the beta channel is eligible, and the binding reason says
// so.
func TestSelectionBetaNeedsPrereleaseOptIn(t *testing.T) {
	t.Parallel()

	p := newProvider(betaIndex)
	got, err := p.Discover(t.Context(), resourceFor(t, p, betaChannel))
	require.NoError(t, err)

	_, reason, ok := version.SelectReason(nil, got, attrs)
	require.False(t, ok)
	require.Equal(t, version.ReasonPrerelease, reason)

	chosen, ok := version.Select(nil, got, attrs, version.WithPrerelease(true))
	require.True(t, ok)
	require.Equal(t, "1.98.0-beta.1", chosen.Version)
}

// TestSelectionCooldown covers date-driven cooldown: with a cooldown in force
// the too-fresh newest release is held back and the newest older one is
// selected.
func TestSelectionCooldown(t *testing.T) {
	t.Parallel()

	p := newProvider(stableIndex)
	got, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)

	current, err := version.Parse("1.8.0")
	require.NoError(t, err)

	// 1.97.0 published 2026-07-09; a now 5 days later with a 30-day cooldown
	// makes it too fresh, so 1.75.0 is the newest eligible upgrade.
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	chosen, ok := version.Select(current, got, attrs,
		version.WithCooldown(30*24*time.Hour), version.WithNow(now))
	require.True(t, ok)
	require.Equal(t, "1.75.0", chosen.Version)
}

func TestDiscoverErrorStatus(t *testing.T) {
	t.Parallel()

	p := rust.New(rust.WithTransport(
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
	require.EqualError(t, err, "rust: list releases: not found (404 Not Found)")
}

func TestDiscoverInvalidResource(t *testing.T) {
	t.Parallel()

	_, err := rust.New().Discover(t.Context(), "not-a-resource")
	require.EqualError(t, err, "rust: invalid resource string")
}
