package docker_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/docker"
	"github.com/stretchr/testify/require"
)

func TestDatedMarker(t *testing.T) {
	t.Parallel()

	p := docker.New(anon())
	d, ok := any(p).(provider.Dater)
	require.True(t, ok, "docker implements provider.Dater")
	d.Dated()
}
