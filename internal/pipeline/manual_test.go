package pipeline_test

import (
	"context"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/manual"
	"github.com/stretchr/testify/require"
)

// A manual root publishes the value already on its line under its id, and never
// rewrites its own line - a person owns it. A follower in another file reads the
// published value and renders it onto its own line.
func TestRunManualPublishesToFollower(t *testing.T) {
	provider.Register(manual.New())

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=manual id=nginx\nARG NGINX_VERSION=1.27.3\n",
		"b.txt": "# clover: from=nginx value=version\nversion: 0.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 2)

	// a.txt sorts before b.txt: the manual root resolves to the line's own value.
	root := files[0].Results[0]
	require.NoError(t, root.Err)
	require.False(t, root.Changed, "a manual root never rewrites its own line")
	require.Equal(t, "ARG NGINX_VERSION=1.27.3", root.NewLine)
	require.Equal(t, "1.27.3", root.Current)
	require.Equal(t, "1.27.3", root.Resolved)
	require.Equal(t, "1.27.3", root.Written)

	// b.txt follows the published id and renders it onto its line.
	follower := files[1].Results[0]
	require.NoError(t, follower.Err)
	require.Equal(t, "1.27.3", follower.Resolved)
	require.Equal(t, "version: 1.27.3", follower.NewLine)
}

// The key safety property: a manual marker on a digest-pinned line leaves the
// whole line - tag and @sha256 - byte-identical. It only publishes; rendering it
// would risk dropping the digest the candidate does not carry.
func TestRunManualLeavesDigestPinIntact(t *testing.T) {
	provider.Register(manual.New())

	digest := "sha256:" + strings.Repeat("a", 64)
	from := "FROM library/nginx:1.27.3@" + digest
	dir := write(t, map[string]string{
		"Dockerfile": "# clover: provider=manual id=nginx\n" + from + "\n",
		"b.txt":      "# clover: from=nginx value=version\nversion: 0.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)

	root := files[0].Results[0]
	require.NoError(t, root.Err)
	require.False(t, root.Changed)
	require.Equal(t, from, root.NewLine, "tag and digest are left untouched")

	require.Equal(t, "version: 1.27.3", files[1].Results[0].NewLine,
		"the follower still gets the value the manual root published")
}

// A follower may template the manual value into a styled line via find/replace.
func TestRunManualFollowerFindReplace(t *testing.T) {
	provider.Register(manual.New())

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=manual id=nginx\nARG NGINX_VERSION=1.27.3\n",
		"b.txt": "# clover: from=nginx value=version find=nginx-v<version>-linux\n" +
			"url: nginx-v0.0.0-linux\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, "url: nginx-v1.27.3-linux", files[1].Results[0].NewLine)
}

// find= pins which token on an ambiguous line is the value, ignoring other
// version-shaped tokens - the way to extract a value by pattern.
func TestRunManualFindExtractsToken(t *testing.T) {
	provider.Register(manual.New())

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=manual id=tool find=tool-<version>-linux\n" +
			"deps: other-9.9.9 tool-1.27.3-linux\n",
		"b.txt": "# clover: from=tool value=version\nversion: 0.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	root := files[0].Results[0]
	require.NoError(t, root.Err)
	require.False(t, root.Changed)
	require.Equal(t, "1.27.3", root.Current, "find pins the tool token, not other-9.9.9")
	require.Equal(t, "version: 1.27.3", files[1].Results[0].NewLine)
}

// find= that matches nothing on the line fails loud, rather than publishing an
// empty or wrong value.
func TestRunManualFindNoMatchErrors(t *testing.T) {
	provider.Register(manual.New())

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=manual id=tool find=tool-<version>-linux\ndeps: 1.2.3\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Error(t, files[0].Results[0].Err, "a find that matches nothing fails loud")
	require.False(t, files[0].Results[0].Changed)
}

// A manual root publishes a prerelease verbatim: with no selection stage, the
// prerelease gate that filters discovered candidates never applies.
func TestRunManualPublishesPrereleaseVerbatim(t *testing.T) {
	provider.Register(manual.New())

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=manual id=app\nARG APP_VERSION=1.0.0-beta.1\n",
		"b.txt": "# clover: from=app value=version\nversion: 0.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	root := files[0].Results[0]
	require.NoError(t, root.Err)
	require.Equal(t, "1.0.0-beta.1", root.Current)
	require.Equal(
		t,
		"1.0.0-beta.1",
		root.Resolved,
		"a hand-pinned prerelease is published, gate skipped",
	)
	require.Equal(t, "1.0.0-beta.1", files[1].Results[0].Resolved, "the follower receives it")
}

// A manual marker is pointless without an id to publish under - it neither
// rewrites its line nor contacts an upstream - so resolution fails loud.
func TestRunManualRequiresID(t *testing.T) {
	provider.Register(manual.New())

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=manual\nARG NGINX_VERSION=1.27.3\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.EqualError(t, files[0].Results[0].Err, `manual: "id" is required`)
	require.False(t, files[0].Results[0].Changed)
}
