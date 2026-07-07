package mode_test

// clover:ignore-file - the fixtures below embed clover: directives as test data,
// not real markers for clover to act on.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/progress"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/docker"
	"github.com/gechr/clover/internal/provider/github"
	"github.com/stretchr/testify/require"
)

// sha is a 40-hex commit, the shape the action-pin route keys on.
const sha = "a0dfaeb072753c3d48cd4df5fdacfd035b2281bf"

// testWorkers exercises the per-file concurrency path in the mode tests; results
// must be identical and correctly ordered regardless of worker count.
const testWorkers = 8

// TestMain registers the real docker and github providers, which annotate's
// verify gate validates inferred resources against. Their Resource parsing is
// offline (only Discover touches the network, which annotate never calls), so the
// tests stay hermetic.
func TestMain(m *testing.M) {
	provider.RegisterAll(docker.New(), github.New())
	os.Exit(m.Run())
}

// annotateTree writes files under a fresh temp dir and returns its path.
func annotateTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return root
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(got)
}

func annotate(t *testing.T, root string, write, force bool) mode.AnnotateSummary {
	t.Helper()
	summary, err := mode.Annotate(
		context.Background(),
		[]string{root},
		write,
		force,
		config.NewResolver(nil, "", false),
		progress.Nop{},
		testWorkers,
	)
	require.NoError(t, err)
	return summary
}

// trackReporter is a progress.Reporter that records the verification tracker's
// configuration and highest count, so a test can assert annotate drives it. Set
// is called concurrently from the parallel verify loop, so it is mutex-guarded
// and keeps the max (the order of concurrent updates is not deterministic).
type trackReporter struct {
	mu      sync.Mutex
	label   string
	field   string
	total   int
	last    int
	stopped bool
}

func (r *trackReporter) Track(label, field string, total int) progress.Tracker {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.label, r.field, r.total = label, field, total
	return r
}

func (r *trackReporter) Set(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last = max(r.last, n)
}

func (r *trackReporter) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = true
}

func (*trackReporter) Discovered(int, int, int) {}

func (*trackReporter) Begin([]string) ([]progress.Task, func()) {
	return nil, func() {}
}

// TestAnnotateReportsVerifyProgress confirms annotate drives a fraction tracker
// over the files it verifies, advancing it to the file count and stopping it.
func TestAnnotateReportsVerifyProgress(t *testing.T) {
	root := annotateTree(t, map[string]string{
		"Dockerfile":   "FROM nginx:1.27\n",
		"compose.yaml": "services:\n  web:\n    image: ghcr.io/owner/api:1.2.0\n",
	})

	reporter := &trackReporter{}
	summary, err := mode.Annotate(
		context.Background(),
		[]string{root},
		false,
		false,
		config.NewResolver(nil, "", false),
		reporter,
		testWorkers,
	)
	require.NoError(t, err)
	require.NotEmpty(t, summary.Files)

	require.Equal(t, "Verifying annotate candidates", reporter.label)
	require.Equal(t, "progress", reporter.field)
	require.Positive(t, reporter.total, "the verify line shows a fraction over the file count")
	require.Equal(t, reporter.total, reporter.last, "every file advances the tracker")
	require.True(t, reporter.stopped, "the verify tracker must be stopped")
}

func TestAnnotateInsertsForRecognizedLines(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		".github/workflows/ci.yaml": "jobs:\n  build:\n    steps:\n      - uses: actions/checkout@" + sha + " # v4.1.0\n",
		"Dockerfile":                "FROM nginx:1.27\n",
		"compose.yaml":              "services:\n  web:\n    image: ghcr.io/owner/api:1.2.0\n",
	})

	summary := annotate(t, root, true, false)
	require.Equal(t, 3, summary.Added())
	require.Equal(t, 0, summary.Updated())

	require.Equal(
		t,
		"jobs:\n  build:\n    steps:\n      # clover: provider=auto\n      - uses: actions/checkout@"+sha+" # v4.1.0\n",
		readFile(t, filepath.Join(root, ".github/workflows/ci.yaml")),
		"the comment is inserted above the uses: line, indented to match it",
	)
	require.Equal(t,
		"# clover: provider=auto\nFROM nginx:1.27\n",
		readFile(t, filepath.Join(root, "Dockerfile")))
	require.Equal(t,
		"services:\n  web:\n    # clover: provider=auto\n    image: ghcr.io/owner/api:1.2.0\n",
		readFile(t, filepath.Join(root, "compose.yaml")))
}

func TestAnnotateInsertsForTagPinnedUses(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		".github/workflows/ci.yaml": "jobs:\n  build:\n    steps:\n      - uses: actions/checkout@v4\n",
	})

	summary := annotate(t, root, true, false)
	require.Equal(t, 1, summary.Added())

	require.Equal(
		t,
		"jobs:\n  build:\n    steps:\n      # clover: provider=auto\n      - uses: actions/checkout@v4\n",
		readFile(t, filepath.Join(root, ".github/workflows/ci.yaml")),
		"a tag-pinned uses: earns an annotation just like a SHA pin",
	)
}

