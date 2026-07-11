package scan_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gechr/clover/internal/scan"
	"github.com/stretchr/testify/require"
)

// writeTree writes files under a fresh temp dir and returns its path.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return root
}

func basenames(files []scan.File) []string {
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = filepath.Base(f.Path)
	}
	return names
}

func TestWithProgress(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"a.yaml":     "# clover: provider=github repository=a/a\n",
		"b.yaml":     "# clover: provider=github repository=b/b\n",
		"plain.txt":  "no directive here\n",
		"sub/c.yaml": "# clover: provider=github repository=c/c\n",
	})

	var highest atomic.Int64
	progress := func(scanned int) {
		for {
			cur := highest.Load()
			if int64(scanned) <= cur || highest.CompareAndSwap(cur, int64(scanned)) {
				break
			}
		}
	}

	_, scanned, err := scan.Scan(t.Context(), []string{root}, scan.WithProgress(progress))
	require.NoError(t, err)
	require.Equal(t, int64(scanned), highest.Load(),
		"the final progress count equals the number of files examined")
}

func TestWithMaxSize(t *testing.T) {
	t.Parallel()

	const directive = "# clover: provider=github repository=a/b\n"
	root := writeTree(t, map[string]string{
		"keep.yaml": directive,
		"big.yaml":  directive + strings.Repeat("x", 100),
	})

	files, _, err := scan.Scan(t.Context(), []string{root},
		scan.WithMaxSize(int64(len(directive))))
	require.NoError(t, err)
	require.Equal(t, []string{"keep.yaml"}, basenames(files),
		"a file at the size boundary is kept, one over it is skipped")
}

func TestWithWorkers(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"a.yaml":     "# clover: provider=github repository=a/a\n",
		"sub/b.yaml": "# clover: provider=github repository=b/b\n",
	}
	root := writeTree(t, files)

	base, _, err := scan.Scan(t.Context(), []string{root})
	require.NoError(t, err)

	serial, _, err := scan.Scan(t.Context(), []string{root}, scan.WithWorkers(1))
	require.NoError(t, err)

	require.Equal(t, basenames(base), basenames(serial),
		"a single worker returns the same files as the default pool")
	require.Len(t, serial, len(files))
}
