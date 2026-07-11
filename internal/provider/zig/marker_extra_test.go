package zig_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/zig"
	"github.com/stretchr/testify/require"
)

func TestDatedMarker(t *testing.T) {
	t.Parallel()

	p := zig.New()
	d, ok := any(p).(provider.Dater)
	require.True(t, ok, "zig implements provider.Dater")
	d.Dated()
}
