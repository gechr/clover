package mode_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/mode"
	"github.com/stretchr/testify/require"
)

// TestAnnotateGeneratesSidecarForJSON is the headline case: a strict-JSON target
// with a trackable image line cannot host an inline comment, so annotate proposes
// a sidecar with an explicit provider directive and a jq locator. Preview reports
// it; --write lays down <target>.clover.yaml.
func TestAnnotateGeneratesSidecarForJSON(t *testing.T) {
	t.Parallel()

	target := "{\n  \"image\": \"nginx:1.27\"\n}\n"
	root := annotateTree(t, map[string]string{"k8s.json": target})

	// Preview writes nothing but reports the one entry it would add.
	preview := annotate(t, root, false, false)
	require.Equal(t, 1, preview.Added())
	require.Equal(t, 0, preview.Updated())
	sidecarPath := filepath.Join(root, "k8s.json.clover.yaml")
	require.NoFileExists(t, sidecarPath, "preview never writes the sidecar")

	// --write creates the sidecar in canonical key order with a verified jq locator
	// and a repository-anchored find (so a later edit of the line fails loud).
	summary := annotate(t, root, true, false)
	require.Equal(t, 1, summary.Added())
	require.Equal(t,
		"- provider: docker\n  repository: nginx\n  jq: .[\"image\"]\n  find: nginx:<version>\n",
		readFile(t, sidecarPath),
		"explicit provider, then provider keys, then the locator zone (jq, find)",
	)
	require.Equal(t, target,
		readFile(t, filepath.Join(root, "k8s.json")),
		"the JSON target itself is never rewritten by annotate")
}

// TestAnnotateSidecarOneEntryPerLine is the S1 regression: two trackable leaves on
// one physical line (minified JSON) must not each earn an entry whose jq resolves
// to that same line - that sidecar would fail its own lint with double-governance.
// A line earns one entry, so the generated sidecar lints clean.
func TestAnnotateSidecarOneEntryPerLine(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		"k8s.json": "{\"a\":{\"image\":\"nginx:1.27\"},\"b\":{\"image\":\"redis:6.0\"}}\n",
	})

	require.Equal(t, 1, annotate(t, root, true, false).Added(),
		"only the first leaf on the shared line earns an entry")
	require.Equal(
		t,
		"- provider: docker\n  repository: nginx\n  jq: .[\"a\"][\"image\"]\n  find: nginx:<version>\n",
		readFile(t, filepath.Join(root, "k8s.json.clover.yaml")),
	)

	// annotate never emits a directive lint would reject: the generated sidecar
	// must pass its own lint rather than self-collide on the shared line.
	summary, err := mode.Lint(context.Background(), []string{root})
	require.NoError(t, err)
	require.True(t, summary.OK(), "the generated sidecar lints clean")
}

// TestAnnotateSidecarForceLeavesBrokenSidecar is the S2 regression: --force must
// not clobber a structurally broken (non-list) sidecar - its hand-written content
// would be lost. Like the non-force path, force leaves it byte-identical for lint.
func TestAnnotateSidecarForceLeavesBrokenSidecar(t *testing.T) {
	t.Parallel()

	broken := "# IMPORTANT hand-written note\nconstraint: major\nnote-key: do-not-lose-me\n"
	root := annotateTree(t, map[string]string{
		"svc.json":             "{\n  \"image\": \"nginx:1.27\"\n}\n",
		"svc.json.clover.yaml": broken,
	})

	require.True(
		t,
		annotate(t, root, true, true).OK(),
		"a broken sidecar is not rewritten under force",
	)
	require.Equal(t, broken, readFile(t, filepath.Join(root, "svc.json.clover.yaml")),
		"the non-list sidecar is left byte-identical for lint to surface")
}

