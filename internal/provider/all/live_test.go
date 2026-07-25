//go:build live

// Package all_test's live smoke test. Behind the live build tag so it is never
// part of the standard gate: it needs the network, spends real rate limit, and
// fails when an upstream is down. Run it with `make smoke`.
//
// `make lint` vets this file with the tag set, so it is compiled and checked
// like any other - a drift detector that silently stopped compiling would be
// worse than none.
package all_test

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/all"
	"github.com/gechr/clover/internal/version"
	"github.com/stretchr/testify/require"
)

// liveTimeout bounds each probe, so a hanging upstream fails rather than stalls.
const liveTimeout = 90 * time.Second

// TestLiveDiscover resolves every probed resource against its actual upstream,
// asserting only that candidates come back and carry parseable versions.
//
// It exists because a hermetic fixture proves the parser reads that fixture, not
// that it reads the upstream. Both are needed: the fixture tests pin behaviour
// precisely and run everywhere, while this catches the divergence they cannot
// see - a response shape that changed, or one that was hand-written wrongly in
// the first place. That is not hypothetical here: the Terraform module listing
// nests its versions where the provider listing does not, and a fixture copied
// from the provider shape passed while the live endpoint yielded zero
// candidates.
//
// Assertions stay deliberately weak. A specific version would rot, and an
// ordering claim belongs in the fixture tests; an empty listing is the shape
// every parsing divergence collapses to.
func TestLiveDiscover(t *testing.T) {
	t.Parallel()

	for _, p := range all.New("") {
		if reason, exempt := liveExempt[p.Name()]; exempt {
			t.Run(p.Name(), func(t *testing.T) { t.Skipf("%s %s", p.Name(), reason) })
			continue
		}
		for _, probe := range liveProbes[p.Name()] {
			t.Run(p.Name()+"/"+probe.shape, func(t *testing.T) {
				t.Parallel()

				ctx, cancel := context.WithTimeout(t.Context(), liveTimeout)
				defer cancel()

				resource, err := p.Resource(directive.Directive{Pairs: probe.pairs})
				require.NoError(t, err, "the probe directive must build a resource")

				candidates, err := p.Discover(ctx, resource)
				require.NoError(t, err)
				require.NotEmpty(t, candidates,
					"%s returned no candidates for %s, which is how a response shape the "+
						"provider no longer parses presents itself",
					p.Name(), p.Describe(resource))

				parseable := 0
				for _, c := range candidates {
					require.NotEmpty(t, c.Version, "every candidate must carry a version")
					if _, err := version.Parse(c.Version); err == nil {
						parseable++
					}
				}
				require.NotZero(t, parseable,
					"%s returned %d candidates but none parsed as a version, so the field "+
						"read is probably the wrong one",
					p.Name(), len(candidates))

				// First-listed, not newest: only a RecencyOrderer promises an order,
				// and an unparseable tail is normal (an OCI listing carries latest and
				// edge beside its versions).
				t.Logf("%s/%s: %d candidates, %d parseable, first-listed %q",
					p.Name(), probe.shape, len(candidates), parseable, candidates[0].Version)
			})
		}
	}
}

// TestLiveCapabilities exercises the optional capabilities that read upstream
// responses Discover never touches. Several back a secure pin, so a divergence
// there stays silent until a pin is written wrong.
//
// The runners are keyed by provider/capability and checked against the coverage
// map, so a pair recorded as probed but never run fails here - the map cannot
// claim coverage it does not have.
func TestLiveCapabilities(t *testing.T) {
	t.Parallel()

	runners := map[string]func(context.Context, *testing.T, provider.Provider){
		"docker/Digester":        probeDigest,
		"helm/Digester":          probeChartDigest,
		"github/Committer":       probeCommit,
		"github/AssetDownloader": probeAssetDownload,
		"github/BranchChecker":   probeBranches,
		"gitea/BranchChecker":    probeBranches,
		"gitlab/BranchChecker":   probeBranches,
	}

	keys := make([]string, 0, len(runners))
	for pair := range runners {
		keys = append(keys, pair)
	}
	require.ElementsMatch(t, probedCapabilities(), keys,
		"every pair recorded as probed needs a runner, and vice versa")

	for pair, run := range runners {
		name, _, _ := strings.Cut(pair, "/")
		t.Run(pair, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), liveTimeout)
			defer cancel()
			run(ctx, t, providerNamed(t, name))
		})
	}
}

