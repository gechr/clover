package mode_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// orderedProvider declares two keys so format's provider-block ordering can be
// exercised. It never resolves - format is offline.
type orderedProvider struct{ name string }

func (p orderedProvider) Name() string { return p.name }

func (p orderedProvider) Keys() []provider.Key {
	return []provider.Key{{Name: "repo", Required: true}, {Name: "source"}}
}

func (p orderedProvider) Resource(directive.Directive) (provider.Resource, error) {
	return p.name, nil
}

func (p orderedProvider) Describe(provider.Resource) string { return p.name }

func (p orderedProvider) Discover(context.Context, provider.Resource) ([]model.Candidate, error) {
	panic("format must never call Discover")
}

// formatDir writes a single file and returns the dir and the file path.
func formatDir(t *testing.T, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.txt")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return dir, path
}

func TestFormatReordersAndWrites(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtp"})
	dir, path := formatDir(
		t,
		"# clover: source=tags constraint=patch repo=a/b provider=fmtp\nversion: 1.0.0\n",
	)

	summary, err := mode.Format(context.Background(), []string{dir}, false)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Changed())
	require.True(t, summary.Files[0].Written)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t,
		"# clover: provider=fmtp repo=a/b source=tags constraint=patch\nversion: 1.0.0\n",
		string(got),
	)
}

func TestFormatLeavesVersionLineUntouched(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtv"})
	dir, path := formatDir(t, "# clover: repo=a/b provider=fmtv\nversion: 1.2.3-rc.1\n")

	_, err := mode.Format(context.Background(), []string{dir}, false)
	require.NoError(t, err)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(got), "version: 1.2.3-rc.1") // version untouched
}

func TestFormatCheckWritesNothing(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtc"})
	original := "# clover: repo=a/b provider=fmtc\nversion: 1.0.0\n"
	dir, path := formatDir(t, original)

	summary, err := mode.Format(context.Background(), []string{dir}, true)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Changed())
	require.False(t, summary.OK())
	require.False(t, summary.Files[0].Written)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, string(got)) // check never writes
}

func TestFormatAlreadyCanonicalIsNoop(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtn"})
	original := "# clover: provider=fmtn repo=a/b\nversion: 1.0.0\n"
	dir, path := formatDir(t, original)

	summary, err := mode.Format(context.Background(), []string{dir}, false)
	require.NoError(t, err)
	require.True(t, summary.OK())
	require.False(t, summary.Files[0].Written)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, string(got))
}

func TestFormatIsIdempotent(t *testing.T) {
	provider.Register(orderedProvider{name: "fmti"})
	dir, path := formatDir(t, "# clover: source=tags repo=a/b provider=fmti\nv: 1.0.0\n")

	_, err := mode.Format(context.Background(), []string{dir}, false)
	require.NoError(t, err)
	first, err := os.ReadFile(path)
	require.NoError(t, err)

	summary, err := mode.Format(context.Background(), []string{dir}, false)
	require.NoError(t, err)
	require.True(t, summary.OK()) // second pass finds nothing to change
	second, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(first), string(second))
}

func TestFormatNormalisesQuoting(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtq"})
	dir, path := formatDir(t, `# clover: provider=fmtq repo="a/b"`+"\nv: 1.0.0\n")

	_, err := mode.Format(context.Background(), []string{dir}, false)
	require.NoError(t, err)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(got), "repo=a/b") // redundant quotes dropped
}

func TestFormatPreservesBlockComment(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtb"})
	dir := t.TempDir()
	path := filepath.Join(dir, "index.html")
	require.NoError(
		t,
		os.WriteFile(path, []byte("<!-- clover: repo=a/b provider=fmtb -->\nv: 1.0.0\n"), 0o644),
	)

	_, err := mode.Format(context.Background(), []string{dir}, false)
	require.NoError(t, err)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "<!-- clover: provider=fmtb repo=a/b -->\nv: 1.0.0\n", string(got))
}

func TestFormatPreservesFileMode(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtperm"})
	dir, path := formatDir(t, "# clover: repo=a/b provider=fmtperm\nv: 1.0.0\n")
	require.NoError(t, os.Chmod(path, 0o777))

	_, err := mode.Format(context.Background(), []string{dir}, false)
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o777), info.Mode().Perm()) // clover never changes perms
}

func TestFormatFollowerReordersCommonKeys(t *testing.T) {
	// No provider= ⇒ a follower; only common keys, reordered (from before value).
	dir, path := formatDir(t, "# clover: value=version from=app\nv: 1.0.0\n")

	_, err := mode.Format(context.Background(), []string{dir}, false)
	require.NoError(t, err)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "# clover: from=app value=version\nv: 1.0.0\n", string(got))
}
