package python_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/python"
	"github.com/stretchr/testify/require"
)

func TestDatedMarker(t *testing.T) {
	t.Parallel()

	p := python.New()
	d, ok := any(p).(provider.Dater)
	require.True(t, ok, "python implements provider.Dater")
	d.Dated()
}
