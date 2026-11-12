package mode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWritersReplaceSymlinks(t *testing.T) {
	t.Parallel()

	writers := map[string]func(path string) error{
		"writeFile": func(path string) error { return writeFile(path, []string{"new"}) },
		"writeNew":  func(path string) error { return writeNew(path, []byte("new")) },
	}
	for name, write := range writers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			outside := filepath.Join(t.TempDir(), "outside.yaml")
			require.NoError(t, os.WriteFile(outside, []byte("original"), 0o644))
			link := filepath.Join(dir, "link.yaml")
			require.NoError(t, os.Symlink(outside, link))

			require.NoError(t, write(link))

			got, err := os.ReadFile(outside)
			require.NoError(t, err)
			require.Equal(t, "original", string(got), "written through the symlink")
			got, err = os.ReadFile(link)
			require.NoError(t, err)
			require.Equal(t, "new", string(got))
			info, err := os.Lstat(link)
			require.NoError(t, err)
			require.True(t, info.Mode().IsRegular(), "symlink not replaced")
		})
	}
}
