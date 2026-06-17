package repo_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/cusp/internal/repo"
	"github.com/stretchr/testify/require"
)

// mkrepo creates a directory with a .git marker (dir or file) under root.
func mkrepo(t *testing.T, dir string, asFile bool) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	git := filepath.Join(dir, ".git")
	if asFile {
		require.NoError(t, os.WriteFile(git, []byte("gitdir: ../real\n"), 0o644))
		return
	}
	require.NoError(t, os.MkdirAll(git, 0o755))
}

func TestRoot(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	repoA := filepath.Join(base, "a")
	repoB := filepath.Join(base, "b")
	sub := filepath.Join(repoA, "vendor", "tool") // submodule: .git is a file
	mkrepo(t, repoA, false)
	mkrepo(t, repoB, false)
	mkrepo(t, sub, true)

	resolver := repo.NewResolver()

	tests := []struct {
		name string
		file string
		want string
	}{
		{name: "file in repo A", file: filepath.Join(repoA, "Dockerfile"), want: repoA},
		{name: "nested file in repo A", file: filepath.Join(repoA, "deep", "x.yaml"), want: repoA},
		{name: "file in repo B", file: filepath.Join(repoB, "Dockerfile"), want: repoB},
		{name: "nearest repo wins (submodule)", file: filepath.Join(sub, "go.mod"), want: sub},
		{name: "outside any repo", file: filepath.Join(base, "loose.txt"), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, resolver.Root(tt.file))
		})
	}
}

func TestSameIDDistinctReposDoNotClash(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	repoA := filepath.Join(base, "a")
	repoB := filepath.Join(base, "b")
	mkrepo(t, repoA, false)
	mkrepo(t, repoB, false)

	resolver := repo.NewResolver()

	// The same id= in two repos namespaces to different roots, so prefixing
	// yields distinct keys - no clash.
	keyA := resolver.Root(filepath.Join(repoA, "Dockerfile")) + "\x00nginx-version"
	keyB := resolver.Root(filepath.Join(repoB, "Dockerfile")) + "\x00nginx-version"
	require.NotEqual(t, keyA, keyB)
}