// capabilitySubject is the resource each capability probe resolves against: this
// project's own repository where the artifact must be stable, and a long-lived
// third-party one otherwise.
var capabilitySubject = map[string]string{
	constant.ProviderGitea:  "forgejo/forgejo",
	constant.ProviderGithub: cloverRepository,
	constant.ProviderGitlab: "gitlab-org/gitlab-runner",
}

// probeDigest resolves an image's manifest digest, the value a digest-pinned
// FROM carries. Two pinned tags are resolved rather than one: a well-formed
// digest proves the response parses, but only a differing pair proves the tag
// reached the registry at all. Both are release tags, never a floating one,
// so distinct manifests are guaranteed - do not simplify either to latest.
func probeDigest(ctx context.Context, t *testing.T, p provider.Provider) {
	t.Helper()

	digester, ok := p.(provider.Digester)
	require.True(t, ok)

	resource, err := p.Resource(directive.Directive{
		Pairs: []directive.KV{{Key: constant.DirectiveRepository, Value: "library/alpine"}},
	})
	require.NoError(t, err)

	first, err := digester.Digest(ctx, resource, "3.20")
	require.NoError(t, err)
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, first)

	second, err := digester.Digest(ctx, resource, "3.19")
	require.NoError(t, err)
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, second)
	require.NotEqual(
		t,
		first,
		second,
		"two releases cannot share a manifest, so the tag is ignored",
	)

	t.Logf("docker: alpine:3.20 -> %s", first)
}

// probeCommit peels a tag to its commit, the value a secure action pin carries.
// As for the digest, two pinned tags prove the tag is honoured and not just that
// the response parses.
func probeCommit(ctx context.Context, t *testing.T, p provider.Provider) {
	t.Helper()

	committer, ok := p.(provider.Committer)
	require.True(t, ok)

	resource, err := p.Resource(directive.Directive{
		Pairs: []directive.KV{{Key: constant.DirectiveRepository, Value: "actions/checkout"}},
	})
	require.NoError(t, err)

	first, err := committer.Commit(ctx, resource, "v4.2.2")
	require.NoError(t, err)
	require.Regexp(t, `^[0-9a-f]{40}$`, first)

	second, err := committer.Commit(ctx, resource, "v4.1.0")
	require.NoError(t, err)
	require.Regexp(t, `^[0-9a-f]{40}$`, second)
	require.NotEqual(t, first, second, "two releases cannot share a commit, so the tag is ignored")

	t.Logf("github: actions/checkout@v4.2.2 -> %s", first)
}

// probeAssetDownload streams a release asset through the provider's own channel,
// the path a sha256 follower reads. The subject is this project's repository, so
// the asset cannot be renamed or withdrawn by anyone else - which is what makes
// downloading real bytes safe to run unattended.
//
// It asserts the bytes read as a checksums file rather than matching them: the
// contents change every release, the format does not.
func probeAssetDownload(ctx context.Context, t *testing.T, p provider.Provider) {
	t.Helper()

	downloader, ok := p.(provider.AssetDownloader)
	require.True(t, ok)

	// Assets hang off releases, not tags, and a forge marker discovers tags
	// unless told otherwise - so the probe must ask for releases to see any.
	resource, err := p.Resource(directive.Directive{Pairs: []directive.KV{
		{Key: constant.DirectiveRepository, Value: cloverRepository},
		{Key: forge.KeySource, Value: forge.SourceReleases},
	}})
	require.NoError(t, err)

	candidates, err := p.Discover(ctx, resource)
	require.NoError(t, err)

	asset, release, found := findAsset(candidates, checksumsAsset)
	require.True(t, found, "no clover release lists a %s asset", checksumsAsset)

	body, err := downloader.DownloadAsset(ctx, resource, asset)
	require.NoError(t, err)
	defer body.Close()

	content, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NotEmpty(t, content, "an asset that downloads to nothing is a broken stream")
	require.Regexp(t, `(?m)^[0-9a-f]{64}\s+\S+$`, string(content),
		"the asset does not read as a checksums file, so the stream carried something else")

	t.Logf("github: %s %s %s -> %d bytes", cloverRepository, release, checksumsAsset, len(content))
}

