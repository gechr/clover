package config_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/output"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
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
	require.Equal(t, new(">=0.1.0"), cfg.RequiredVersion)
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
	require.Equal(t, new(">=1.0.0"), cfg.RequiredVersion, ".clover.yaml takes precedence")
}

// TestLoadWarnsUnknownKey confirms an unknown key no longer fails the load: it
// is warned about (see unknownField) while the known keys still decode, so a
// config written for a newer clover stays usable on an older one.
func TestLoadWarnsUnknownKey(t *testing.T) {
	t.Parallel()

	dir := writeConfig(t, ".clover.yaml", "nonsense: true\nrequired-version: \">=0.1.0\"\n")
	cfg, err := config.Load(dir, "")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(
		t,
		new(">=0.1.0"),
		cfg.RequiredVersion,
		"known keys decode despite the unknown one",
	)
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
			cfg := &config.Config{RequiredVersion: &tc.constraint}
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
	require.Nil(t, cfg.Verify())
	require.Nil(t, cfg.Prerelease())
	require.Nil(t, cfg.Downgrade())
	require.Nil(t, cfg.Deep())
	require.Nil(t, cfg.Prune())
	require.Equal(t, output.Text, cfg.RunOutput(nil))
	require.Equal(t, output.Text, cfg.LintOutput(nil))
}

// TestLoadUser confirms the user config is read from
// $XDG_CONFIG_HOME/clover/config.yaml. The env var is set (not parallel) so the
// XDG config dir resolves to a temp tree.
func TestLoadUser(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "clover")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "config.yaml"),
		[]byte("paths:\n  exclude: [vendor/**]\n"), 0o644))
	t.Setenv("XDG_CONFIG_HOME", home)

	cfg, err := config.LoadUser()
	require.NoError(t, err)
	require.Equal(t, []string{"vendor/**"}, cfg.ExcludeGlobs())
}

// TestLoadUserAbsentIsNil confirms a missing user config is nil, not an error,
// so users without one run normally.
func TestLoadUserAbsentIsNil(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg, err := config.LoadUser()
	require.NoError(t, err)
	require.Nil(t, cfg)
}

func TestMerge(t *testing.T) {
	t.Parallel()

	base := &config.Config{
		RequiredVersion: new(">=0.1.0"),
		Paths:           config.Paths{Exclude: []string{"vendor/**"}},
	}

	tests := []struct {
		name    string
		user    *config.Config
		project *config.Config
		want    *config.Config
	}{
		{
			name: "nil user returns project",
			user: nil, project: base, want: base,
		},
		{
			name: "nil project returns user",
			user: base, project: nil, want: base,
		},
		{
			name:    "project wins field by field",
			user:    base,
			project: &config.Config{RequiredVersion: new(">=0.2.0")},
			want: &config.Config{
				RequiredVersion: new(">=0.2.0"),
				Paths:           config.Paths{Exclude: []string{"vendor/**"}},
			},
		},
		{
			name:    "unset project fields fall back to user",
			user:    base,
			project: &config.Config{Paths: config.Paths{Exclude: []string{"build/**"}}},
			want: &config.Config{
				RequiredVersion: new(">=0.1.0"),
				Paths:           config.Paths{Exclude: []string{"build/**"}},
			},
		},
		{
			name:    "project clears user to empty",
			user:    base,
			project: &config.Config{Paths: config.Paths{Exclude: []string{}}},
			want: &config.Config{
				RequiredVersion: new(">=0.1.0"),
				Paths:           config.Paths{Exclude: []string{}},
			},
		},
		{
			name:    "project clears user required-version",
			user:    base,
			project: &config.Config{RequiredVersion: new("")},
			want: &config.Config{
				RequiredVersion: new(""),
				Paths:           config.Paths{Exclude: []string{"vendor/**"}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, config.Merge(tc.user, tc.project))
		})
	}
}

// TestReferenceConfigValid guards that the documented .clover.reference.yaml in
// the repo root stays in sync with the schema and the struct: it must load
// cleanly, and - since Load only warns on unknown keys - a strict decode must
// find no key the struct does not know, so a typo in the reference is caught.
func TestReferenceConfigValid(t *testing.T) {
	t.Parallel()

	const path = "../../.clover.reference.yaml"
	cfg, err := config.Load("", path)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	require.NoError(t, dec.Decode(new(config.Config)), "every documented key must be known")
}

