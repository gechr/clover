package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/config"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	return dir
}

func TestLoad(t *testing.T) {
	t.Parallel()

	dir := writeConfig(
		t,
		".clover.yaml",
		"required-version: \">=0.1.0\"\npaths:\n  exclude:\n    - vendor/**\n    - \"**/testdata/**\"\n",
	)

	cfg, err := config.Load(dir, "")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, ">=0.1.0", cfg.RequiredVersion)
	require.Equal(t, []string{"vendor/**", "**/testdata/**"}, cfg.ExcludeGlobs())
}

func TestLoadAbsentIsNil(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(t.TempDir(), "")
	require.NoError(t, err)
	require.Nil(t, cfg)
}

// TestLoadEmptyDocument confirms a present but content-free config (only
// comments, so YAML parses to null) is valid and yields a zero Config, since an
// empty config means "all defaults" - and init can legitimately write one.
func TestLoadEmptyDocument(t *testing.T) {
	t.Parallel()

	dir := writeConfig(t, ".clover.yaml", "# just a comment, no settings\n")
	cfg, err := config.Load(dir, "")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Empty(t, cfg.RequiredVersion)
	require.Empty(t, cfg.ExcludeGlobs())
}

func TestLoadYmlExtension(t *testing.T) {
	t.Parallel()

	dir := writeConfig(t, ".clover.yml", "paths:\n  exclude: [build/**]\n")
	cfg, err := config.Load(dir, "")
	require.NoError(t, err)
	require.Equal(t, []string{"build/**"}, cfg.ExcludeGlobs())
}

// TestLoadPrefersYamlOverYml confirms .clover.yaml wins when both extensions are
// present, matching the order read() tries them.
func TestLoadPrefersYamlOverYml(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, ".clover.yaml"), []byte("required-version: \">=1.0.0\"\n"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, ".clover.yml"), []byte("required-version: \">=2.0.0\"\n"), 0o644))

	cfg, err := config.Load(dir, "")
	require.NoError(t, err)
	require.Equal(t, ">=1.0.0", cfg.RequiredVersion, ".clover.yaml takes precedence")
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	t.Parallel()

	dir := writeConfig(t, ".clover.yaml", "nonsense: true\n")
	_, err := config.Load(dir, "")
	require.Error(t, err, "additionalProperties:false rejects unknown keys")
}

func TestLoadRejectsWrongType(t *testing.T) {
	t.Parallel()

	dir := writeConfig(t, ".clover.yaml", "required-version: 12\n") // number, not string
	_, err := config.Load(dir, "")
	require.Error(t, err)
}

func TestLoadExplicitPathMissing(t *testing.T) {
	t.Parallel()

	_, err := config.Load(t.TempDir(), "/no/such/.clover.yaml")
	require.Error(t, err, "an explicitly requested config that is missing is an error")
}

func TestCheckVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		constraint string
		current    string
		wantErr    bool
	}{
		{name: "satisfied", constraint: ">=0.1.0", current: "0.2.0"},
		{name: "violated", constraint: ">=0.2.0", current: "0.1.0", wantErr: true},
		{name: "empty constraint passes", constraint: "", current: "0.1.0"},
		{name: "dev version skips gate", constraint: ">=9.0.0", current: "dev"},
		{
			name:       "bad constraint errors",
			constraint: "not-a-constraint!",
			current:    "0.1.0",
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{RequiredVersion: tc.constraint}
			err := cfg.CheckVersion(tc.current)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNilConfigSafe(t *testing.T) {
	t.Parallel()

	var cfg *config.Config
	require.Nil(t, cfg.ExcludeGlobs())
	require.NoError(t, cfg.CheckVersion("0.1.0"))
}
