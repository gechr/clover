package vcs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/vcs"
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

func TestRootDir(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	nested := filepath.Join(repo, "deep", "pkg")
	mkmarker(t, repo, ".git", false)
	require.NoError(t, os.MkdirAll(nested, 0o755))

	resolver := vcs.NewResolver()

	tests := []struct {
		name string
		dir  string
		want string
	}{
		// Unlike Root, the directory itself is the search start, not a file whose
		// parent is searched: the repo root resolves to itself, not its parent.
		{name: "repo root resolves to itself", dir: repo, want: repo},
		{name: "nested dir resolves to root", dir: nested, want: repo},
		{name: "dir outside any repo", dir: base, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, resolver.RootDir(tt.dir))
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

// A resolver created after a working-directory change anchors relative paths
// on that directory: the cwd is captured per resolver, not per process, so a
// caller that changed directory before constructing one is never poisoned by
// an earlier capture.
func TestRootRelativePaths(t *testing.T) {
	repo := t.TempDir()
	mkmarker(t, repo, ".git", false)
	t.Chdir(repo)
	// Getwd resolves symlinks (macOS /var -> /private/var), so the expected
	// root comes from it rather than the TempDir literal.
	want, err := os.Getwd()
	require.NoError(t, err)

	resolver := vcs.NewResolver()
	require.Equal(t, want, resolver.RootDir("."))
	require.Equal(t, want, resolver.RootDir(filepath.Join("deep", "nested")))
	require.Equal(t, want, resolver.Root(filepath.Join("deep", "x.yaml")))
}
