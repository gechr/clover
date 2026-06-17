package vcs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/cusp/internal/vcs"
	"github.com/stretchr/testify/require"
)

// mkmarker creates dir with the given VCS marker. A file marker (e.g. a
// submodule's .git) is written as a file; otherwise it is a directory.
func mkmarker(t *testing.T, dir, marker string, asFile bool) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, marker)
	if asFile {
		require.NoError(t, os.WriteFile(path, []byte("gitdir: ../real\n"), 0o644))
		return
	}
	require.NoError(t, os.MkdirAll(path, 0o755))
}

func TestRoot(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	git := filepath.Join(base, "git")
	jj := filepath.Join(base, "jj")           // pure jj working copy, no .git
	hg := filepath.Join(base, "hg")           //nolint:varnamelen // VCS name
	colocated := filepath.Join(base, "coloc") // .jj + .git in one dir
	sub := filepath.Join(git, "vendor", "tool")
	mkmarker(t, git, ".git", false)
	mkmarker(t, jj, ".jj", false)
	mkmarker(t, hg, ".hg", false)
	mkmarker(t, colocated, ".jj", false)
	mkmarker(t, colocated, ".git", false)
	mkmarker(t, sub, ".git", true) // submodule: .git is a file

	resolver := vcs.NewResolver()

	tests := []struct {
		name string
		file string
		want string
	}{
		{name: "git repo", file: filepath.Join(git, "Dockerfile"), want: git},
		{name: "nested file", file: filepath.Join(git, "deep", "x.yaml"), want: git},
		{name: "pure jj repo", file: filepath.Join(jj, "Dockerfile"), want: jj},
		{name: "mercurial repo", file: filepath.Join(hg, "Dockerfile"), want: hg},
		{name: "jj-colocated repo", file: filepath.Join(colocated, "Dockerfile"), want: colocated},
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
	mkmarker(t, repoA, ".git", false)
	mkmarker(t, repoB, ".git", false)

	resolver := vcs.NewResolver()

	// The same id= in two repos namespaces to different roots, so prefixing
	// yields distinct keys - no clash.
	keyA := resolver.Root(filepath.Join(repoA, "Dockerfile")) + "\x00nginx-version"
	keyB := resolver.Root(filepath.Join(repoB, "Dockerfile")) + "\x00nginx-version"
	require.NotEqual(t, keyA, keyB)
}
