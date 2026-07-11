package config_test

import (
	"testing"

	"github.com/gechr/clover/internal/config"
	"github.com/stretchr/testify/require"
)

func TestResolver_Err(t *testing.T) {
	t.Parallel()

	t.Run("nil resolver", func(t *testing.T) {
		t.Parallel()
		var resolver *config.Resolver
		require.NoError(t, resolver.Err())
	})

	t.Run("fresh resolver", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, config.NewResolver(nil, "", false).Err())
	})

	t.Run("malformed config records error", func(t *testing.T) {
		t.Parallel()
		root := repo(t, "required-version: \"not a constraint!!\"\n")
		resolver := config.NewResolver(nil, "", false)
		_, err := resolver.ForDir(root)
		require.Error(t, err)
		require.Equal(t, err, resolver.Err(), "the load error is recorded for later surfacing")
	})
}
