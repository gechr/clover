package mode_test

// clover:ignore-file - the fixtures below embed clover: directives as test data,
// not real markers for clover to act on.

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/progress"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/docker"
	"github.com/gechr/clover/internal/provider/gitea"
	"github.com/gechr/clover/internal/provider/github"
	"github.com/gechr/clover/internal/provider/gitlab"
	"github.com/gechr/clover/internal/provider/golang"
	"github.com/gechr/clover/internal/provider/hashicorp"
	"github.com/gechr/clover/internal/provider/node"
	"github.com/gechr/clover/internal/provider/pypi"
	"github.com/gechr/clover/internal/provider/python"
	"github.com/gechr/clover/internal/provider/rust"
	"github.com/gechr/clover/internal/provider/swift"
	"github.com/gechr/clover/internal/provider/terraform"
	"github.com/gechr/clover/internal/provider/zig"
	"github.com/stretchr/testify/require"
)

// sha is a 40-hex commit, the shape the action-pin route keys on.
const sha = "a0dfaeb072753c3d48cd4df5fdacfd035b2281bf"

// testWorkers exercises the per-file concurrency path in the mode tests; results
// must be identical and correctly ordered regardless of worker count.
const testWorkers = 8

// TestMain registers the real providers annotate's verify gate validates
// inferred resources against. Their Resource parsing is offline (only Discover
// touches the network, which annotate never calls), so the tests stay hermetic.
// The node provider alone carries a canned transport, so the run --infer test
// can resolve an inferred marker end to end without the network.
func TestMain(m *testing.M) {
	provider.RegisterAll(
		docker.New(),
		gitea.New(),
		github.New(),
		gitlab.New(),
		golang.New(),
		hashicorp.New(),
		node.New(node.WithTransport(nodeIndex)),
		pypi.New(),
		python.New(),
		rust.New(),
		swift.New(),
		terraform.New(terraform.Terraform),
		terraform.New(terraform.OpenTofu),
		zig.New(),
	)
	os.Exit(m.Run())
}

// nodeIndex serves a tiny Node.js release index for every request, standing in
// for nodejs.org so inferred node markers resolve hermetically.
var nodeIndex = roundTripFunc(func(req *http.Request) (*http.Response, error) {
	const body = `[{"version":"v26.0.0","date":"2026-06-24","lts":false}]`
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
})

// roundTripFunc adapts a function to an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

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
		true,
		config.NewResolver(nil, "", false),
		progress.Nop{},
		testWorkers,
	)
	require.NoError(t, err)
	return summary
}

// annotateNoSidecar runs annotate with sidecar generation disabled.
func annotateNoSidecar(t *testing.T, root string, write bool) mode.AnnotateSummary {
	t.Helper()
	summary, err := mode.Annotate(
		context.Background(),
		[]string{root},
		write,
		false,
		false,
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
		true,
		config.NewResolver(nil, "", false),
		reporter,
		testWorkers,
	)
	require.NoError(t, err)
	require.NotEmpty(t, summary.Files)

	require.Equal(t, "Verifying annotation candidates", reporter.label)
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
		"jobs:\n  build:\n    steps:\n      # @clover\n      - uses: actions/checkout@"+sha+" # v4.1.0\n",
		readFile(t, filepath.Join(root, ".github/workflows/ci.yaml")),
		"the comment is inserted above the uses: line, indented to match it",
	)
	require.Equal(t,
		"# @clover\nFROM nginx:1.27\n",
		readFile(t, filepath.Join(root, "Dockerfile")))
	require.Equal(t,
		"services:\n  web:\n    # @clover\n    image: ghcr.io/owner/api:1.2.0\n",
		readFile(t, filepath.Join(root, "compose.yaml")))
}

// TestAnnotateReportsResource confirms an inserted annotation carries the tracked
// resource id and its landing URL, derived through the provider's Identifier
// capability, so the report can show a hyperlinked resource=. It runs serially and
// re-registers the real github provider, since a sibling run test overwrites it in
// the shared registry with a fake that names no resource.
func TestAnnotateReportsResource(t *testing.T) {
	provider.Register(github.New())

	root := annotateTree(t, map[string]string{
		".github/workflows/ci.yaml": "jobs:\n  build:\n    steps:\n      - uses: actions/checkout@v4\n",
	})

	summary := annotate(t, root, false, false)
	require.Len(t, summary.Files, 1)
	require.Len(t, summary.Files[0].Changes, 1)
	change := summary.Files[0].Changes[0]
	require.Equal(t, "github", change.Provider)
	require.Equal(t, "actions/checkout", change.Resource)
	require.Equal(t, "https://github.com/actions/checkout", change.ResourceURL)
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
		"jobs:\n  build:\n    steps:\n      # @clover\n      - uses: actions/checkout@v4\n",
		readFile(t, filepath.Join(root, ".github/workflows/ci.yaml")),
		"a tag-pinned uses: earns an annotation just like a SHA pin",
	)
}

