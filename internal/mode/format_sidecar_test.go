package mode_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/mode"
	"github.com/stretchr/testify/require"
)

// formatSidecarTree writes a JSON target plus its sidecar and returns the root
// and the sidecar path.
func formatSidecarTree(t *testing.T, sidecar string) (string, string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(schemaJSON), 0o644),
	)
	path := filepath.Join(root, "tsconfig.json.clover.yaml")
	require.NoError(t, os.WriteFile(path, []byte(sidecar), 0o644))
	return root, path
}

func runFormat(t *testing.T, root string, dry bool, prune *bool) mode.FormatSummary {
	t.Helper()
	summary, err := mode.Format(
		context.Background(),
		[]string{root},
		dry,
		prune,
		config.NewResolver(nil, "", false),
	)
	require.NoError(t, err)
	return summary
}

const schemaJSON = "{\n  \"$schema\": \"https://example.test/schemas/1.5.3/schema.json\"\n}\n"

// TestFormatSidecarReordersAndIsIdempotent is Plan 5's re-emit case: a mis-ordered
// hand-written sidecar is rewritten into canonical key order, and a second pass
// finds nothing to change.
func TestFormatSidecarReordersAndIsIdempotent(t *testing.T) {
	t.Parallel()

	root, path := formatSidecarTree(
		t,
		"- repository: a/b\n  constraint: minor\n  provider: github\n  find: schemas/<version>/schema.json\n",
	)

	summary := runFormat(t, root, false, nil)
	require.Equal(t, 1, summary.Changed())
	require.Equal(
		t,
		"- provider: github\n  repository: a/b\n  find: schemas/<version>/schema.json\n  constraint: minor\n",
		readFile(t, path),
		"provider leads, then provider keys, the locator zone, then the selection rule",
	)

	second := runFormat(t, root, false, nil)
	require.True(t, second.OK(), "the canonical sidecar needs no further change")
}

// TestFormatSidecarPreservesComments confirms a comment above an entry survives
// the re-emit - format normalizes order, not annotations.
func TestFormatSidecarPreservesComments(t *testing.T) {
	t.Parallel()

	root, path := formatSidecarTree(
		t,
		"# managed by clover\n- repository: a/b\n  provider: github\n  find: schemas/<version>/schema.json\n",
	)

	runFormat(t, root, false, nil)
	require.Equal(
		t,
		"# managed by clover\n- provider: github\n  repository: a/b\n  find: schemas/<version>/schema.json\n",
		readFile(t, path),
	)
}

// TestFormatSidecarPreservesKeyComments confirms a note attached to an individual
// key or value - not just one above the entry - survives the re-emit and follows
// its key into canonical order, since the reorder rebuilds the mapping node.
func TestFormatSidecarPreservesKeyComments(t *testing.T) {
	t.Parallel()

	root, path := formatSidecarTree(
		t,
		"- repository: a/b  # the dep\n  provider: github\n  # locate the schema line\n  find: schemas/<version>/schema.json\n",
	)

	runFormat(t, root, false, nil)
	require.Equal(
		t,
		"- provider: github\n  repository: a/b # the dep\n  # locate the schema line\n  find: schemas/<version>/schema.json\n",
		readFile(t, path),
		"the line comment follows repository and the head comment follows find into canonical order",
	)
}

// TestFormatSidecarCheckWritesNothing confirms --check/--dry-run reports the
// pending change but leaves the sidecar untouched.
func TestFormatSidecarCheckWritesNothing(t *testing.T) {
	t.Parallel()

	original := "- repository: a/b\n  provider: github\n  find: schemas/<version>/schema.json\n"
	root, path := formatSidecarTree(t, original)

	summary := runFormat(t, root, true /* dry */, nil)
	require.Equal(t, 1, summary.Changed())
	require.False(t, summary.OK())
	require.Equal(t, original, readFile(t, path), "a dry run never writes")
}

// TestFormatSidecarAlreadyCanonicalIsNoop confirms a canonical sidecar is left
// byte-identical and reports no change.
func TestFormatSidecarAlreadyCanonicalIsNoop(t *testing.T) {
	t.Parallel()

	canonical := "- provider: github\n  repository: a/b\n  find: schemas/<version>/schema.json\n"
	root, path := formatSidecarTree(t, canonical)

	summary := runFormat(t, root, false, nil)
	require.True(t, summary.OK())
	require.Equal(t, canonical, readFile(t, path))
}

// TestFormatSidecarRejectsUnknownKey confirms an unknown key in a sidecar entry
// fails format (non-zero) and leaves the file untouched, exactly as for inline.
func TestFormatSidecarRejectsUnknownKey(t *testing.T) {
	t.Parallel()

	original := "- provider: github\n  repository: a/b\n  find: schemas/<version>/schema.json\n  maxmajor: 4\n"
	root, path := formatSidecarTree(t, original)

	summary := runFormat(t, root, false, nil)
	require.Equal(t, 1, summary.Errored())
	require.Equal(t, 0, summary.Changed(), "a rejected sidecar is not rewritten")
	require.Equal(t, original, readFile(t, path))
}

// TestFormatSidecarPruneRemovesUnknownKey confirms --prune strips an unknown key
// from a sidecar entry while keeping jq, which a sidecar legitimately carries.
func TestFormatSidecarPruneRemovesUnknownKey(t *testing.T) {
	t.Parallel()

	root, path := formatSidecarTree(t,
		"- provider: github\n  repository: a/b\n  jq: .[\"$schema\"]\n  maxmajor: 4\n",
	)

	summary := runFormat(t, root, false, new(true))
	require.Equal(t, 0, summary.Errored(), "prune removes the key rather than erroring")
	require.Equal(t, 1, summary.Changed())
	require.Equal(t,
		"- provider: github\n  repository: a/b\n  jq: .[\"$schema\"]\n",
		readFile(t, path),
		"the unknown key is stripped; the jq locator a sidecar may carry is kept",
	)
}

// TestFormatSidecarLeavesBrokenToLint confirms format does not touch a sidecar
// with a malformed entry - those diagnostics belong to lint, not format.
func TestFormatSidecarLeavesBrokenToLint(t *testing.T) {
	t.Parallel()

	// An entry that is not a mapping is structurally broken; format leaves it be.
	original := "- provider: github\n  repository: a/b\n  find: schemas/<version>/schema.json\n- just a string\n"
	root, path := formatSidecarTree(t, original)

	summary := runFormat(t, root, false, nil)
	require.Equal(t, 0, summary.Errored())
	require.True(t, summary.OK())
	require.Equal(t, original, readFile(t, path))
}
