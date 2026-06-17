package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/config"
	"github.com/stretchr/testify/require"
)

func TestStarterHasSchemaModeline(t *testing.T) {
	out := string(config.Starter("", config.DefaultExcludes()))
	require.True(t,
		strings.HasPrefix(out, "# yaml-language-server: $schema=https://"),
		"starter opens with a schema modeline for editor validation, got:\n%s", out,
	)
}

// TestStarterRoundTrips is the load-bearing test: whatever Starter emits must
// parse and validate through Load, across the matrix of present/absent
// required-version and excludes.
func TestStarterRoundTrips(t *testing.T) {
	tests := []struct {
		name         string
		constraint   string
		excludes     []string
		wantVersion  string
		wantExcludes []string
	}{
		{
			name:         "constraint and excludes",
			constraint:   ">=0.2.0",
			excludes:     []string{"vendor/**", "**/node_modules/**"},
			wantVersion:  ">=0.2.0",
			wantExcludes: []string{"vendor/**", "**/node_modules/**"},
		},
		{
			name:         "no constraint, default excludes",
			constraint:   "",
			excludes:     config.DefaultExcludes(),
			wantVersion:  "",
			wantExcludes: config.DefaultExcludes(),
		},
		{
			name:         "no constraint, no excludes",
			constraint:   "",
			excludes:     nil,
			wantVersion:  "",
			wantExcludes: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".clover.yaml")
			require.NoError(
				t,
				os.WriteFile(path, config.Starter(tc.constraint, tc.excludes), 0o644),
			)

			cfg, err := config.Load(dir, "")
			require.NoError(t, err, "generated config must be schema-valid")
			require.NotNil(t, cfg)
			require.Equal(t, tc.wantVersion, cfg.RequiredVersion)
			require.Equal(t, tc.wantExcludes, cfg.ExcludeGlobs())
		})
	}
}
