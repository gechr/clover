package all_test

import (
	"testing"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/all"
	"github.com/stretchr/testify/require"
)

// liveProbe is one known-good resource: a directive that resolves today, and
// which its provider must return candidates for. The probes live in this
// untagged file rather than beside the test that uses them, so the coverage
// guard below runs in the standard gate.
type liveProbe struct {
	// shape names the kind of resource, and so the subtest. A provider whose
	// upstream answers in more than one shape carries one probe per shape.
	shape string
	// pairs build the directive, as a marker in a real file would.
	pairs []directive.KV
}

// liveProbes maps each provider that contacts an upstream to the resources
// probed against it. The resources are chosen to be long-lived and popular,
// since the point is to exercise the provider's parsing against the real
// response - not to assert anything about a particular version.
//
// A provider needs a probe per response shape, not per provider: one Discover
// proves only the shape it was given. Shapes are enumerated by hand and no
// upstream advertises its inventory, so the trigger is a habit rather than a
// mechanism: a new shape only ever enters this codebase alongside a hermetic
// fixture, and that fixture is the moment to add its probe here. The registry providers are the worked
// example - a module listing nests its versions where a provider listing does
// not, and a fixture written to the provider's shape once passed while the real
// module endpoint returned nothing.
var liveProbes = map[string][]liveProbe{
	constant.ProviderCrates: {{shape: "package", pairs: []directive.KV{
		{Key: constant.DirectivePackage, Value: "serde"},
	}}},
	constant.ProviderDocker: {{shape: "repository", pairs: []directive.KV{
		{Key: constant.DirectiveRepository, Value: "library/alpine"},
	}}},
	constant.ProviderGitea: {{shape: "repository", pairs: []directive.KV{
		{Key: constant.DirectiveRepository, Value: "forgejo/forgejo"},
	}}},
	constant.ProviderGithub: {{shape: "repository", pairs: []directive.KV{
		{Key: constant.DirectiveRepository, Value: "actions/checkout"},
	}}},
	constant.ProviderGitlab: {{shape: "repository", pairs: []directive.KV{
		{Key: constant.DirectiveRepository, Value: "gitlab-org/gitlab-runner"},
	}}},
	constant.ProviderGo: {{shape: "index"}},
	constant.ProviderHashicorp: {{shape: "product", pairs: []directive.KV{
		{Key: constant.DirectiveProduct, Value: "terraform"},
	}}},
	// Not the Bitnami catalog, whose public index is now a redirect shim left by
	// its 2025 deprecation: a shim's next hop changes without notice, and a probe
	// that fails for reasons upstream of us teaches everyone to ignore this test.
	constant.ProviderHelm: {{shape: "chart", pairs: []directive.KV{
		{
			Key:   constant.DirectiveRegistry,
			Value: "https://prometheus-community.github.io/helm-charts",
		},
		{Key: constant.DirectiveChart, Value: "prometheus"},
	}}},
	constant.ProviderNode: {{shape: "index"}},
	constant.ProviderNpm: {{shape: "package", pairs: []directive.KV{
		{Key: constant.DirectivePackage, Value: "prettier"},
	}}},
	constant.ProviderPypi: {{shape: "package", pairs: []directive.KV{
		{Key: constant.DirectivePackage, Value: "requests"},
	}}},
	constant.ProviderPython: {{shape: "index"}},
	constant.ProviderRust:   {{shape: "index"}},
	constant.ProviderSwift:  {{shape: "index"}},
	constant.ProviderZig:    {{shape: "index"}},
	// Both registrations probe both services of the registry protocol, since a
	// provider listing and a module listing are shaped differently and either
	// registry may change one without the other.
	constant.ProviderTerraform: {
		{shape: "provider", pairs: []directive.KV{
			{Key: constant.DirectiveSource, Value: "hashicorp/aws"},
		}},
		{shape: "module", pairs: []directive.KV{
			{Key: constant.DirectiveSource, Value: "terraform-aws-modules/vpc/aws"},
		}},
	},
	constant.ProviderOpentofu: {
		{shape: "provider", pairs: []directive.KV{
			{Key: constant.DirectiveSource, Value: "hashicorp/aws"},
		}},
		{shape: "module", pairs: []directive.KV{
			{Key: constant.DirectiveSource, Value: "terraform-aws-modules/vpc/aws"},
		}},
	},
}

