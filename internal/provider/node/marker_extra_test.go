package node_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/node"
	"github.com/stretchr/testify/require"
)

func TestDatedMarker(t *testing.T) {
	t.Parallel()

	p := node.New()
	d, ok := any(p).(provider.Dater)
	require.True(t, ok, "node implements provider.Dater")
	d.Dated()
}
