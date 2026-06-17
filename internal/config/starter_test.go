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
	out := string(config.Starter(""))
	require.True(t,
		strings.HasPrefix(out, "# yaml-language-server: $schema=https://"),
		"starter opens with a schema modeline for editor validation, got:\n%s", out,
	)
}

// TestStarterRoundTrips is the load-bearing test: whatever Starter emits must
// parse and validate through Load, for both an active and an omitted
// required-version.
func TestStarterRoundTrips(t *testing.T) {
	tests := []struct {
		name        string
		constraint  string
		wantVersion string
	}{
		{name: "with constraint", constraint: ">=0.2.0", wantVersion: ">=0.2.0"},
		{name: "without constraint", constraint: "", wantVersion: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".clover.yaml")
			require.NoError(t, os.WriteFile(path, config.Starter(tc.constraint), 0o644))

			cfg, err := config.Load(dir, "")
			require.NoError(t, err, "generated config must be schema-valid")
			require.NotNil(t, cfg)
			require.Equal(t, tc.wantVersion, cfg.RequiredVersion)
			require.NotEmpty(t, cfg.ExcludeGlobs(), "starter seeds default excludes")
		})
	}
}
