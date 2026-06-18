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
		provider.NoteTruncated(context.Background(), "x") // no sink set
	})

	var got []string
	ctx := provider.WithTruncationSink(context.Background(), func(r string) {
		got = append(got, r)
	})
	provider.NoteTruncated(ctx, "ghcr.io/owner/img")
	require.Equal(t, []string{"ghcr.io/owner/img"}, got)
}
