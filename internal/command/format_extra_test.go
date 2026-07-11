package command_test

import (
	"testing"

	"github.com/gechr/clover/internal/command"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// TestRunFormat drives the format command through its canonicalise, check,
// unknown-key, and prune branches against an offline fake provider.
func TestRunFormat(t *testing.T) {
	provider.Register(stubProvider{
		name: "fmtcmd",
		keys: []provider.Key{{Name: "repository"}, {Name: "source"}},
	})

	const (
		messy     = "# clover: source=tags constraint=patch repository=a/b provider=fmtcmd\nversion: 1.0.0\n"
		canonical = "# clover: provider=fmtcmd repository=a/b source=tags constraint=patch\nversion: 1.0.0\n"
	)

	t.Run("check reports and writes nothing", func(t *testing.T) {
		dir, path := writeMarker(t, messy)
		err := command.RunFormat([]string{dir}, true, false, nil, false, resolver(), 4)
		require.EqualError(t, err, "1 directive would be reformatted")
		require.Equal(t, messy, readAt(t, path), "--check never writes")
	})

	t.Run("default rewrites canonical", func(t *testing.T) {
		dir, path := writeMarker(t, messy)
		require.NoError(
			t,
			command.RunFormat([]string{dir}, false, false, nil, false, resolver(), 4),
		)
		require.Equal(t, canonical, readAt(t, path))
	})

	t.Run("dry-run previews without writing", func(t *testing.T) {
		dir, path := writeMarker(t, messy)
		require.NoError(t, command.RunFormat([]string{dir}, false, true, nil, false, resolver(), 4))
		require.Equal(t, messy, readAt(t, path), "--dry-run exits zero and writes nothing")
	})

	t.Run("unknown key without prune errors", func(t *testing.T) {
		dir, path := writeMarker(t,
			"# clover: provider=fmtcmd repository=a/b max-major=4\nversion: 1.0.0\n")
		err := command.RunFormat([]string{dir}, false, false, nil, false, resolver(), 4)
		require.EqualError(t, err, "1 directive with an unknown key (use --prune to remove)")
		require.Equal(t,
			"# clover: provider=fmtcmd repository=a/b max-major=4\nversion: 1.0.0\n",
			readAt(t, path), "the rejected line is left untouched")
	})

	t.Run("prune removes the unknown key", func(t *testing.T) {
		dir, path := writeMarker(
			t,
			"# clover: provider=fmtcmd repository=a/b max-major=4 constraint=minor\nversion: 1.0.0\n",
		)
		require.NoError(
			t,
			command.RunFormat([]string{dir}, false, false, new(true), false, resolver(), 4),
		)
		require.Equal(t,
			"# clover: provider=fmtcmd repository=a/b constraint=minor\nversion: 1.0.0\n",
			readAt(t, path), "--prune strips the unknown key and reorders the rest")
	})

	t.Run("canonical file needs no write", func(t *testing.T) {
		dir, path := writeMarker(t, canonical)
		require.NoError(
			t,
			command.RunFormat([]string{dir}, false, false, nil, false, resolver(), 4),
		)
		require.Equal(t, canonical, readAt(t, path))
	})
}
