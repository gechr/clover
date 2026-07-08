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
		constant.ProviderOpentofu:  false,
		constant.ProviderPython:    true,
		constant.ProviderTerraform: false,
		constant.ProviderZig:       true,
	}

	for _, p := range all.New() {
		want, known := dater[p.Name()]
		require.True(t, known, "provider %q missing from the Dater expectation map", p.Name())
		_, isDater := p.(provider.Dater)
		require.Equal(t, want, isDater, "provider %q Dater membership", p.Name())
	}
}
