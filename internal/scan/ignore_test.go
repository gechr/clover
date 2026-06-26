package scan_test

import (
	"testing"

	"github.com/gechr/clover/internal/scan"
	"github.com/stretchr/testify/require"
)

func repoOf(t *testing.T, located scan.Located) string {
	t.Helper()
	repo, _ := located.Directive.Get("repository")
	return repo
}

func TestScanIgnoreControls(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		// clover:ignore suppresses only the directive on the next line.
		"next.yaml": "# clover:ignore\n" +
			"# clover: provider=github repository=ignored/one\n" +
			"x: 1\n" +
			"# clover: provider=github repository=kept/two\n" +
			"y: 2\n",
		// A start/end pair suppresses every directive between.
		"block.yaml": "# clover:ignore-start\n" +
			"# clover: provider=github repository=block/one\n" +
			"# clover: provider=github repository=block/two\n" +
			"# clover:ignore-end\n" +
			"# clover: provider=github repository=kept/three\n" +
			"z: 3\n",
		// clover:ignore-file drops the whole file.
		"whole.yaml": "# clover:ignore-file\n" +
			"# clover: provider=github repository=whole/file\n" +
			"w: 4\n",
	})

	files, _, err := scan.Scan(t.Context(), []string{root})
	require.NoError(t, err)

	got := byPath(files)
	require.NotContains(t, got, "whole.yaml", "clover:ignore-file drops the file")

	next := got["next.yaml"]
	require.Len(t, next.Found, 1)
	require.Equal(t, "kept/two", repoOf(t, next.Found[0]))

	block := got["block.yaml"]
	require.Len(t, block.Found, 1)
	require.Equal(t, "kept/three", repoOf(t, block.Found[0]))
}