// liveExempt names the providers that resolve no upstream listing of their own,
// with the reason. Kept separate from [liveProbes] so "has no probe" is a stated
// decision rather than an empty entry.
var liveExempt = map[string]string{
	constant.ProviderHTTP:   "resolves whatever endpoint a marker names, so it has no upstream of its own",
	constant.ProviderManual: "is line-anchored and contacts no upstream",
}

// TestLiveProbes is the drift guard for the probe map. It needs no network, so
// it runs in the standard gate: a provider added without a probe fails here, at
// the moment it is added, rather than the next time someone happens to run the
// live test. Behind the live build tag this check would be excluded by the very
// thing it exists to make meaningful.
func TestLiveProbes(t *testing.T) {
	t.Parallel()

	providers := all.New("")
	for _, p := range providers {
		probes, probed := liveProbes[p.Name()]
		_, exempt := liveExempt[p.Name()]

		require.NotEqual(t, probed, exempt,
			"provider %q needs live probes or an exemption, and cannot have both", p.Name())
		if exempt {
			continue
		}

		shapes := make(map[string]bool, len(probes))
		for _, probe := range probes {
			require.NotEmpty(t, probe.shape, "every probe names the shape it covers")
			require.False(t, shapes[probe.shape],
				"provider %q probes the shape %q twice", p.Name(), probe.shape)
			shapes[probe.shape] = true
			require.True(t, len(probe.pairs) > 0 || noKeyProvider(p),
				"provider %q has an empty probe but requires directive keys", p.Name())
		}
	}
	require.Len(t, providers, len(liveProbes)+len(liveExempt),
		"the probe or exemption maps name a provider that does not exist")
}

// noKeyProvider reports whether a provider needs no directive key, so an empty
// probe is a complete one - a toolchain that resolves its own single upstream.
func noKeyProvider(p provider.Provider) bool {
	for _, key := range p.Keys() {
		if key.Required {
			return false
		}
	}
	return true
}

// liveCapabilities are the optional capabilities whose implementations read an
// upstream response of their own. The marker capabilities are absent: they
// declare a fact about a provider and perform no I/O, so there is nothing for a
// live probe to catch.
var liveCapabilities = map[string]func(provider.Provider) bool{
	"AssetDownloader":     func(p provider.Provider) bool { _, ok := p.(provider.AssetDownloader); return ok },
	"AttestationVerifier": func(p provider.Provider) bool { _, ok := p.(provider.AttestationVerifier); return ok },
	"BranchChecker":       func(p provider.Provider) bool { _, ok := p.(provider.BranchChecker); return ok },
	"Committer":           func(p provider.Provider) bool { _, ok := p.(provider.Committer); return ok },
	"Digester":            func(p provider.Provider) bool { _, ok := p.(provider.Digester); return ok },
}

// liveCapabilityCoverage records the decision taken for every
// provider/capability pair that exists: an empty value means a live probe covers
// it, and a non-empty one is why it is exempt.
//
// The pairs are what drift, not the capabilities: a provider quietly gaining one
// is how helm's Digester came to be unprobed on the day the capability tests
// were written. [TestLiveCapabilityCoverage] turns that from a silent omission
// into a failing build.
var liveCapabilityCoverage = map[string]string{
	"docker/Digester":        "",
	"helm/Digester":          "",
	"gitea/BranchChecker":    "",
	"github/AssetDownloader": "",
	"github/BranchChecker":   "",
	"github/Committer":       "",
	"gitlab/BranchChecker":   "",

	"docker/AttestationVerifier": "needs a signed image and a signer policy; a third party's " +
		"key rotation would fail this test for reasons that are not ours",
	"gitea/AssetDownloader": "asset names come from the release process of a repository we do " +
		"not control, which is the churn this file must not import",
	"gitlab/AssetDownloader": "asset names come from the release process of a repository we do " +
		"not control, which is the churn this file must not import",
}