// TestAnnotateSidecarCarriesRegistry confirms a registry-qualified image yields
// both repository and registry, ordered as docker declares its keys.
func TestAnnotateSidecarCarriesRegistry(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		"k8s.json": "{\n  \"image\": \"ghcr.io/owner/api:1.2.0\"\n}\n",
	})

	require.Equal(t, 1, annotate(t, root, true, false).Added())
	require.Equal(
		t,
		"- provider: docker\n  repository: owner/api\n  registry: ghcr.io\n  jq: .[\"image\"]\n  find: ghcr.io/owner/api:<version>\n",
		readFile(t, filepath.Join(root, "k8s.json.clover.yaml")),
	)
}

// TestAnnotateSidecarRegistryPort confirms an image with a registry :port - which
// adds a second number-shaped token the smart locator would call ambiguous - is
// still annotated, because the repository-anchored find pins the version.
func TestAnnotateSidecarRegistryPort(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		"k8s.json": "{\n  \"image\": \"localhost:5000/team/api:2.0.1\"\n}\n",
	})

	require.Equal(t, 1, annotate(t, root, true, false).Added())
	require.Equal(
		t,
		"- provider: docker\n  repository: team/api\n  registry: localhost:5000\n  jq: .[\"image\"]\n  find: localhost:5000/team/api:<version>\n",
		readFile(t, filepath.Join(root, "k8s.json.clover.yaml")),
	)
}

// TestAnnotateSidecarSkipsPinnedReferences confirms a pinned image (an @sha256
// digest) earns no entry: its secure pin needs a pin-aware rewriter that does not
// yet work on a JSON string value, so annotating it would leave the digest stale.
func TestAnnotateSidecarSkipsPinnedReferences(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		"k8s.json": "{\n  \"image\": \"nginx:1.27@sha256:abc\"\n}\n",
	})

	require.True(t, annotate(t, root, true, false).OK(), "a pinned reference is not annotated")
	require.NoFileExists(t, filepath.Join(root, "k8s.json.clover.yaml"))
}

// TestAnnotateSidecarIsIdempotent proves a second pass finds the line already
// governed by the existing entry and proposes nothing - no duplicate entry.
func TestAnnotateSidecarIsIdempotent(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{"k8s.json": "{\n  \"image\": \"nginx:1.27\"\n}\n"})

	require.Equal(t, 1, annotate(t, root, true, false).Added())
	require.True(t, annotate(t, root, false, false).OK(),
		"the recognized line already has a sidecar entry")
}

// TestAnnotateSidecarSkipsUnlocatableLines guards the verify-before-write gate for
// JSON: a string with no trackable reference (a bare version, an invalid image
// ref) earns no entry, so annotate never emits a directive lint would reject.
func TestAnnotateSidecarSkipsUnlocatableLines(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		"plain.json": "{\n  \"version\": \"1.2.3\"\n}\n",      // no provider route matches a version: key
		"bad.json":   "{\n  \"image\": \"bad repo:1.2\"\n}\n", // an invalid repository never resolves
	})

	require.True(t, annotate(t, root, true, false).OK(), "neither file earns an entry")
	require.NoFileExists(t, filepath.Join(root, "plain.json.clover.yaml"))
	require.NoFileExists(t, filepath.Join(root, "bad.json.clover.yaml"))
}

// TestAnnotateSidecarAppendsPreservingExisting confirms a new entry is appended to
// an existing sidecar without disturbing the entries (or comments) already there.
func TestAnnotateSidecarAppendsPreservingExisting(t *testing.T) {
	t.Parallel()

	existing := "# hand-written\n- provider: github\n  repository: a/b\n  jq: .[\"$schema\"]\n"
	root := annotateTree(t, map[string]string{
		"app.json":             "{\n  \"$schema\": \"https://x/schemas/1.0.0/s.json\",\n  \"image\": \"redis:7.2\"\n}\n",
		"app.json.clover.yaml": existing,
	})

	summary := annotate(t, root, true, false)
	require.Equal(t, 1, summary.Added(), "only the new image entry is added")
	require.Equal(
		t,
		existing+"- provider: docker\n  repository: redis\n  jq: .[\"image\"]\n  find: redis:<version>\n",
		readFile(t, filepath.Join(root, "app.json.clover.yaml")),
		"the existing entry and its comment are preserved verbatim; the new entry is appended",
	)
}

