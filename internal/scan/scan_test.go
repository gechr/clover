package scan_test

import (
	"os"
	"path/filepath"
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

	files, err := scan.Scan(t.Context(), []string{root})
	require.NoError(t, err)

	got := byPath(files)
	require.Len(t, files, 2, "only the Dockerfile and the yaml carry directives")

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

func TestScanReportsParseErrors(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"Dockerfile": `# clover: repository="unterminated` + "\n",
	})

	files, err := scan.Scan(t.Context(), []string{root})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Len(t, files[0].Errors, 1)
	require.Equal(t, 0, files[0].Errors[0].Line)
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

	files, err := scan.Scan(t.Context(), []string{root}, scan.WithIgnore(ignore))
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Contains(t, files[0].Path, filepath.Join("keep", "a.yaml"))
}
