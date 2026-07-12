package provider_test

import (
	"context"
	"image/color"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// stubProvider is a minimal Provider for exercising the registry.
type stubProvider struct {
	name string
}

func (s stubProvider) Name() string { return s.name }

func (s stubProvider) Color(bool) color.Color            { return color.Gray{Y: 0x80} }
func (s stubProvider) Keys() []provider.Key              { return nil }
func (s stubProvider) Describe(provider.Resource) string { return s.name }

func (s stubProvider) Resource(directive.Directive) (provider.Resource, error) {
	return struct{}{}, nil
}

func (s stubProvider) Discover(context.Context, provider.Resource) ([]model.Candidate, error) {
	return nil, nil
}

func TestRegistry(t *testing.T) {
	t.Parallel()

	provider.Register(stubProvider{name: "stub"})

	got, ok := provider.Get("stub")
	require.True(t, ok)
	require.Equal(t, "stub", got.Name())

	_, ok = provider.Get("missing")
	require.False(t, ok)

	require.Contains(t, provider.Names(), "stub")
}
