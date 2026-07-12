package npm_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider/npm"
	"github.com/stretchr/testify/require"
)

// fixtureProvider serves the named testdata packument for every request. The
// fixtures are real, verbatim slices of registry.npmjs.org packuments, keeping
// the full field set (maintainers, dist signatures, deprecated, ...) the
// provider ignores, so the tests exercise the actual shape of the data.
func fixtureProvider(t *testing.T, file string) *npm.Provider {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", file))
	require.NoError(t, err)
	return newProvider(string(body))
}

// discoverFixture resolves candidates for the given directive pairs against a
// fixture packument.
func discoverFixture(t *testing.T, file string, pairs ...directive.KV) []model.Candidate {
	t.Helper()
	p := fixtureProvider(t, file)
	candidates, err := p.Discover(t.Context(), resourceFor(t, p, pairs...))
	require.NoError(t, err)
	return candidates
}

// candidateFor returns the candidate carrying the given version.
func candidateFor(t *testing.T, candidates []model.Candidate, v string) model.Candidate {
	t.Helper()
	for _, c := range candidates {
		if c.Version == v {
			return c
		}
	}
	t.Fatalf("no candidate for version %q", v)
	return model.Candidate{}
}

// TestFixtureDiscover covers an unscoped package (left-pad): only the versions
// map drives the listing - the fixture's time map also dates versions with no
// versions entry, mirroring an unpublished version, and none of those surface.
// Every left-pad version is deprecated, so the default listing is empty and
// deprecated=true restores it. Every candidate parses, is dated, and carries
// its tarball as its sole asset.
func TestFixtureDiscover(t *testing.T) {
	t.Parallel()

	require.Empty(t, discoverFixture(t, "left-pad.json",
		directive.KV{Key: "package", Value: "left-pad"},
	))

	got := discoverFixture(t, "left-pad.json",
		directive.KV{Key: "package", Value: "left-pad"},
		directive.KV{Key: "deprecated", Value: "true"},
	)
	require.Equal(t, []string{"0.0.3", "0.0.9", "1.0.0", "1.2.0", "1.3.0"}, versions(got))

	for _, c := range got {
		require.NotNil(t, c.Semver, c.Version)
		require.Equal(t, c.Version, c.Ref)
		require.False(t, c.PublishedAt.IsZero(), c.Version)
	}

	latest := candidateFor(t, got, "1.3.0")
	require.Equal(t,
		time.Date(2018, 4, 9, 1, 10, 45, 796_000_000, time.UTC),
		latest.PublishedAt,
	)
	require.Equal(t, []model.Asset{{
		Name: "left-pad-1.3.0.tgz",
		URL:  "https://registry.npmjs.org/left-pad/-/left-pad-1.3.0.tgz",
	}}, latest.Assets)
}

// TestFixtureUnpublished covers a fully unpublished package (wowdude-11): the
// registry replaces its versions map with an "unpublished" object inside the
// time map, so discovery must return no candidates rather than fail the decode
// on the non-date value.
func TestFixtureUnpublished(t *testing.T) {
	t.Parallel()

	got := discoverFixture(t, "wowdude-11.json",
		directive.KV{Key: "package", Value: "wowdude-11"},
	)
	require.Empty(t, got)
}

// TestFixtureScoped covers a scoped package (@vue/reactivity): stable and
// prerelease versions both surface - a beta parses with its prerelease part and
// selection handles the gating - and the scoped tarball's asset name is the
// bare basename, without the scope.
func TestFixtureScoped(t *testing.T) {
	t.Parallel()

	got := discoverFixture(t, "vue-reactivity.json",
		directive.KV{Key: "package", Value: "@vue/reactivity"},
	)
	require.Equal(t, []string{"3.5.38", "3.5.39", "3.6.0-beta.17"}, versions(got))

	stable := candidateFor(t, got, "3.5.39")
	require.Equal(t,
		time.Date(2026, 6, 25, 9, 43, 30, 837_000_000, time.UTC),
		stable.PublishedAt,
	)

	beta := candidateFor(t, got, "3.6.0-beta.17")
	require.NotNil(t, beta.Semver)
	require.False(t, beta.Prerelease, "npm has no out-of-band prerelease flag")
	require.Equal(t, []model.Asset{{
		Name: "reactivity-3.6.0-beta.17.tgz",
		URL:  "https://registry.npmjs.org/@vue/reactivity/-/reactivity-3.6.0-beta.17.tgz",
	}}, beta.Assets)
}

// discoverTag resolves candidates for the scoped fixture package narrowed to a
// dist-tag.
func discoverTag(t *testing.T, p *npm.Provider, tag string) ([]model.Candidate, error) {
	t.Helper()
	return p.Discover(
		t.Context(),
		resourceFor(t, p,
			directive.KV{Key: "package", Value: "@vue/reactivity"},
			directive.KV{Key: "dist-tag", Value: tag},
		),
	)
}

// TestFixtureDistTag covers the dist-tag key: each tag resolves to exactly the
// version the registry's pointer names, a tag whose version has no versions
// entry still surfaces (undated and assetless), and a tag the registry does not
// carry is its own error, distinct from a missing package's 404.
func TestFixtureDistTag(t *testing.T) {
	t.Parallel()

	p := fixtureProvider(t, "vue-reactivity.json")

	beta, err := discoverTag(t, p, "beta")
	require.NoError(t, err)
	require.Equal(t, []string{"3.6.0-beta.17"}, versions(beta))
	require.Equal(t,
		time.Date(2026, 6, 24, 9, 18, 46, 592_000_000, time.UTC),
		beta[0].PublishedAt,
	)
	require.Equal(t, []model.Asset{{
		Name: "reactivity-3.6.0-beta.17.tgz",
		URL:  "https://registry.npmjs.org/@vue/reactivity/-/reactivity-3.6.0-beta.17.tgz",
	}}, beta[0].Assets)

	latest, err := discoverTag(t, p, "latest")
	require.NoError(t, err)
	require.Equal(t, []string{"3.5.39"}, versions(latest))

	// The trimmed fixture keeps the rc tag but not its version's entries.
	rc, err := discoverTag(t, p, "rc")
	require.NoError(t, err)
	require.Equal(t, []string{"3.5.0-rc.1"}, versions(rc))
	require.True(t, rc[0].PublishedAt.IsZero())
	require.Empty(t, rc[0].Assets)

	_, err = discoverTag(t, p, "next")
	require.EqualError(t, err, `npm: package "@vue/reactivity" has no dist-tag "next"`)
}
