package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/config"
	"github.com/stretchr/testify/require"
)

// repo creates a directory marked as a repository root carrying a .clover.yaml
// of the given body, and returns its path.
func repo(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".clover.yaml"), []byte(body), 0o644))
	return dir
}

func TestResolverForDirAnchorsOnRepoRoot(t *testing.T) {
	t.Parallel()

	root := repo(t, "paths:\n  exclude:\n    - vendor/**\n")
	nested := filepath.Join(root, "deep", "pkg")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	resolver := config.NewResolver(nil, "", false)

	// A nested directory loads the config at its repository root, not its own dir.
	cfg, err := resolver.ForDir(nested)
	require.NoError(t, err)
	require.Equal(t, []string{"vendor/**"}, cfg.ExcludeGlobs())
}

func TestResolverForDirFallsBackToDirOutsideRepo(t *testing.T) {
	t.Parallel()

	// With no repository marker, the directory itself anchors the config.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, ".clover.yaml"),
		[]byte("paths:\n  exclude:\n    - build/**\n"),
		0o644,
	))

	cfg, err := config.NewResolver(nil, "", false).ForDir(dir)
	require.NoError(t, err)
	require.Equal(t, []string{"build/**"}, cfg.ExcludeGlobs())
}

func TestResolverOverlaysUserConfig(t *testing.T) {
	t.Parallel()

	user := &config.Config{RequiredVersion: new(">=1.0.0")}
	root := repo(t, "paths:\n  exclude:\n    - vendor/**\n")

	cfg, err := config.NewResolver(user, "", false).ForDir(root)
	require.NoError(t, err)
	// The project sets exclude; the user's required-version shows through where the
	// project leaves it unset.
	require.Equal(t, []string{"vendor/**"}, cfg.ExcludeGlobs())
	require.Equal(t, new(">=1.0.0"), cfg.RequiredVersion)
}

func TestResolverExplicitGovernsEveryDir(t *testing.T) {
	t.Parallel()

	explicit := filepath.Join(t.TempDir(), "custom.yaml")
	require.NoError(t, os.WriteFile(explicit, []byte("paths:\n  exclude:\n    - dist/**\n"), 0o644))
	root := repo(t, "paths:\n  exclude:\n    - vendor/**\n")

	resolver := config.NewResolver(nil, explicit, false)
	// The explicit --config file wins over any project config under the path.
	cfg, err := resolver.ForDir(root)
	require.NoError(t, err)
	require.Equal(t, []string{"dist/**"}, cfg.ExcludeGlobs())
}

func TestResolverNoConfigIsNil(t *testing.T) {
	t.Parallel()

	resolver := config.NewResolver(&config.Config{}, "", true)
	cfg, err := resolver.ForDir(repo(t, "paths:\n  exclude:\n    - vendor/**\n"))
	require.NoError(t, err)
	require.Nil(t, cfg)
	require.Nil(t, resolver.Primary())
}

func TestResolverMalformedConfigErrors(t *testing.T) {
	t.Parallel()

	root := repo(t, "required-version: \"not a constraint!!\"\n")
	_, err := config.NewResolver(nil, "", false).ForDir(root)
	require.Error(t, err)
}

func TestResolverPrimarySingleRootHonorsProject(t *testing.T) {
	t.Parallel()

	root := repo(t, "fmt:\n  prune: true\n")
	resolver := config.NewResolver(nil, "", false)
	_, err := resolver.ForDir(root)
	require.NoError(t, err)

	// A scan that touched exactly one repository resolves per-invocation settings
	// from that repository's config.
	require.Equal(t, new(true), resolver.Primary().Prune())
}

func TestResolverPrimaryMultiRootFallsBackToUser(t *testing.T) {
	t.Parallel()

	user := &config.Config{Format: config.Format{Prune: new(false)}}
	resolver := config.NewResolver(user, "", false)
	for _, body := range []string{"fmt:\n  prune: true\n", "fmt:\n  prune: true\n"} {
		_, err := resolver.ForDir(repo(t, body))
		require.NoError(t, err)
	}

	// Two repositories are ambiguous, so per-invocation settings fall back to the
	// user default rather than either project's.
	require.Equal(t, new(false), resolver.Primary().Prune())
}

func TestResolverPrimaryForPathsSingleRootHonorsProject(t *testing.T) {
	t.Parallel()

	root := repo(t, "annotate:\n  write: true\n")
	file := filepath.Join(root, "subdir", "Dockerfile")
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	require.NoError(t, os.WriteFile(file, []byte("FROM redis:7\n"), 0o644))

	cfg, err := config.NewResolver(nil, "", false).PrimaryForPaths([]string{file})
	require.NoError(t, err)
	require.Equal(t, new(true), cfg.AnnotateWrite())
}

func TestResolverPrimaryForPathsMultiRootFallsBackToUser(t *testing.T) {
	t.Parallel()

	user := &config.Config{Annotate: config.Annotate{Write: new(false)}}
	resolver := config.NewResolver(user, "", false)
	cfg, err := resolver.PrimaryForPaths([]string{
		repo(t, "annotate:\n  write: true\n"),
		repo(t, "annotate:\n  write: true\n"),
	})
	require.NoError(t, err)
	require.Equal(t, new(false), cfg.AnnotateWrite())
}

func TestResolverVCSIsShared(t *testing.T) {
	t.Parallel()

	resolver := config.NewResolver(nil, "", false)
	shared := resolver.VCS()
	require.Same(t, shared, resolver.VCS(),
		"one ancestry cache serves the whole run")
}

func TestResolverVCSNilReceiver(t *testing.T) {
	t.Parallel()

	var resolver *config.Resolver
	require.NotNil(t, resolver.VCS(),
		"a nil resolver still yields a usable root resolver")
}