func TestAnnotatePreviewWritesNothing(t *testing.T) {
	t.Parallel()

	original := "FROM nginx:1.27\n"
	root := annotateTree(t, map[string]string{"Dockerfile": original})

	summary := annotate(t, root, false, false)
	require.Equal(t, 1, summary.Added())
	require.False(t, summary.Files[0].Written, "preview never writes")
	require.Equal(t, original, readFile(t, filepath.Join(root, "Dockerfile")),
		"the file is byte-identical after a preview")
}

func TestAnnotateIsIdempotent(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{"Dockerfile": "FROM nginx:1.27\n"})

	require.Equal(t, 1, annotate(t, root, true, false).Added())
	require.True(t, annotate(t, root, false, false).OK(),
		"a second pass finds the line already annotated and proposes nothing")
}

func TestAnnotateBottomUpInsertsMultiple(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		"Dockerfile": "FROM nginx:1.27\nRUN build\nFROM redis:7.2\n",
	})

	require.Equal(t, 2, annotate(t, root, true, false).Added())
	require.Equal(
		t,
		"# clover: provider=auto\nFROM nginx:1.27\nRUN build\n# clover: provider=auto\nFROM redis:7.2\n",
		readFile(t, filepath.Join(root, "Dockerfile")),
		"both comments land above their own line; later insertions do not shift earlier ones",
	)
}

func TestAnnotateLeavesExistingByDefault(t *testing.T) {
	t.Parallel()

	original := "# clover: provider=docker repository=nginx constraint=minor\nFROM nginx:1.27\n"
	root := annotateTree(t, map[string]string{"Dockerfile": original})

	require.True(t, annotate(t, root, true, false).OK(),
		"an annotated line is never touched without --force")
	require.Equal(t, original, readFile(t, filepath.Join(root, "Dockerfile")))
}

func TestAnnotateForceCanonicalises(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		original string
		want     string
	}{
		{
			name:     "collapses redundant keys, keeps rule keys",
			original: "# clover: provider=docker repository=nginx constraint=minor\nFROM nginx:1.27\n",
			want:     "# clover: provider=auto constraint=minor\nFROM nginx:1.27\n",
		},
		{
			name:     "repairs a repository that has drifted from its line",
			original: "# clover: provider=docker repository=library/stale\nFROM nginx:1.27\n",
			want:     "# clover: provider=auto\nFROM nginx:1.27\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := annotateTree(t, map[string]string{"Dockerfile": tc.original})

			summary := annotate(t, root, true, true)
			require.Equal(t, 1, summary.Updated())
			require.Equal(t, 0, summary.Added())
			require.Equal(t, tc.want, readFile(t, filepath.Join(root, "Dockerfile")))
		})
	}
}

// TestAnnotateForceLeavesNonInferableProvider guards the blocker: --force must
// not rewrite a deliberate non-inferable directive (here provider=http with
// find/replace) into provider=auto just because its target line is docker-shaped
// - that would drop the keys it needs and break the marker.
func TestAnnotateForceLeavesNonInferableProvider(t *testing.T) {
	t.Parallel()

	original := "# clover: provider=http url=https://example.test/v jq=.version find=nginx:<version> replace=nginx:<version>\nFROM nginx:1.27\n"
	root := annotateTree(t, map[string]string{"Dockerfile": original})

	require.True(t, annotate(t, root, true, true).OK(),
		"a provider clover cannot infer from the line is never collapsed to auto")
	require.Equal(t, original, readFile(t, filepath.Join(root, "Dockerfile")))
}

// TestAnnotateSkipsMalformedReference guards the verify gate: a recognized shape
// whose inferred resource is invalid (a repository with whitespace) is not
// annotated, since lint would reject the resulting provider=auto.
func TestAnnotateSkipsMalformedReference(t *testing.T) {
	t.Parallel()

	original := "services:\n  app:\n    image: \"bad repo:1.2\"\n"
	root := annotateTree(t, map[string]string{"compose.yaml": original})

	require.True(t, annotate(t, root, true, false).OK(),
		"an image ref whose repository is invalid is not annotated")
	require.Equal(t, original, readFile(t, filepath.Join(root, "compose.yaml")))
}

// TestAnnotateSkipsCommentedOutLines guards against annotating documentation: a
// commented-out uses:/image: example is not a live field, so it is left alone.
func TestAnnotateSkipsCommentedOutLines(t *testing.T) {
	t.Parallel()

	original := "steps:\n" +
		"  # - uses: actions/checkout@" + sha + " # v4.1.0\n" +
		"  # image: nginx:1.27\n"
	root := annotateTree(t, map[string]string{"ci.yaml": original})

	require.True(t, annotate(t, root, true, false).OK(),
		"commented-out examples are not annotated")
	require.Equal(t, original, readFile(t, filepath.Join(root, "ci.yaml")))
}