// probeBranches reads the three endpoints --verify uses to prove a pinned commit
// came from a trunk rather than an off-trunk branch.
func probeBranches(ctx context.Context, t *testing.T, p provider.Provider) {
	t.Helper()

	checker, ok := p.(provider.BranchChecker)
	require.True(t, ok)

	resource, err := p.Resource(directive.Directive{
		Pairs: []directive.KV{
			{Key: constant.DirectiveRepository, Value: capabilitySubject[p.Name()]},
		},
	})
	require.NoError(t, err)

	branch, err := checker.DefaultBranch(ctx, resource)
	require.NoError(t, err)
	require.NotEmpty(t, branch)

	branches, err := checker.Branches(ctx, resource)
	require.NoError(t, err)
	require.NotEmpty(t, branches)

	names := make([]string, 0, len(branches))
	for _, b := range branches {
		names = append(names, b.Name)
		require.NotEmpty(t, b.Tip, "a branch with no tip commit cannot anchor reachability")
	}
	require.Contains(t, names, branch, "the default branch must appear in the branch listing")

	// A branch tip is by definition reachable from its own branch, which is the
	// assertion --verify makes of a pinned commit. Using the tip keeps the probe
	// independent of any particular tag's provenance.
	tip := branches[slices.Index(names, branch)].Tip
	reachable, err := checker.Reachable(ctx, resource, branch, tip)
	require.NoError(t, err)
	require.True(t, reachable, "%s tip %s is not reachable from %s", p.Name(), tip, branch)

	t.Logf("%s: repo=%s default-branch=%s tip=%s reachable, %d branches listed",
		p.Name(), capabilitySubject[p.Name()], branch, tip[:8], len(branches))
}

// providerNamed returns the built-in provider registered under name.
func providerNamed(t *testing.T, name string) provider.Provider {
	t.Helper()

	for _, p := range all.New("") {
		if p.Name() == name {
			return p
		}
	}
	t.Fatalf("no built-in provider named %q", name)
	return nil
}

// findAsset returns the named asset from the newest candidate carrying one,
// along with that candidate's version.
func findAsset(candidates []model.Candidate, name string) (model.Asset, string, bool) {
	for _, c := range candidates {
		for _, asset := range c.Assets {
			if asset.Name == name {
				return asset, c.Version, true
			}
		}
	}
	return model.Asset{}, "", false
}

// cloverRepository is this project's own repository, the subject wherever a
// probe needs an artifact that cannot be renamed or withdrawn by a third party.
const cloverRepository = "gechr/clover"

// checksumsAsset is the small text asset every clover release publishes. It is
// the file a value=sha256 follower reads, and at a few hundred bytes it can be
// fetched in a test without downloading a binary.
const checksumsAsset = "checksums.txt"

// chartRegistry is an OCI chart repository published by the project that owns
// the charts, on a registry it controls. Deliberately not the Bitnami OCI
// catalogue: it resolves today, but its classic index is already the deprecation
// shim this file avoids, and a probe should not sit on a distribution being
// wound down.
const chartRegistry = "oci://ghcr.io/prometheus-community/charts"

// probeChartDigest resolves an OCI chart's manifest digest, the value a
// digest-pinned Helm dependency carries. Two versions from the listing are
// digested rather than one, for the same reason as the image probe: a
// well-formed digest proves the response parses, but only a differing pair
// proves the version reached the registry.
//
// The pair is taken from the listing rather than pinned, since two published
// chart versions cannot share a manifest and nothing here needs a particular
// one.
func probeChartDigest(ctx context.Context, t *testing.T, p provider.Provider) {
	t.Helper()

	digester, ok := p.(provider.Digester)
	require.True(t, ok)

	resource, err := p.Resource(directive.Directive{Pairs: []directive.KV{
		{Key: constant.DirectiveRegistry, Value: chartRegistry},
		{Key: constant.DirectiveChart, Value: "prometheus"},
	}})
	require.NoError(t, err)

	candidates, err := p.Discover(ctx, resource)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(candidates), 2, "the probe needs two versions to compare")

	first, err := digester.Digest(ctx, resource, candidates[0].Version)
	require.NoError(t, err)
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, first)

	second, err := digester.Digest(ctx, resource, candidates[len(candidates)-1].Version)
	require.NoError(t, err)
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, second)
	require.NotEqual(t, first, second,
		"two chart versions cannot share a manifest, so the version is ignored")

	t.Logf("helm: chart=%s version=%s digest=%s", chartRegistry, candidates[0].Version, first)
}
