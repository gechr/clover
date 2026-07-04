package provider_test

import (
	"context"
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestTagPrefix(t *testing.T) {
	t.Parallel()

	require.Empty(t, provider.TagPrefix(context.Background()), "default is unfiltered")
	require.Equal(
		t,
		"api/",
		provider.TagPrefix(provider.WithTagPrefix(context.Background(), "api/")),
	)
}