func TestAnnotateInsertsForContainerJobUses(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		".github/workflows/ci.yaml": "jobs:\n  build:\n    steps:\n      - uses: docker://alpine:3.20\n",
	})

	summary := annotate(t, root, true, false)
	require.Equal(t, 1, summary.Added())

	require.Equal(
		t,
		"jobs:\n  build:\n    steps:\n      # @clover\n      - uses: docker://alpine:3.20\n",
		readFile(t, filepath.Join(root, ".github/workflows/ci.yaml")),
		"a container job's docker:// image earns an annotation",
	)
}

func TestAnnotateInsertsForGitLabComponent(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		".gitlab-ci.yml": "include:\n  - component: gitlab.com/components/opentofu/full-pipeline@2.0.1\n",
	})

	summary := annotate(t, root, true, false)
	require.Equal(t, 1, summary.Added())

	require.Equal(
		t,
		"include:\n  # @clover\n  - component: gitlab.com/components/opentofu/full-pipeline@2.0.1\n",
		readFile(t, filepath.Join(root, ".gitlab-ci.yml")),
		"a component include earns an annotation",
	)
}

func TestAnnotateInsertsForMiseTools(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		".mise.toml": "[tools]\n" +
			"terraform = \"1.9.8\"\n" +
			"node = \"24.11.0\"\n" +
			"tofu = \"1.8.5\"\n" +
			"\"ubi:owner/tool\" = \"1.2.3\"\n" +
			"go = \"1.23.2\"\n" +
			"zig = \"0.15.2\"\n" +
			"java = \"21.0.5\"\n",
	})

	summary := annotate(t, root, true, false)
	require.Equal(
		t,
		6,
		summary.Added(),
		"every recognized tool earns an annotation, java has no provider",
	)

	require.Equal(
		t,
		"[tools]\n"+
			"# @clover\n"+
			"terraform = \"1.9.8\"\n"+
			"# @clover\n"+
			"node = \"24.11.0\"\n"+
			"# @clover\n"+
			"tofu = \"1.8.5\"\n"+
			"# @clover\n"+
			"\"ubi:owner/tool\" = \"1.2.3\"\n"+
			"# @clover\n"+
			"go = \"1.23.2\"\n"+
			"# @clover\n"+
			"zig = \"0.15.2\"\n"+
			"java = \"21.0.5\"\n",
		readFile(t, filepath.Join(root, ".mise.toml")),
	)
}

func TestAnnotateInsertsForGoMod(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		"go.mod": "module github.com/owner/repo\n\ngo 1.23.2\n",
	})

	summary := annotate(t, root, true, false)
	require.Equal(t, 1, summary.Added())

	require.Equal(
		t,
		"module github.com/owner/repo\n\n// @clover\ngo 1.23.2\n",
		readFile(t, filepath.Join(root, "go.mod")),
		"the go directive earns a slash-comment annotation",
	)
}

func TestAnnotateInsertsForPyproject(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		"pyproject.toml": "[project]\n" +
			"requires-python = \">=3.13\"\n\n" +
			"[tool.ruff]\n" +
			"target-version = \"py313\"\n",
	})

	summary := annotate(t, root, true, false)
	require.Equal(t, 2, summary.Added())

	require.Equal(
		t,
		"[project]\n"+
			"# @clover\n"+
			"requires-python = \">=3.13\"\n\n"+
			"[tool.ruff]\n"+
			"# @clover\n"+
			"target-version = \"py313\"\n",
		readFile(t, filepath.Join(root, "pyproject.toml")),
		"the requires-python floor and the compact target-version both earn annotations",
	)
}

// TestAnnotateInsertsForPyprojectDependencies covers the pypi inference: each
// dependency specifier on its own line earns an annotation - a [project]
// dependency, a spaced specifier with a dotted name, and a [build-system]
// requires entry - while a single-line group listing several specifiers is
// skipped as ambiguous.
func TestAnnotateInsertsForPyprojectDependencies(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		"pyproject.toml": "[project]\n" +
			"dependencies = [\n" +
			"  \"pydantic>=2.12.5\",\n" +
			"  \"ruamel.yaml >= 0.19.1\",\n" +
			"]\n\n" +
			"[dependency-groups]\n" +
			"dev = [\"ruff>=0.15.2\", \"pytest>=9.0.3\"]\n\n" +
			"[build-system]\n" +
			"requires = [\"uv_build>=0.8.24\"]\n",
	})

	summary := annotate(t, root, true, false)
	require.Equal(t, 3, summary.Added())

	require.Equal(
		t,
		"[project]\n"+
			"dependencies = [\n"+
			"  # @clover\n"+
			"  \"pydantic>=2.12.5\",\n"+
			"  # @clover\n"+
			"  \"ruamel.yaml >= 0.19.1\",\n"+
			"]\n\n"+
			"[dependency-groups]\n"+
			"dev = [\"ruff>=0.15.2\", \"pytest>=9.0.3\"]\n\n"+
			"[build-system]\n"+
			"# @clover\n"+
			"requires = [\"uv_build>=0.8.24\"]\n",
		readFile(t, filepath.Join(root, "pyproject.toml")),
		"each single-specifier line earns an annotation, the multi-specifier group is left alone",
	)

	require.Len(t, summary.Files, 1)
	require.Len(t, summary.Files[0].Skips, 1)
	require.Equal(
		t,
		"multiple dependency specifiers, so it is ambiguous which to track",
		summary.Files[0].Skips[0].Reason,
	)
}

