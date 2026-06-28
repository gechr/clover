package scan_test

import (
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/scan"
	"github.com/stretchr/testify/require"
)

const tsconfig = `{
  "$schema": "https://biomejs.dev/schemas/1.5.3/schema.json",
  "compilerOptions": { "strict": true }
}
`

func TestScanDiscoversSidecar(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"tsconfig.json": tsconfig,
		"tsconfig.json.clover.yaml": "- provider: github\n" +
			"  repository: biomejs/biome\n" +
			"  find: schemas/<version>/schema.json\n",
	})

	files, _, err := scan.Scan(t.Context(), []string{root})
	require.NoError(t, err)

	got := byPath(files)
	require.Len(t, files, 1, "the sidecar yields one File, for the target")
	target := got["tsconfig.json"]
	require.Len(t, target.Found, 1)

	loc := target.Found[0]
	require.True(t, loc.Sidecar, "the entry is marked as sidecar-sourced")
	require.Equal(t, 1, loc.Line, "resolved to the $schema line, not the comment line above")
	repo, _ := loc.Directive.Get("repository")
	require.Equal(t, "biomejs/biome", repo)
}

func TestScanSidecarYmlExtension(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"tsconfig.json": tsconfig,
		"tsconfig.json.clover.yml": "- provider: github\n" +
			"  repository: biomejs/biome\n" +
			"  find: schemas/<version>/schema.json\n",
	})

	files, _, err := scan.Scan(t.Context(), []string{root})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Len(t, files[0].Found, 1)
}

func TestScanSidecarYAMLWinsOverYml(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"tsconfig.json": tsconfig,
		"tsconfig.json.clover.yaml": "- provider: github\n" +
			"  repository: from/yaml\n" +
			"  find: schemas/<version>/schema.json\n",
		"tsconfig.json.clover.yml": "- provider: github\n" +
			"  repository: from/yml\n" +
			"  find: schemas/<version>/schema.json\n",
	})

	files, _, err := scan.Scan(t.Context(), []string{root})
	require.NoError(t, err)
	require.Len(t, files, 1, "only the .yaml sidecar is processed")
	repo, _ := files[0].Found[0].Directive.Get("repository")
	require.Equal(t, "from/yaml", repo, ".yaml wins over .yml")
}

// A sidecar must not smuggle an excluded target past the ignore/exclude rules:
// the walk only filters the sidecar path, so the target is re-checked by name.
func TestScanSidecarRespectsExclude(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"tsconfig.json": tsconfig,
		"tsconfig.json.clover.yaml": "- provider: github\n" +
			"  repository: a/b\n" +
			"  find: schemas/<version>/schema.json\n",
	})
	ignore := func(path string, _ bool) bool { return filepath.Base(path) == "tsconfig.json" }

	files, _, err := scan.Scan(t.Context(), []string{root}, scan.WithIgnore(ignore))
	require.NoError(t, err)
	require.Empty(t, files, "an excluded target is not processed through its sidecar")
}

func TestScanDanglingSidecarIgnored(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"orphan.json.clover.yaml": "- provider: github\n  find: x\n",
	})

	files, _, err := scan.Scan(t.Context(), []string{root})
	require.NoError(t, err)
	require.Empty(t, files, "a sidecar with no target sibling fabricates nothing")
}

func TestScanSidecarFindAmbiguous(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"versions.json": "{\n  \"a\": \"1.0.0\",\n  \"b\": \"2.0.0\"\n}\n",
		"versions.json.clover.yaml": "- provider: github\n" +
			"  repository: a/b\n" +
			"  find: <version>\n",
	})

	files, _, err := scan.Scan(t.Context(), []string{root})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Empty(t, files[0].Found)
	require.Len(t, files[0].Errors, 1)
	require.EqualError(t, files[0].Errors[0].Err,
		"sidecar entry at line 1: find matched 2 lines; make it more specific")
}

func TestScanSidecarMissingLocator(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"tsconfig.json":             tsconfig,
		"tsconfig.json.clover.yaml": "- provider: github\n  repository: a/b\n",
	})

	files, _, err := scan.Scan(t.Context(), []string{root})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Empty(t, files[0].Found)
	require.Len(t, files[0].Errors, 1)
	require.EqualError(t, files[0].Errors[0].Err,
		`sidecar entry at line 1: needs a "find" or "jq" locator`)
}

// A sidecar entry resolving onto a line a clover:ignore control suppresses is
// dropped (the local opt-out wins), not applied.
func TestScanSidecarRespectsIgnore(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		// The // clover:ignore control suppresses the schema line below it.
		"tsconfig.jsonc": "{\n  // clover:ignore\n  \"$schema\": \"https://x/schemas/1.5.3/schema.json\"\n}\n",
		"tsconfig.jsonc.clover.yaml": "- provider: github\n" +
			"  repository: a/b\n" +
			"  find: schemas/<version>/schema.json\n",
	})

	files, _, err := scan.Scan(t.Context(), []string{root})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Empty(t, files[0].Found, "the ignored line is not governed by the sidecar")
	require.Empty(t, files[0].Errors, "an ignore opt-out is a skip, not an error")
}

// A sidecar entry resolving onto a line an inline directive already governs is
// double-governance: an errored marker, never a silent second write.
func TestScanSidecarDoubleGovernance(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"deps.yaml": "# clover: provider=github repository=a/b\nimage: redis:1.5.3\n",
		"deps.yaml.clover.yaml": "- provider: github\n" +
			"  repository: a/b\n" +
			"  find: redis:<version>\n",
	})

	files, _, err := scan.Scan(t.Context(), []string{root})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Len(t, files[0].Found, 1, "only the inline directive governs the line")
	require.False(t, files[0].Found[0].Sidecar)
	require.Len(t, files[0].Errors, 1)
	require.EqualError(t, files[0].Errors[0].Err,
		"sidecar entry at line 1: targets line 2, already governed by another directive")
}

// A target that carries its own inline directive is merged with its sidecar's
// entries into one File, when the two govern different lines.
func TestScanSidecarMergesWithInline(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"deps.yaml": "# clover: provider=github repository=a/b\n" +
			"image: redis:1.0.0\n" +
			"schema: https://x/schemas/2.0.0/schema.json\n",
		"deps.yaml.clover.yaml": "- provider: github\n" +
			"  repository: c/d\n" +
			"  find: schemas/<version>/schema.json\n",
	})

	files, _, err := scan.Scan(t.Context(), []string{root})
	require.NoError(t, err)
	require.Len(t, files, 1, "inline and sidecar fold into one File for the target")
	require.Len(t, files[0].Found, 2)
	// Sorted by line: the inline comment (line 0) precedes the sidecar entry (line 2).
	require.False(t, files[0].Found[0].Sidecar)
	require.True(t, files[0].Found[1].Sidecar)
	require.Equal(t, 2, files[0].Found[1].Line)
}
