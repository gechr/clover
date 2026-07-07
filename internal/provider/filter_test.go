package provider_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestNewFilter(t *testing.T) {
	provider.Register(stubProvider{name: constant.ProviderDocker})
	provider.Register(stubProvider{name: constant.ProviderGithub})
	provider.Register(stubProvider{name: constant.ProviderManual})

	_, err := provider.NewFilter([]string{"bogus"}, nil)
	require.EqualError(t, err, fmt.Sprintf(
		"unknown provider %q for --enable, valid providers: %s",
		"bogus", strings.Join(provider.Selectable(), ", "),
	))

	_, err = provider.NewFilter(nil, []string{constant.ProviderManual})
	require.EqualError(t, err, fmt.Sprintf(
		"unknown provider %q for --disable, valid providers: %s",
		constant.ProviderManual, strings.Join(provider.Selectable(), ", "),
	))

	_, err = provider.NewFilter(
		[]string{constant.ProviderGithub},
		[]string{constant.ProviderDocker},
	)
	require.EqualError(t, err, "--enable and --disable cannot be combined")
}

func TestFilterMatch(t *testing.T) {
	provider.Register(stubProvider{name: constant.ProviderDocker})
	provider.Register(stubProvider{name: constant.ProviderGithub})

	enable, err := provider.NewFilter([]string{"github,docker"}, nil)
	require.NoError(t, err)
	require.True(t, enable.Match(constant.ProviderGithub))
	require.True(t, enable.Match(constant.ProviderDocker))
	require.False(t, enable.Match(constant.ProviderNode))
	require.True(t, enable.Match(constant.ProviderManual), "manual always runs")

	disable, err := provider.NewFilter(nil, []string{constant.ProviderDocker})
	require.NoError(t, err)
	require.False(t, disable.Match(constant.ProviderDocker))
	require.True(t, disable.Match(constant.ProviderGithub))
	require.True(t, disable.Match(constant.ProviderManual), "manual always runs")

	var zero provider.Filter
	require.True(t, zero.Empty())
	require.True(t, zero.Match(constant.ProviderNode), "the zero filter matches everything")
}

func TestFilterString(t *testing.T) {
	provider.Register(stubProvider{name: constant.ProviderDocker})
	provider.Register(stubProvider{name: constant.ProviderGithub})

	enable, err := provider.NewFilter([]string{"github", "docker"}, nil)
	require.NoError(t, err)
	require.Equal(t, "only docker, github", enable.String())

	disable, err := provider.NewFilter(nil, []string{"docker"})
	require.NoError(t, err)
	require.Equal(t, "all except docker", disable.String())

	require.Empty(t, provider.Filter{}.String())
}