// TestLoadCommandSettings parses the per-command blocks and confirms the typed
// accessors read them back, including the command-over-global output override.
func TestLoadCommandSettings(t *testing.T) {
	t.Parallel()

	body := "global:\n  output: wide\n" +
		"run:\n  verify: true\n  prerelease: false\n  deep: true\n  output: github\n" +
		"lint:\n  output: text\n" +
		"fmt:\n  prune: true\n" +
		"annotate:\n  write: true\n  check: false\n"
	dir := writeConfig(t, ".clover.yaml", body)

	cfg, err := config.Load(dir, "")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.True(t, *cfg.Verify())
	require.False(t, *cfg.Prerelease())
	require.Nil(t, cfg.Downgrade(), "an absent key stays nil, distinct from false")
	require.True(t, *cfg.Deep())
	require.True(t, *cfg.Prune())
	require.True(t, *cfg.AnnotateWrite())
	require.False(t, *cfg.AnnotateCheck())
	require.Equal(t, output.GitHub, cfg.RunOutput(nil), "run.output overrides global.output")
	require.Equal(t, output.Text, cfg.LintOutput(nil), "lint.output overrides global.output")
}

func TestLoadRejectsConflictingAnnotateModes(t *testing.T) {
	t.Parallel()

	dir := writeConfig(t, ".clover.yaml", "annotate:\n  write: true\n  check: true\n")
	_, err := config.Load(dir, "")
	require.Error(t, err, "annotate.write and annotate.check cannot both be true")
}

func TestLoadRejectsInvalidOutput(t *testing.T) {
	t.Parallel()

	dir := writeConfig(t, ".clover.yaml", "run:\n  output: fancy\n")
	_, err := config.Load(dir, "")
	require.Error(t, err, "the output enum is validated by the schema")
}

func TestLoadRejectsEmptyExclude(t *testing.T) {
	t.Parallel()

	dir := writeConfig(t, ".clover.yaml", "paths:\n  exclude: [\"\"]\n")
	_, err := config.Load(dir, "")
	require.Error(t, err, "an empty glob is rejected by the schema (minLength)")
}

func TestLoadRejectsBadRequiredVersion(t *testing.T) {
	t.Parallel()

	dir := writeConfig(t, ".clover.yaml", "required-version: \"not-a-constraint!\"\n")
	_, err := config.Load(dir, "")
	require.Error(t, err, "an unparseable constraint is caught at load")
}

// TestOutputPrecedence exercises the CLI > command > global > text chain that
// RunOutput and LintOutput apply.
func TestOutputPrecedence(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Global: config.Global{Output: new(output.Wide)}}

	require.Equal(
		t,
		output.GitHub,
		cfg.RunOutput(new(output.GitHub)),
		"CLI wins over global",
	)
	require.Equal(t, output.Wide, cfg.RunOutput(nil), "global applies when CLI absent")
	require.Equal(t, output.Wide, cfg.LintOutput(nil), "global is shared across commands")

	cfg.Run.Output = new(output.GitHub)
	require.Equal(t, output.GitHub, cfg.RunOutput(nil), "run.output overrides global")
	require.Equal(t, output.Wide, cfg.LintOutput(nil), "lint still sees global")

	var nilCfg *config.Config
	require.Equal(t, output.Text, nilCfg.RunOutput(nil), "no config defaults to text")
	require.Equal(
		t,
		output.GitHub,
		nilCfg.RunOutput(new(output.GitHub)),
		"CLI works without a config",
	)
}

// TestMergeCommandSettings confirms the deep merge overrides set leaves while
// preserving unset ones across the nested command blocks.
func TestMergeCommandSettings(t *testing.T) {
	t.Parallel()

	user := &config.Config{
		Global:   config.Global{Output: new(output.Wide)},
		Run:      config.Run{Verify: new(true), Deep: new(true)},
		Annotate: config.Annotate{Write: new(true), Check: new(false)},
	}
	project := &config.Config{
		Run:      config.Run{Verify: new(false)},
		Lint:     config.Lint{Output: new(output.GitHub)},
		Annotate: config.Annotate{Write: new(false)},
	}

	got := config.Merge(user, project)
	require.False(t, *got.Verify(), "project run.verify overrides user")
	require.True(t, *got.Deep(), "unset project run.deep falls back to user")
	require.False(t, *got.AnnotateWrite(), "project annotate.write overrides user")
	require.False(t, *got.AnnotateCheck(), "unset project annotate.check falls back to user")
	require.Equal(t, output.Wide, got.RunOutput(nil), "user global.output preserved")
	require.Equal(t, output.GitHub, got.LintOutput(nil), "project lint.output applied")
}
