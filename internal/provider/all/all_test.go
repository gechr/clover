package all_test

import (
	"testing"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/all"
	"github.com/stretchr/testify/require"
)

// TestDaterCoversDatedProviders is the drift guard for the cooldown
// short-circuit: a provider must declare the [provider.Dater] capability exactly
// when its listing can carry publication dates for some resource. A provider that
// gains or loses dating without updating its Dater membership fails here, keeping
// the pre-discovery skip honest.
func TestDaterCoversDatedProviders(t *testing.T) {
	dater := map[string]bool{
		constant.ProviderCrates:    true,
		constant.ProviderDocker:    true,
		constant.ProviderGitea:     true,
		constant.ProviderGithub:    true,
		constant.ProviderGitlab:    true,
		constant.ProviderGo:        false,
		constant.ProviderHashicorp: true,
		constant.ProviderHelm:      true,
		constant.ProviderHTTP:      false,
		constant.ProviderManual:    false,
		constant.ProviderNode:      true,
		constant.ProviderNpm:       true,
		constant.ProviderOpentofu:  false,
		constant.ProviderPypi:      true,
		constant.ProviderPython:    true,
		constant.ProviderRust:      true,
		constant.ProviderSwift:     true,
		constant.ProviderTerraform: false,
		constant.ProviderZig:       true,
	}

	for _, p := range all.New("") {
		want, known := dater[p.Name()]
		require.True(t, known, "provider %q missing from the Dater expectation map", p.Name())
		_, isDater := p.(provider.Dater)
		require.Equal(t, want, isDater, "provider %q Dater membership", p.Name())
	}
}

// TestBareMajorers is the drift guard for the scheme guard's bare-pin
// exemption: a provider must declare [provider.BareMajorer] exactly when its
// upstream versions are always semver-shaped and never calendar stamps. A
// provider that wrongly claims it would let a dotted candidate replace a calendar
// tag; one that wrongly omits it leaves a bare major pin (node-version: 20)
// matching no candidate at all.
func TestBareMajorers(t *testing.T) {
	bareMajor := map[string]bool{
		// Toolchains and products with a guaranteed semver scheme.
		constant.ProviderGo:        true,
		constant.ProviderHashicorp: true,
		constant.ProviderNode:      true,
		constant.ProviderPython:    true,
		constant.ProviderRust:      true,
		constant.ProviderSwift:     true,
		constant.ProviderZig:       true,
		// Registries and forges, whose tags and versions may be calendar stamps.
		constant.ProviderCrates:    false,
		constant.ProviderDocker:    false,
		constant.ProviderGitea:     false,
		constant.ProviderGithub:    false,
		constant.ProviderGitlab:    false,
		constant.ProviderHelm:      false,
		constant.ProviderNpm:       false,
		constant.ProviderOpentofu:  false,
		constant.ProviderPypi:      false,
		constant.ProviderTerraform: false,
		// Sources that resolve no listing of their own.
		constant.ProviderHTTP:   false,
		constant.ProviderManual: false,
	}

	for _, p := range all.New("") {
		want, known := bareMajor[p.Name()]
		require.True(t, known, "provider %q missing from the BareMajorer expectation map", p.Name())
		_, is := p.(provider.BareMajorer)
		require.Equal(t, want, is, "provider %q BareMajorer membership", p.Name())
	}
}
