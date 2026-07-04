package provider_test

import (
	"context"
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestVersionFloor(t *testing.T) {
	t.Parallel()

	require.Empty(t, provider.VersionFloor(context.Background()), "default is exhaustive")
	require.Equal(
		t,
		"1.2.0",
		provider.VersionFloor(provider.WithVersionFloor(context.Background(), "1.2.0")),
	)
}
