package ignore_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/cusp/internal/ignore"
	"github.com/gechr/cusp/internal/vcs"
	"github.com/stretchr/testify/require"
)

// repo writes a .git marker plus the given .gitignore files (keyed by directory
// relative to root, "" = root) under a fresh temp dir, returning a matcher and
// the root.
func repo(t *testing.T, ignores map[string]string) (*ignore.Matcher, string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	for dir, content := range ignores {
		full := filepath.Join(root, dir)
		require.NoError(t, os.MkdirAll(full, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(full, ".gitignore"), []byte(content), 0o644))
	}
	return ignore.New(vcs.NewResolver()), root
}

func TestIgnorePatterns(t *testing.T) {
	t.Parallel()

	const content = `# a comment

*.log
build/
/dist
node_modules
**/*.tmp
docs/*.md
!keep.log
`
	matcher, root := repo(t, map[string]string{"": content})

	tests := []struct {
		name  string
		rel   string
		isDir bool
		want  bool
	}{
		{name: "basename glob any depth", rel: "app.log", want: true},
		{name: "basename glob nested", rel: "sub/app.log", want: true},
		{name: "negation overrides earlier match", rel: "keep.log", want: false},
		{name: "dir-only matches dir", rel: "build", isDir: true, want: true},
		{name: "dir-only ignores file", rel: "build", isDir: false, want: false},
		{name: "anchored matches at root", rel: "dist", want: true},
		{name: "anchored not nested", rel: "sub/dist", want: false},
		{name: "name matches dir any depth", rel: "node_modules", isDir: true, want: true},
		{name: "name nested", rel: "a/b/node_modules", isDir: true, want: true},
		{name: "doublestar zero dirs", rel: "foo.tmp", want: true},
		{name: "doublestar many dirs", rel: "a/b/foo.tmp", want: true},
		{name: "path glob single level", rel: "docs/readme.md", want: true},
		{name: "path glob does not cross slash", rel: "docs/sub/readme.md", want: false},
		{name: "unmatched", rel: "src/main.go", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := matcher.Ignore(filepath.Join(root, tt.rel), tt.isDir)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNestedGitignoreNegation(t *testing.T) {
	t.Parallel()

	matcher, root := repo(t, map[string]string{
		"":    "*.log\n",
		"sub": "!important.log\n",
	})

	require.True(t, matcher.Ignore(filepath.Join(root, "sub", "other.log"), false),
		"root *.log applies in sub")
	require.False(t, matcher.Ignore(filepath.Join(root, "sub", "important.log"), false),
		"deeper negation wins")
}

func TestMultipleIgnoreFilesOverride(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n"), 0o644))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(root, ".cuspignore"), []byte("!keep.log\n"), 0o644),
	)

	// .cuspignore is listed last, so it overrides .gitignore - the seam a real
	// .cuspignore will use.
	matcher := ignore.New(vcs.NewResolver(), ignore.WithFiles(".gitignore", ".cuspignore"))

	require.True(t, matcher.Ignore(filepath.Join(root, "app.log"), false))
	require.False(t, matcher.Ignore(filepath.Join(root, "keep.log"), false),
		".cuspignore negation overrides .gitignore")
}

func TestOutsideRepoNeverIgnored(t *testing.T) {
	t.Parallel()

	base := t.TempDir() // no .git marker
	require.NoError(t, os.WriteFile(filepath.Join(base, ".gitignore"), []byte("*.log\n"), 0o644))

	matcher := ignore.New(vcs.NewResolver())
	require.False(t, matcher.Ignore(filepath.Join(base, "app.log"), false))
}
