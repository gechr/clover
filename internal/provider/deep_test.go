package provider_test

import (
	"context"
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestDeep(t *testing.T) {
	t.Parallel()

	require.False(t, provider.Deep(context.Background()), "default is shallow")
	require.True(t, provider.Deep(provider.WithDeep(context.Background(), true)))
	require.False(t, provider.Deep(provider.WithDeep(context.Background(), false)))
}

func TestTruncationSink(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		provider.NoteTruncated(context.Background(), "x", "https://x") // no sink set
	})

	var got []provider.Truncation
	ctx := provider.WithTruncationSink(context.Background(), func(t provider.Truncation) {
		got = append(got, t)
	})
	provider.NoteTruncated(ctx, "ghcr.io/owner/img", "https://ghcr.io/owner/img")
	require.Equal(t, []provider.Truncation{{
		Resource: "ghcr.io/owner/img",
		URL:      "https://ghcr.io/owner/img",
	}}, got)
}
