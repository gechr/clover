package config_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gechr/clover/internal/config"
	"github.com/stretchr/testify/require"
)

// loadRules parses a run.rules config body through the full load path, so every
// test exercises the schema and value validation a real config goes through.
func loadRules(t *testing.T, body string) *config.Config {
	t.Helper()
	cfg, err := config.Load(writeConfig(t, ".clover.yaml", body), "")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	return cfg
}

func TestRulesScopeByProvider(t *testing.T) {
	t.Parallel()

	cfg := loadRules(t, `
run:
  prerelease: false
  rules:
    - providers: [docker]
      prerelease: true
`)

	docker := config.Marker{Path: "compose.yaml", Provider: "docker"}
	github := config.Marker{Path: "compose.yaml", Provider: "github"}
	require.Equal(t, new(true), cfg.PrereleaseFor(docker))
	require.Equal(
		t,
		new(false),
		cfg.PrereleaseFor(github),
		"unmatched markers fall back to run defaults",
	)
}

func TestRulesScopeByPathRootRelative(t *testing.T) {
	t.Parallel()

	cfg := loadRules(t, `
run:
  rules:
    - paths: [vendor/**]
      downgrade: true
`)

	require.Equal(t,
		new(true),
		cfg.DowngradeFor(config.Marker{Path: "vendor/lib/app.txt", Provider: "github"}),
	)
	require.Nil(t, cfg.DowngradeFor(config.Marker{Path: "src/app.txt", Provider: "github"}))
}

func TestRulesTagsRequireEveryTag(t *testing.T) {
	t.Parallel()

	cfg := loadRules(t, `
run:
  rules:
    - tags: [prod, eu]
      verify: true
`)

	both := config.Marker{Path: "a.txt", Provider: "github", Tags: []string{"eu", "prod", "extra"}}
	one := config.Marker{Path: "a.txt", Provider: "github", Tags: []string{"prod"}}
	require.Equal(t, new(true), cfg.VerifyFor(both))
	require.Nil(t, cfg.VerifyFor(one), "a marker missing one of the rule's tags is not selected")
}

func TestRulesSelectorsCombineWithAnd(t *testing.T) {
	t.Parallel()

	cfg := loadRules(t, `
run:
  rules:
    - paths: [images/**]
      providers: [docker]
      force: true
`)

	require.Equal(t,
		new(true),
		cfg.ForceFor(config.Marker{Path: "images/app.yaml", Provider: "docker"}),
	)
	require.Nil(t,
		cfg.ForceFor(config.Marker{Path: "images/app.yaml", Provider: "helm"}),
		"every set selector must accept the marker",
	)
	require.Nil(t, cfg.ForceFor(config.Marker{Path: "charts/app.yaml", Provider: "docker"}))
}

// TestRulesFirstMatchWinsPerSetting confirms settings resolve independently:
// each one comes from the first matching rule that sets it, so orthogonal rules
// compose rather than shadow each other.
func TestRulesFirstMatchWinsPerSetting(t *testing.T) {
	t.Parallel()

	cfg := loadRules(t, `
run:
  rules:
    - providers: [docker]
      cooldown: 2w
    - paths: [vendor/**]
      cooldown: 1w
      prerelease: true
`)

	vendoredDocker := config.Marker{Path: "vendor/compose.yaml", Provider: "docker"}
	require.Equal(t, 14*24*time.Hour, cfg.CooldownFor(vendoredDocker), "first matching rule wins")
	require.Equal(t,
		new(true),
		cfg.PrereleaseFor(vendoredDocker),
		"a setting the first rule leaves unset falls through to the next match",
	)
}

func TestRulesCooldownFallsBackToRunDefault(t *testing.T) {
	t.Parallel()

	cfg := loadRules(t, `
run:
  cooldown: 3d
  rules:
    - providers: [docker]
      cooldown: 1w
`)

	require.Equal(
		t,
		7*24*time.Hour,
		cfg.CooldownFor(config.Marker{Path: "a.yaml", Provider: "docker"}),
	)
	require.Equal(
		t,
		3*24*time.Hour,
		cfg.CooldownFor(config.Marker{Path: "a.tf", Provider: "terraform"}),
	)
}

func TestRulesMatchCaseInsensitively(t *testing.T) {
	t.Parallel()

	cfg := loadRules(t, `
run:
  rules:
    - providers: [Docker]
      tags: [Prod]
      deep: true
`)

	m := config.Marker{Path: "a.yaml", Provider: "docker", Tags: []string{"prod"}}
	require.Equal(t, new(true), cfg.DeepFor(m))
}

func TestRulesNilConfigIsInert(t *testing.T) {
	t.Parallel()

	var cfg *config.Config
	m := config.Marker{Path: "a.txt", Provider: "github"}
	require.Nil(t, cfg.VerifyFor(m))
	require.Nil(t, cfg.PrereleaseFor(m))
	require.Nil(t, cfg.DowngradeFor(m))
	require.Nil(t, cfg.DeepFor(m))
	require.Nil(t, cfg.ForceFor(m))
	require.Zero(t, cfg.CooldownFor(m))
}

func TestRulesValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "no selector",
			body: "run:\n  rules:\n    - prerelease: true\n",
			want: `"run.rules[0]" needs at least one of "paths", "providers", or "tags"`,
		},
		{
			name: "no settings",
			body: "run:\n  rules:\n    - providers: [docker]\n",
			want: `"run.rules[0]" sets no defaults`,
		},
		{
			name: "bad cooldown",
			body: "run:\n  rules:\n    - providers: [docker]\n      cooldown: soon\n",
			want: `"run.rules[0].cooldown" must be a duration like 2w3d, got "soon"`,
		},
		{
			name: "bad glob",
			body: "run:\n  rules:\n    - paths: [\"[\"]\n      prerelease: true\n",
			want: `invalid "run.rules[0].paths" glob "["`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := writeConfig(t, ".clover.yaml", tc.body)
			_, err := config.Load(dir, "")
			require.EqualError(t, err, filepath.Join(dir, ".clover.yaml")+": "+tc.want)
		})
	}
}

// TestRulesMergeReplacesWholesale confirms a project's run.rules replaces the
// user layer's list outright, like paths.exclude, rather than concatenating.
func TestRulesMergeReplacesWholesale(t *testing.T) {
	t.Parallel()

	user := loadRules(t, "run:\n  rules:\n    - providers: [docker]\n      prerelease: true\n")
	project := loadRules(t, "run:\n  rules:\n    - providers: [helm]\n      deep: true\n")

	merged := config.Merge(user, project)
	docker := config.Marker{Path: "a.yaml", Provider: "docker"}
	helm := config.Marker{Path: "a.yaml", Provider: "helm"}
	require.Nil(t, merged.PrereleaseFor(docker), "the user rule list is replaced")
	require.Equal(t, new(true), merged.DeepFor(helm))

	kept := config.Merge(user, loadRules(t, "run:\n  deep: true\n"))
	require.Equal(
		t,
		new(true),
		kept.PrereleaseFor(docker),
		"a project without rules keeps the user's",
	)
}
