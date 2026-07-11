package config_test

import (
	"testing"

	"github.com/gechr/clover/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCommonExcludes(t *testing.T) {
	t.Parallel()

	want := []string{
		"vendor/**",
		"**/testdata/**",
		"**/node_modules/**",
		"dist/**",
		"build/**",
	}

	got := config.CommonExcludes()
	require.Equal(t, want, got)

	got[0] = "mutated"
	require.Equal(t, want, config.CommonExcludes(), "each call returns a fresh copy")
}
