package pipeline_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/scan"
	xslices "github.com/gechr/x/slices"
	"github.com/stretchr/testify/require"
)

// A scan honours .gitignore by default: a directive in a gitignored directory is
// pruned, while a sibling outside it is scanned. Regression test for the wiring
// that previously passed an empty WithFiles, clobbering the .gitignore default.
func TestScanHonorsGitignoreByDefault(t *testing.T) {
	dir := writeRepo(t, "",
		map[string]string{
			".gitignore":        "ignored/\n",
			"keep.yaml":         "# clover: provider=github repository=keep/repo\nversion: 1.0.0\n",
			"ignored/drop.yaml": "# clover: provider=github repository=drop/repo\nversion: 1.0.0\n",
		})
	t.Chdir(dir)

	files, _, err := pipeline.Scan(
		context.Background(),
		[]string{"."},
		pipeline.WithConfig(config.NewResolver(nil, "", false)),
	)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "keep.yaml", filepath.Base(files[0].Path))
}

// WithNoIgnore scans a file .gitignore would otherwise exclude.
func TestScanNoIgnoreScansGitignored(t *testing.T) {
	dir := writeRepo(t, "",
		map[string]string{
			".gitignore":        "ignored/\n",
			"keep.yaml":         "# clover: provider=github repository=keep/repo\nversion: 1.0.0\n",
			"ignored/drop.yaml": "# clover: provider=github repository=drop/repo\nversion: 1.0.0\n",
		})
	t.Chdir(dir)

	files, _, err := pipeline.Scan(
		context.Background(),
		[]string{"."},
		pipeline.WithConfig(config.NewResolver(nil, "", false)),
		pipeline.WithNoIgnore(true),
	)
	require.NoError(t, err)

	bases := xslices.Map(files, func(f scan.File) string { return filepath.Base(f.Path) })
	require.ElementsMatch(t, []string{"keep.yaml", "drop.yaml"}, bases)
}

// WithNoIgnore still excludes VCS directories: a directive hidden inside .git is
// never scanned, even with ignoring disabled.
func TestScanNoIgnoreStillExcludesVCS(t *testing.T) {
	dir := write(t, map[string]string{
		".git/HEAD":        "ref: refs/heads/main\n",
		".git/hooked.yaml": "# clover: provider=github repository=vcs/repo\nversion: 1.0.0\n",
		"keep.yaml":        "# clover: provider=github repository=keep/repo\nversion: 1.0.0\n",
	})
	t.Chdir(dir)

	files, _, err := pipeline.Scan(
		context.Background(),
		[]string{"."},
		pipeline.WithConfig(config.NewResolver(nil, "", false)),
		pipeline.WithNoIgnore(true),
	)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "keep.yaml", filepath.Base(files[0].Path))
}
