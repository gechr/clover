package provider_test

import (
	"context"
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestQualifier(t *testing.T) {
	t.Parallel()

	require.Empty(t, provider.Qualifier(context.Background()), "default is unfiltered")
	require.Equal(
		t,
		"alpine3.22",
		provider.Qualifier(provider.WithQualifier(context.Background(), "alpine3.22")),
	)
}
