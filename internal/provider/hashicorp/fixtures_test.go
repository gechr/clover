package hashicorp_test

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider/hashicorp"
	"github.com/stretchr/testify/require"
)

// fixtureProvider serves the testdata/<product>.json fixture for a request,
// keyed by the product in the list endpoint's path. The fixtures are real,
// verbatim responses captured from api.releases.hashicorp.com, so the tests
// exercise the actual shape of the data (extra fields, every build flavor,
// prereleases, and the per-product FIPS token).
func fixtureProvider(t *testing.T) *hashicorp.Provider {
	t.Helper()
	return hashicorp.New(hashicorp.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			product := path.Base(req.URL.Path)
			body, err := os.ReadFile(filepath.Join("testdata", product+".json"))
			require.NoError(t, err)
			return jsonResponse(req, string(body)), nil
		}),
	))
}

// discoverFixture resolves candidates for a product against its fixture.
func discoverFixture(t *testing.T, product string, extra ...directive.KV) []model.Candidate {
	t.Helper()
	p := fixtureProvider(t)
	pairs := append([]directive.KV{{Key: "product", Value: product}}, extra...)
	candidates, err := p.Discover(t.Context(), resourceFor(t, p, pairs...))
	require.NoError(t, err)
	return candidates
}

// prereleaseByVersion maps each candidate's version to its prerelease flag.
func prereleaseByVersion(candidates []model.Candidate) map[string]bool {
	out := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		out[c.Version] = c.Prerelease
	}
	return out
}

// TestFixtureVaultEditions covers vault, whose page carries the full enterprise
// flavor matrix (ent, ent.hsm, ent.fips1403, ent.hsm.fips1403) alongside a lone
// open-source release.
func TestFixtureVaultEditions(t *testing.T) {
	t.Parallel()

	t.Run("oss is the lone open-source release on the page", func(t *testing.T) {
		t.Parallel()
		got := discoverFixture(t, "vault")
		require.Equal(t, []string{"2.0.3"}, versions(got))
	})

	t.Run("enterprise renders bare semver, collapsing every flavor", func(t *testing.T) {
		t.Parallel()
		got := discoverFixture(t, "vault", directive.KV{Key: "enterprise", Value: "true"})
		require.Equal(t,
			[]string{"2.0.3", "1.21.8", "1.20.13", "1.19.19", "2.0.2", "1.21.7"},
			versions(got),
		)
	})

	t.Run("build=ent.hsm.fips1403 selects the exact flavor, full suffix", func(t *testing.T) {
		t.Parallel()
		got := discoverFixture(t, "vault", directive.KV{Key: "build", Value: "ent.hsm.fips1403"})
		require.Equal(t, []string{
			"2.0.3+ent.hsm.fips1403",
			"1.21.8+ent.hsm.fips1403",
			"1.20.13+ent.hsm.fips1403",
			"1.19.19+ent.hsm.fips1403",
			"2.0.2+ent.hsm.fips1403",
			"1.21.7+ent.hsm.fips1403",
		}, versions(got))
	})

	t.Run("build=ent matches only the plain enterprise flavor", func(t *testing.T) {
		t.Parallel()
		got := discoverFixture(t, "vault", directive.KV{Key: "build", Value: "ent"})
		require.Equal(t,
			[]string{"2.0.3+ent", "1.21.8+ent", "1.20.13+ent", "1.19.19+ent"},
			versions(got),
		)
	})

	t.Run("build=ent.hsm does not bleed into ent.hsm.fips1403", func(t *testing.T) {
		t.Parallel()
		got := discoverFixture(t, "vault", directive.KV{Key: "build", Value: "ent.hsm"})
		require.Equal(t,
			[]string{"2.0.3+ent.hsm", "1.21.8+ent.hsm", "1.20.13+ent.hsm", "1.19.19+ent.hsm"},
			versions(got),
		)
	})

	t.Run("every candidate carries a parsed semver and a published date", func(t *testing.T) {
		t.Parallel()
		got := discoverFixture(t, "vault", directive.KV{Key: "build", Value: "ent.hsm.fips1403"})
		for _, c := range got {
			require.NotNil(t, c.Semver, c.Version)
			require.False(t, c.PublishedAt.IsZero(), c.Version)
		}
	})
}

