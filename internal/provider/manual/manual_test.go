package manual_test

import (
	"context"
	"testing"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/manual"
	"github.com/stretchr/testify/require"
)

func directiveOf(pairs ...directive.KV) directive.Directive {
	return directive.Directive{Pairs: pairs}
}

func TestNameAndAnchorer(t *testing.T) {
	t.Parallel()

	p := manual.New()
	require.Equal(t, constant.ProviderManual, p.Name())

	var prov provider.Provider = p
	_, ok := prov.(provider.Anchorer)
	require.True(t, ok, "manual implements provider.Anchorer")
}

func TestResourceRequiresID(t *testing.T) {
	t.Parallel()

	p := manual.New()

	_, err := p.Resource(directiveOf())
	require.EqualError(t, err, `manual: "id" is required`)

	res, err := p.Resource(directiveOf(directive.KV{Key: constant.DirectiveID, Value: "nginx"}))
	require.NoError(t, err)
	require.NotNil(t, res)
}

func TestDiscoverIsInert(t *testing.T) {
	t.Parallel()

	p := manual.New()
	candidates, err := p.Discover(context.Background(), nil)
	require.NoError(t, err)
	require.Nil(t, candidates)
}
