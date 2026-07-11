package pypi_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/pypi"
	"github.com/stretchr/testify/require"
)

func TestDatedMarker(t *testing.T) {
	t.Parallel()

	p := pypi.New()
	d, ok := any(p).(provider.Dater)
	require.True(t, ok, "pypi implements provider.Dater")
	d.Dated()
}