// TestFixtureNomadMusl covers nomad, whose enterprise flavor is musl - a build
// token that neither an hsm nor a fips boolean would have reached.
func TestFixtureNomadMusl(t *testing.T) {
	t.Parallel()

	t.Run("oss", func(t *testing.T) {
		t.Parallel()
		got := discoverFixture(t, "nomad")
		require.Equal(t, []string{"2.0.3", "2.0.2"}, versions(got))
	})

	t.Run("build=ent.musl", func(t *testing.T) {
		t.Parallel()
		got := discoverFixture(t, "nomad", directive.KV{Key: "build", Value: "ent.musl"})
		require.Equal(t, []string{
			"2.0.3+ent.musl",
			"1.11.7+ent.musl",
			"1.10.13+ent.musl",
			"2.0.2+ent.musl",
			"1.11.6+ent.musl",
			"1.10.12+ent.musl",
			"1.10.11+ent.musl",
			"2.0.1+ent.musl",
			"1.11.5+ent.musl",
		}, versions(got))
	})
}

// TestFixtureConsulFipsAndPrereleases covers consul, whose FIPS token is fips1402
// (not vault's fips1403) and whose page includes enterprise prereleases.
func TestFixtureConsulFipsAndPrereleases(t *testing.T) {
	t.Parallel()

	t.Run("build=ent.fips1402 resolves the product's own fips token", func(t *testing.T) {
		t.Parallel()
		got := discoverFixture(t, "consul", directive.KV{Key: "build", Value: "ent.fips1402"})
		require.Equal(t, []string{
			"1.22.9+ent.fips1402",
			"1.21.15+ent.fips1402",
			"2.0.1+ent.fips1402",
			"1.22.8+ent.fips1402",
			"1.21.14+ent.fips1402",
			"2.0.0+ent.fips1402",
			"2.0.0-rc2+ent.fips1402",
			"2.0.0-rc1+ent.fips1402",
		}, versions(got))
	})

	t.Run("prerelease flag is carried through, suffix or not", func(t *testing.T) {
		t.Parallel()
		oss := prereleaseByVersion(discoverFixture(t, "consul"))
		require.False(t, oss["2.0.1"])
		require.False(t, oss["2.0.0"])
		require.True(t, oss["2.0.0-rc2"])
		require.True(t, oss["2.0.0-rc1"])

		fips := prereleaseByVersion(discoverFixture(t, "consul",
			directive.KV{Key: "build", Value: "ent.fips1402"}))
		require.False(t, fips["2.0.0+ent.fips1402"])
		require.True(t, fips["2.0.0-rc2+ent.fips1402"])
	})
}

// TestFixtureTerraformOSS covers terraform, which publishes only open-source
// releases (so enterprise/build selectors find nothing) and a mix of stable
// releases with alpha/beta/rc prereleases.
func TestFixtureTerraformOSS(t *testing.T) {
	t.Parallel()

	t.Run("default lists every release, bare", func(t *testing.T) {
		t.Parallel()
		got := discoverFixture(t, "terraform")
		require.Len(t, got, 20)
		require.Equal(t, "1.16.0-alpha20260626", got[0].Version)

		pre := prereleaseByVersion(got)
		require.True(t, pre["1.16.0-alpha20260626"])
		require.True(t, pre["1.15.0-rc4"])
		require.True(t, pre["1.15.0-beta2"])
		require.False(t, pre["1.15.7"])
		require.False(t, pre["1.15.0"])
	})

	t.Run("enterprise and build find nothing", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, versions(discoverFixture(t, "terraform",
			directive.KV{Key: "enterprise", Value: "true"})))
		require.Empty(t, versions(discoverFixture(t, "terraform",
			directive.KV{Key: "build", Value: "ent"})))
	})
}
