package manual_test

import (
	"testing"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/manual"
	"github.com/stretchr/testify/require"
)

func TestAnchor(t *testing.T) {
	t.Parallel()

	p := manual.New()

	a, ok := any(p).(provider.Anchorer)
	require.True(t, ok, "manual implements provider.Anchorer")
	a.Anchor()
}

func TestKeys(t *testing.T) {
	t.Parallel()

	require.Nil(t, manual.New().Keys())
}

func TestDescribe(t *testing.T) {
	t.Parallel()

	require.Equal(t, constant.ProviderManual, manual.New().Describe(nil))
}
