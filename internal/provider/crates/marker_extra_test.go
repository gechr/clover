package crates_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/crates"
	"github.com/stretchr/testify/require"
)

func TestDatedMarker(t *testing.T) {
	t.Parallel()

	p := crates.New()
	d, ok := any(p).(provider.Dater)
	require.True(t, ok, "crates implements provider.Dater")
	d.Dated()
}
