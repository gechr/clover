package github_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/github"
	"github.com/stretchr/testify/require"
)

func TestNameMarker(t *testing.T) {
	t.Parallel()

	require.Equal(t, "github", github.New().Name())
}

func TestDatedMarker(t *testing.T) {
	t.Parallel()

	p := github.New()
	d, ok := any(p).(provider.Dater)
	require.True(t, ok, "github implements provider.Dater")
	d.Dated()
}