// TestAnnotateSkipsRequiresPythonRange guards the offline gate on the
// requires-python route: a range constraint carries two version tokens, so the
// smart rewriter cannot say which bound to bump and the line is skipped with
// the reason recorded.
func TestAnnotateSkipsRequiresPythonRange(t *testing.T) {
	t.Parallel()

	original := "[project]\nrequires-python = \">=3.10,<4\"\n"
	root := annotateTree(t, map[string]string{"pyproject.toml": original})

	summary := annotate(t, root, true, false)
	require.Equal(t, 0, summary.Added())
	require.Equal(t, original, readFile(t, filepath.Join(root, "pyproject.toml")),
		"an ambiguous range stays unannotated")

	require.Len(t, summary.Files, 1)
	require.Len(t, summary.Files[0].Skips, 1)
	require.Equal(
		t,
		"multiple version-shaped tokens, so the target is ambiguous",
		summary.Files[0].Skips[0].Reason,
	)
}

func TestAnnotateInsertsForDigestPinnedFloatingTag(t *testing.T) {
	t.Parallel()

	digest := strings.Repeat("0", 64)
	root := annotateTree(t, map[string]string{
		"Dockerfile": "FROM gcr.io/distroless/static:nonroot@sha256:" + digest + "\n",
	})

	summary := annotate(t, root, true, false)
	require.Equal(t, 1, summary.Added())

	require.Equal(
		t,
		"# @clover\nFROM gcr.io/distroless/static:nonroot@sha256:"+digest+"\n",
		readFile(t, filepath.Join(root, "Dockerfile")),
		"a digest pin on a floating tag earns an annotation, resolved as track",
	)
}

func TestAnnotateInsertsForTerraformRequiredVersion(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		"versions.tf": "terraform {\n  required_version = \"~> 1.11.0\"\n}\n",
	})

	summary := annotate(t, root, true, false)
	require.Equal(t, 1, summary.Added())

	require.Equal(
		t,
		"terraform {\n  # @clover\n  required_version = \"~> 1.11.0\"\n}\n",
		readFile(t, filepath.Join(root, "versions.tf")),
		"a required_version constraint earns an annotation",
	)
}

func TestAnnotateInsertsForRequiredProviders(t *testing.T) {
	t.Parallel()

	root := annotateTree(t, map[string]string{
		"versions.tf": "terraform {\n" +
			"  required_providers {\n" +
			"    aws = {\n" +
			"      source  = \"hashicorp/aws\"\n" +
			"      version = \"~> 6.39\"\n" +
			"    }\n" +
			"  }\n" +
			"}\n",
	})

	summary := annotate(t, root, true, false)
	require.Equal(t, 1, summary.Added())

	require.Equal(
		t,
		"terraform {\n"+
			"  required_providers {\n"+
			"    aws = {\n"+
			"      source  = \"hashicorp/aws\"\n"+
			"      # @clover\n"+
			"      version = \"~> 6.39\"\n"+
			"    }\n"+
			"  }\n"+
			"}\n",
		readFile(t, filepath.Join(root, "versions.tf")),
		"a required_providers version earns an annotation, sourced from its block",
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
		"# @clover\nFROM nginx:1.27\nRUN build\n# @clover\nFROM redis:7.2\n",
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
			want:     "# @clover: constraint=minor\nFROM nginx:1.27\n",
		},
		{
			name:     "repairs a repository that has drifted from its line",
			original: "# clover: provider=docker repository=library/stale\nFROM nginx:1.27\n",
			want:     "# @clover\nFROM nginx:1.27\n",
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

	original := "# @clover\nFROM nginx:1.27\n"
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
		"    # @clover\n    - uses: actions/checkout@"+sha+"\n",
		readFile(t, filepath.Join(root, ".github/workflows/ci.yaml")),
		"an undocumented action pin is annotated; run will add its version comment")
	require.Equal(t,
		"# @clover\nFROM redis:7.2\n",
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
	require.Equal(t, "image has no tag to anchor the version", reasons["Dockerfile"])
	require.Equal(
		t,
		`docker: "repository" must not contain whitespace, got "bad repo"`,
		reasons["compose.yaml"],
	)
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
			want:     "# clover:ignore\nFROM nginx:1.27\n# @clover\nFROM redis:7.2\n",
		},
		{
			name:     "ignore block",
			original: "# clover:ignore-start\nFROM nginx:1.27\n# clover:ignore-end\nFROM redis:7.2\n",
			want:     "# clover:ignore-start\nFROM nginx:1.27\n# clover:ignore-end\n# @clover\nFROM redis:7.2\n",
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
