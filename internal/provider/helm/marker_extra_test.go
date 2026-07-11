package helm_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/helm"
	"github.com/stretchr/testify/require"
)

func TestDatedMarker(t *testing.T) {
	t.Parallel()

	p := helm.New(anon())
	d, ok := any(p).(provider.Dater)
	require.True(t, ok, "helm implements provider.Dater")
	d.Dated()
}
