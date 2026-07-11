package ignore_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/ignore"
	"github.com/gechr/clover/internal/vcs"
	"github.com/stretchr/testify/require"
)

// disabledRepo writes a .git marker and a root .gitignore excluding foo under a
// fresh temp dir, returning the root.
func disabledRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("foo\n"), 0o644))
	return root
}

func TestWithDisabled(t *testing.T) {
	t.Parallel()

	root := disabledRepo(t)

	enabled := ignore.New(vcs.NewResolver())
	require.True(t, enabled.Ignore(filepath.Join(root, "foo"), false),
		"the matcher excludes foo by default")

	disabled := ignore.New(vcs.NewResolver(), ignore.WithDisabled())
	require.False(t, disabled.Ignore(filepath.Join(root, "foo"), false),
		"WithDisabled ignores nothing")
}

func TestClassPatternMatches(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, ".gitignore"),
		[]byte("[abc].log\n"),
		0o644,
	))

	matcher := ignore.New(vcs.NewResolver())
	require.True(t, matcher.Ignore(filepath.Join(root, "a.log"), false),
		"a class member matches")
	require.False(t, matcher.Ignore(filepath.Join(root, "d.log"), false),
		"a non-member does not match")
}