func TestAnnotateForceNoOpWhenCanonical(t *testing.T) {
	t.Parallel()

	original := "# clover: provider=auto\nFROM nginx:1.27\n"
	root := annotateTree(t, map[string]string{"Dockerfile": original})

	require.True(t, annotate(t, root, true, true).OK(),
		"an already-canonical annotation needs no rewrite")
	require.Equal(t, original, readFile(t, filepath.Join(root, "Dockerfile")))
}

func TestAnnotateForceLeavesUnrecognizedUntouched(t *testing.T) {
	t.Parallel()

	original := "# clover: provider=manual id=x\nfoo_version: 1.2.3\n"
	root := annotateTree(t, map[string]string{"versions.yaml": original})

	require.True(t, annotate(t, root, true, true).OK(),
		"a line no route recognizes is left to its existing directive, even under --force")
	require.Equal(t, original, readFile(t, filepath.Join(root, "versions.yaml")))
}

// TestAnnotateSkipsUnlocatableLines confirms the verify-before-write gate: a
// line clover recognizes by shape but that carries no trackable version is never
// annotated, so annotate cannot emit a directive that lint would reject. A docker
// FROM with no tag has no anchor; an undocumented action pin, by contrast, IS
// locatable (run resolves and adds the comment), so it is annotated.
func TestAnnotateSkipsUnlocatableLines(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		"Dockerfile":                "FROM nginx\n",                               // no tag to track: skipped
		".github/workflows/ci.yaml": "    - uses: actions/checkout@" + sha + "\n", // undocumented pin: annotated
		"ok/Dockerfile":             "FROM redis:7.2\n",                           // a real version: annotated
	})

	summary := annotate(t, root, true, false)
	require.Equal(t, 2, summary.Added())
	require.Equal(t, "FROM nginx\n", readFile(t, filepath.Join(root, "Dockerfile")),
		"a docker line with no tag stays unannotated")
	require.Equal(t,
		"    # clover: provider=auto\n    - uses: actions/checkout@"+sha+"\n",
		readFile(t, filepath.Join(root, ".github/workflows/ci.yaml")),
		"an undocumented action pin is annotated; run will add its version comment")
	require.Equal(t,
		"# clover: provider=auto\nFROM redis:7.2\n",
		readFile(t, filepath.Join(root, "ok/Dockerfile")))
}

// TestAnnotateRecordsSkippedCandidates keeps the quiet-by-default diagnostics
// useful: a line that matched an annotate route but failed a safety gate records
// the reason for verbose reporting.
func TestAnnotateRecordsSkippedCandidates(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		"Dockerfile":   "FROM nginx\n",
		"compose.yaml": "services:\n  app:\n    image: \"bad repo:1.2\"\n",
	})

	summary := annotate(t, root, false, false)
	require.True(t, summary.OK(), "skipped candidates are diagnostics, not work")

	reasons := map[string]string{}
	for _, file := range summary.Files {
		if len(file.Skips) > 0 {
			reasons[filepath.Base(file.Path)] = file.Skips[0].Reason
		}
	}
	require.Contains(t, reasons["Dockerfile"], "image has no tag")
	require.Contains(t, reasons["compose.yaml"], "must not contain whitespace")
}

// TestAnnotateSkipsProseExamples guards the file-level route scope: a uses: pin
// inside a Markdown code fence is documentation, not a real workflow line, so
// annotate must leave it alone. The action-pin route is scoped to YAML; without
// that scope annotate would match the line by shape and, worse, insert an XML
// comment (Markdown's syntax) that turns the example into a live marker.
func TestAnnotateSkipsProseExamples(t *testing.T) {
	t.Parallel()

	original := "Pin an action:\n\n```yaml\n- uses: actions/checkout@" + sha + "\n```\n"
	root := annotateTree(t, map[string]string{"docs/actions.md": original})

	require.True(t, annotate(t, root, true, false).OK(),
		"a uses: example in Markdown prose is not a real target")
	require.Equal(t, original, readFile(t, filepath.Join(root, "docs/actions.md")))
}

func TestAnnotateHonorsIgnoreControls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		original string
		want     string
	}{
		{
			name:     "ignore next-line",
			original: "# clover:ignore\nFROM nginx:1.27\nFROM redis:7.2\n",
			want:     "# clover:ignore\nFROM nginx:1.27\n# clover: provider=auto\nFROM redis:7.2\n",
		},
		{
			name:     "ignore block",
			original: "# clover:ignore-start\nFROM nginx:1.27\n# clover:ignore-end\nFROM redis:7.2\n",
			want:     "# clover:ignore-start\nFROM nginx:1.27\n# clover:ignore-end\n# clover: provider=auto\nFROM redis:7.2\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := annotateTree(t, map[string]string{"Dockerfile": tc.original})

			require.Equal(t, 1, annotate(t, root, true, false).Added(),
				"only the un-ignored line is annotated")
			require.Equal(t, tc.want, readFile(t, filepath.Join(root, "Dockerfile")))
		})
	}
}