// TestLiveCapabilityCoverage is the drift guard for the capability probes, the
// second axis this file polices. Like [TestLiveProbes] it needs no network and
// runs in the standard gate, so a provider that gains a capability fails here
// rather than going quietly unprobed.
func TestLiveCapabilityCoverage(t *testing.T) {
	t.Parallel()

	pairs := map[string]bool{}
	for _, p := range all.New("") {
		for capability, implements := range liveCapabilities {
			if !implements(p) {
				continue
			}
			pair := p.Name() + "/" + capability
			pairs[pair] = true
			_, decided := liveCapabilityCoverage[pair]
			require.True(t, decided,
				"%s needs a live capability probe, or an entry saying why it has none", pair)
		}
	}
	for pair := range liveCapabilityCoverage {
		require.True(t, pairs[pair], "%s is covered but no provider implements it", pair)
	}
}

// probedCapabilities returns the provider/capability pairs a live probe must
// cover, so the tagged tests can assert their runners match this list exactly -
// without which "probed" here would be an unverifiable claim.
func probedCapabilities() []string {
	var probed []string
	for pair, exempt := range liveCapabilityCoverage {
		if exempt == "" {
			probed = append(probed, pair)
		}
	}
	return probed
}

// preCommitForgeHosts mirrors the forge hosts the pre-commit inference in
// internal/match recognizes. The duplication is deliberate: match cannot import
// provider (the github provider imports match), so this package - whose role is
// seeing every provider at once - is the only place the two halves meet.
var preCommitForgeHosts = map[string]string{
	"codeberg.org": constant.ProviderGitea,
	"github.com":   constant.ProviderGithub,
	"gitlab.com":   constant.ProviderGitlab,
}

// TestFrozenRevFollowsCommitter ties a detection rule to the capability it
// depends on. Rewriting a frozen pre-commit rev needs the commit a resolved tag
// points at, so the inference accepts one only on a forge whose provider
// implements [provider.Committer] - a decision hardcoded in internal/match,
// which cannot see the capability without an import cycle.
//
// Without this, the day another forge's provider learns to peel a tag, nothing
// says the inference may now be widened, and frozen revs there stay silently
// undetected. This fails on that day, naming the forge.
func TestFrozenRevFollowsCommitter(t *testing.T) {
	t.Parallel()

	providers := map[string]provider.Provider{}
	for _, p := range all.New("") {
		providers[p.Name()] = p
	}

	for host, name := range preCommitForgeHosts {
		p, known := providers[host]
		if !known {
			p, known = providers[name]
		}
		require.True(t, known, "no provider named %q for forge host %q", name, host)

		_, peelsTags := p.(provider.Committer)
		_, inferred := match.Infer(".pre-commit-config.yaml", []string{
			"repos:",
			"- repo: https://" + host + "/owner/tool",
			"  rev: 552baf822992936134cbd31a38f69c8cfe7c0f05",
		}, 2)

		require.Equal(t, peelsTags, inferred,
			"a frozen rev on %s is inferred exactly when %s implements Committer; "+
				"if %s has just gained it, widen inferFrozenRev in internal/match",
			host, name, name)
	}
}

// TestPreCommitCodebergDefaultFlavor guards a cross-package coupling that is
// otherwise stated only in a comment. The pre-commit inference maps
// codeberg.org to the gitea provider and supplies no flavor key, which resolves
// correctly only because gitea's default flavor is codeberg. Were that default
// to change, a codeberg-hosted hook would silently resolve against a forge that
// never published it - the exact failure the flavor refusal avoids by declining
// the non-default flavors outright.
func TestPreCommitCodebergDefaultFlavor(t *testing.T) {
	t.Parallel()

	var gitea provider.Provider
	for _, p := range all.New("") {
		if p.Name() == constant.ProviderGitea {
			gitea = p
		}
	}
	require.NotNil(t, gitea)

	resource, err := gitea.Resource(directive.Directive{Pairs: []directive.KV{
		{Key: constant.DirectiveRepository, Value: "owner/tool"},
	}})
	require.NoError(t, err)
	require.Equal(t, preCommitCodebergHost+"/owner/tool (tags)", gitea.Describe(resource),
		"the pre-commit inference sends codeberg.org hooks to gitea with no flavor "+
			"key, so gitea's default flavor must still be codeberg")
}

// preCommitCodebergHost is the forge host whose pre-commit hooks rely on gitea's
// default flavor.
const preCommitCodebergHost = "codeberg.org"
