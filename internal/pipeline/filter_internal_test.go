package pipeline

import (
	"context"
	"testing"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	xslices "github.com/gechr/x/slices"
	"github.com/stretchr/testify/require"
)

// filterStub is a minimal Provider so --enable/--disable validation accepts its
// name; it never resolves anything.
type filterStub struct{ name string }

func (s filterStub) Name() string                      { return s.name }
func (s filterStub) Keys() []provider.Key              { return nil }
func (s filterStub) Describe(provider.Resource) string { return s.name }

func (s filterStub) Resource(directive.Directive) (provider.Resource, error) {
	return struct{}{}, nil
}

func (s filterStub) Discover(context.Context, provider.Resource) ([]model.Candidate, error) {
	return nil, nil
}

// A follower is judged by the producer it follows, and the manual provider always
// survives; the empty filter is a pass-through.
func TestFilterProviders(t *testing.T) {
	provider.Register(filterStub{name: constant.ProviderGithub})
	provider.Register(filterStub{name: constant.ProviderDocker})

	markers := []Marker{
		{Provider: constant.ProviderGithub, ID: "gh"},
		{Provider: constant.ProviderDocker, ID: "img"},
		{Provider: constant.ProviderFollow, From: "gh"},  // follows github
		{Provider: constant.ProviderFollow, From: "img"}, // follows docker
		{Provider: constant.ProviderManual, ID: "man"},   // always kept
	}

	enable, err := provider.NewFilter([]string{constant.ProviderGithub}, nil)
	require.NoError(t, err)

	kept := providers(filterProviders(markers, enable))
	require.Equal(t, []string{
		constant.ProviderGithub, constant.ProviderFollow, constant.ProviderManual,
	}, kept, "github producer, its follower, and manual survive")

	require.Len(t, filterProviders(markers, provider.Filter{}), len(markers),
		"the empty filter keeps every marker")
}

func providers(markers []Marker) []string {
	return xslices.Map(markers, func(m Marker) string { return m.Provider })
}