// TestAnnotateSidecarAppendLeavesBrokenSidecar confirms append refuses to write
// after a structurally broken (non-list) sidecar - compounding the corruption -
// and leaves it for lint to surface.
func TestAnnotateSidecarAppendLeavesBrokenSidecar(t *testing.T) {
	t.Parallel()

	broken := "not: a list\n"
	root := annotateTree(t, map[string]string{
		"k8s.json":             "{\n  \"image\": \"nginx:1.27\"\n}\n",
		"k8s.json.clover.yaml": broken,
	})

	require.True(t, annotate(t, root, true, false).OK(), "a broken sidecar is not appended to")
	require.Equal(t, broken, readFile(t, filepath.Join(root, "k8s.json.clover.yaml")),
		"the broken sidecar is left byte-identical for lint to report")
}

// TestAnnotateSidecarForceRepairsDrift exercises --force: an existing entry whose
// explicit repository has drifted from the line it locates is re-derived, while a
// selection rule and any comment it carries survive. Without --force the stale
// entry is left be.
func TestAnnotateSidecarForceRepairsDrift(t *testing.T) {
	t.Parallel()

	stale := "- provider: docker\n  repository: nginx\n  jq: .[\"image\"]\n  # keep us on minors\n  constraint: minor\n"
	files := map[string]string{
		"k8s.json":             "{\n  \"image\": \"redis:7.2\"\n}\n",
		"k8s.json.clover.yaml": stale,
	}

	// Default: the drifted entry is untouched (its line is already governed).
	noForce := annotateTree(t, files)
	require.True(t, annotate(t, noForce, true, false).OK())
	require.Equal(t, stale, readFile(t, filepath.Join(noForce, "k8s.json.clover.yaml")))

	// --force re-derives the source keys from the current line, keeping the rule
	// and the comment that sits above it through the re-render.
	forced := annotateTree(t, files)
	summary := annotate(t, forced, true, true)
	require.Equal(t, 1, summary.Updated())
	require.Equal(t, 0, summary.Added())
	require.Equal(
		t,
		"- provider: docker\n  repository: redis\n  jq: .[\"image\"]\n  # keep us on minors\n  constraint: minor\n",
		readFile(t, filepath.Join(forced, "k8s.json.clover.yaml")),
		"repository repaired to match the line; locator, constraint, and its comment preserved",
	)
}

// TestAnnotateSidecarForcePreservesUnresolvableEntries is the C1 regression: a
// force pass triggered by one drifted entry must NOT delete a sibling entry whose
// locator does not currently resolve (it lives only in the diagnostics file, not
// file.Found), nor drop the file's comments. The unresolvable entry and the
// comment survive; only the drifted entry is repaired.
func TestAnnotateSidecarForcePreservesUnresolvableEntries(t *testing.T) {
	t.Parallel()

	sidecar := "- provider: docker\n  repository: stale\n  jq: .[\"image\"]\n" +
		"# hand-written, keep me\n- provider: github\n  repository: a/b\n  jq: .[\"missing\"]\n"
	root := annotateTree(t, map[string]string{
		"k8s.json":             "{\n  \"image\": \"nginx:1.27\"\n}\n",
		"k8s.json.clover.yaml": sidecar,
	})

	summary := annotate(t, root, true, true)
	require.Equal(t, 1, summary.Updated(), "only the drifted docker entry is repaired")
	got := readFile(t, filepath.Join(root, "k8s.json.clover.yaml"))
	require.Equal(
		t,
		"- provider: docker\n  repository: nginx\n  jq: .[\"image\"]\n"+
			"# hand-written, keep me\n- provider: github\n  repository: a/b\n  jq: .[\"missing\"]\n",
		got,
		"the unresolvable github entry and the comment survive; only the docker repository is repaired",
	)
}
