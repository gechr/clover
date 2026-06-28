package scan_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/scan"
	"github.com/stretchr/testify/require"
)

// tree writes a set of files under a fresh temp dir and returns its path.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return root
}

func byPath(files []scan.File) map[string]scan.File {
	m := make(map[string]scan.File, len(files))
	for _, f := range files {
		m[filepath.Base(f.Path)] = f
	}
	return m
}

func TestScanFindsDirectives(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"Dockerfile":       "# clover: provider=github repository=owner/name\nFROM nginx:1.27\n",
		"README.md":        "no directives here\n",
		".git/config":      "# clover: provider=github repository=should/skip\n",
		"sub/deploy.yaml":  "image: redis:7.2 # clover: provider=github repository=redis/redis\n",
		"vendored/bin.dat": "\x00\x01# clover: provider=github repository=bin/ary\x00",
	})

	files, scanned, err := scan.Scan(t.Context(), []string{root})
	require.NoError(t, err)

	got := byPath(files)
	require.Len(t, files, 2, "only the Dockerfile and the yaml carry directives")
	require.Equal(t, 4, scanned,
		"every examined file is counted (incl. the binary and README), but .git is pruned")

	dockerfile := got["Dockerfile"]
	require.Len(t, dockerfile.Found, 1)
	require.Equal(t, 0, dockerfile.Found[0].Line)
	repo, _ := dockerfile.Found[0].Directive.Get("repository")
	require.Equal(t, "owner/name", repo)
	require.Equal(t, "FROM nginx:1.27", dockerfile.Lines[1], "content retained for rewrite")

	yaml := got["deploy.yaml"]
	require.Len(t, yaml.Found, 1)

	require.NotContains(t, got, "config", ".git is skipped")
	require.NotContains(t, got, "bin.dat", "binary files are skipped")
	require.NotContains(t, got, "README.md", "files without directives are dropped")
}

func TestScanRequireDirectiveOffReturnsAllText(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"Dockerfile":       "FROM nginx:1.27\n",
		"README.md":        "no directives here\n",
		"annotated.yaml":   "# clover: provider=auto\nimage: redis:7.2\n",
		".git/config":      "should be pruned\n",
		"vendored/bin.dat": "\x00\x01binary\x00",
	})

	files, _, err := scan.Scan(t.Context(), []string{root}, scan.WithRequireDirective(false))
	require.NoError(t, err)

	got := byPath(files)
	require.Equal(t,
		[]string{"Dockerfile", "README.md", "annotated.yaml"},
		slices.Sorted(maps.Keys(got)),
		"every text file is returned; the binary and .git stay excluded")
	require.Empty(t, got["Dockerfile"].Found, "no directive found, but the lines are available")
	require.Equal(t, "FROM nginx:1.27", got["Dockerfile"].Lines[0])
	require.Len(t, got["annotated.yaml"].Found, 1, "an existing directive is still parsed")
}

func TestScanRequireDirectiveOffStillHonorsIgnoreFile(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"skip.yaml": "# clover:ignore-file\nFROM nginx:1.27\n",
		"keep.yaml": "FROM redis:7.2\n",
	})

	files, _, err := scan.Scan(t.Context(), []string{root}, scan.WithRequireDirective(false))
	require.NoError(t, err)

	require.Equal(t,
		[]string{"keep.yaml"},
		slices.Sorted(maps.Keys(byPath(files))),
		"clover:ignore-file opts the whole file out")
}

func TestScanReportsParseErrors(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"Dockerfile": `# clover: repository="unterminated` + "\n",
	})

	files, _, err := scan.Scan(t.Context(), []string{root})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Len(t, files[0].Errors, 1)
	require.Equal(t, 0, files[0].Errors[0].Line)
}

func TestScanFindsDirectiveAcrossPrefilterBoundary(t *testing.T) {
	t.Parallel()

	const splitAfter = 3 // split "clover:" as "clo" + "ver:"
	pad := strings.Repeat("x", 32<<10-len("# ")-splitAfter)
	root := tree(t, map[string]string{
		"long.yaml": pad + "# clover: provider=github repository=owner/name\nversion: 1.2.0\n",
	})

	files, _, err := scan.Scan(t.Context(), []string{root})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "long.yaml", filepath.Base(files[0].Path))
}

func TestScanIgnoreSeam(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"keep/a.yaml":         "# clover: provider=github repository=keep/a\n",
		"node_modules/b.yaml": "# clover: provider=github repository=skip/b\n",
	})

	ignore := func(path string, _ bool) bool {
		return filepath.Base(filepath.Dir(path)) == "node_modules" ||
			filepath.Base(path) == "node_modules"
	}

	files, _, err := scan.Scan(t.Context(), []string{root}, scan.WithIgnore(ignore))
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, filepath.Join(root, "keep", "a.yaml"), files[0].Path)
}

func TestScanSkipsMissingRoot(t *testing.T) {
	t.Parallel()

	root := tree(
		t,
		map[string]string{"a.yaml": "# clover: provider=github repository=owner/name\n"},
	)
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	files, _, err := scan.Scan(t.Context(), []string{missing, root})
	require.NoError(t, err, "a missing root warns and is skipped, not fatal")
	require.Len(t, files, 1, "the valid root is still scanned")
}

func TestScanDoesNotFollowSymlinks(t *testing.T) {
	t.Parallel()

	// A directive-bearing file lives outside the scanned tree.
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.yaml")
	require.NoError(t, os.WriteFile(
		target,
		[]byte("# clover: provider=github repository=out/of/tree\nversion: 1.0.0\n"),
		0o644,
	))

	// The scanned tree contains a real file plus a symlink to the outside file.
	root := tree(
		t,
		map[string]string{"real.yaml": "# clover: provider=github repository=in/tree\n"},
	)
	require.NoError(t, os.Symlink(target, filepath.Join(root, "link.yaml")))

	files, _, err := scan.Scan(t.Context(), []string{root})
	require.NoError(t, err)

	// Only the real in-tree file is found; the symlink is never resolved, so the
	// out-of-tree target is never read (nor, in the apply phase, written through).
	require.Len(t, files, 1)
	require.Equal(t, "real.yaml", filepath.Base(files[0].Path))
}

func TestScanMergesOverlappingRoots(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"sub/a.yaml": "# clover: provider=github repository=owner/name\n",
	})

	// The same root twice, plus a nested subpath - all coalesce to a single walk.
	files, scanned, err := scan.Scan(t.Context(), []string{root, root, filepath.Join(root, "sub")})
	require.NoError(t, err)
	require.Len(t, files, 1, "the file is found once despite overlapping roots")
	require.Equal(t, 1, scanned, "the file is examined once, not three times")
}
