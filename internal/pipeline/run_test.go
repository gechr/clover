package pipeline_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/manual"
	"github.com/gechr/clover/internal/version"
	"github.com/stretchr/testify/require"
)

// fakeProvider is a registered provider that returns canned candidates without
// touching the network, so a run resolves deterministically in tests.
type fakeProvider struct {
	name         string
	candidates   []model.Candidate
	err          error
	deep         *bool             // when set, Discover records whether a deep lookup was requested
	qualifier    *string           // when set, Discover records the qualifier hint
	tagPrefix    *string           // when set, Discover records the tag-prefix hint
	floor        *string           // when set, Discover records the version-floor hint
	truncate     bool              // when set, Discover reports a truncated lookup
	link         string            // when set, the Linker capability returns link+candidate.Ref
	digest       string            // the digest the Digester capability returns
	digestByTag  map[string]string // tag -> digest, overriding digest when the tag matches
	defaultBr    string            // the BranchChecker default branch
	branches     []provider.Branch // the BranchChecker branch list
	commitBranch map[string]string // commit SHA -> the branch it is reachable from
	tagCommit    map[string]string // tag -> commit, for the Committer fallback
}

func (f fakeProvider) Digest(_ context.Context, _ provider.Resource, tag string) (string, error) {
	if d, ok := f.digestByTag[tag]; ok {
		return d, nil
	}
	return f.digest, nil
}

func (f fakeProvider) Commit(_ context.Context, _ provider.Resource, tag string) (string, error) {
	return f.tagCommit[tag], nil
}

func (f fakeProvider) URL(_ provider.Resource, c model.Candidate) string {
	if f.link == "" {
		return ""
	}
	return f.link + c.Ref
}

func (f fakeProvider) DefaultBranch(context.Context, provider.Resource) (string, error) {
	return f.defaultBr, nil
}

func (f fakeProvider) Branches(context.Context, provider.Resource) ([]provider.Branch, error) {
	return f.branches, nil
}

func (f fakeProvider) Reachable(
	_ context.Context,
	_ provider.Resource,
	branch, commit string,
) (bool, error) {
	return f.commitBranch[commit] == branch, nil
}

func (f fakeProvider) Name() string { return f.name }

func (f fakeProvider) Keys() []provider.Key {
	return []provider.Key{{Name: "repository", Required: false}}
}

func (f fakeProvider) Resource(directive.Directive) (provider.Resource, error) {
	return f.name, nil
}

func (f fakeProvider) Describe(provider.Resource) string { return f.name }

func (f fakeProvider) Discover(
	ctx context.Context,
	_ provider.Resource,
) ([]model.Candidate, error) {
	if f.deep != nil {
		*f.deep = provider.Deep(ctx)
	}
	if f.qualifier != nil {
		*f.qualifier = provider.Qualifier(ctx)
	}
	if f.tagPrefix != nil {
		*f.tagPrefix = provider.TagPrefix(ctx)
	}
	if f.floor != nil {
		*f.floor = provider.VersionFloor(ctx)
	}
	if f.truncate {
		provider.NoteTruncated(ctx, f.name, "https://"+f.name)
	}
	return f.candidates, f.err
}

// recencyProvider is a fakeProvider that lists newest-first, so its truncation
// drives the gated per-marker --deep hint rather than the run-wide blanket sink.
type recencyProvider struct{ fakeProvider }

func (recencyProvider) RecencyOrdered() {}

func TestRunDeepReachesDiscover(t *testing.T) {
	var got bool
	provider.Register(fakeProvider{
		name:       "deepfake",
		deep:       &got,
		candidates: []model.Candidate{candidate(t, "1.3.0")},
	})

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=deepfake repository=x/y\nversion: 1.2.0\n",
	})

	_, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.False(t, got, "the default run is shallow")

	_, err = pipeline.Run(context.Background(), []string{dir}, pipeline.WithDeep(new(true)))
	require.NoError(t, err)
	require.True(t, got, "WithDeep(true) reaches the provider's Discover")
}

// The located tag's qualifier reaches Discover as a hint, so a provider can
// narrow discovery server-side; a plain tag or an explicit include - which can
// widen selection beyond the suffix - leaves the hint unset.
func TestRunQualifierHintReachesDiscover(t *testing.T) {
	var got string
	provider.Register(fakeProvider{
		name:      "qualhint",
		qualifier: &got,
		candidates: []model.Candidate{
			candidate(t, "1.3.0"),
			variantTag(t, "1.3.0-alpine"),
		},
	})

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=qualhint repository=x/y\nversion: 1.2.0-alpine\n",
	})
	_, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, "alpine", got, "a qualified tag hints its suffix")

	dir = write(t, map[string]string{
		"app.txt": "# clover: provider=qualhint repository=x/y\nversion: 1.2.0\n",
	})
	_, err = pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Empty(t, got, "a plain tag sets no hint")

	dir = write(t, map[string]string{
		"app.txt": "# clover: provider=qualhint repository=x/y include=*-alpine\n" +
			"version: 1.2.0-alpine\n",
	})
	_, err = pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Empty(t, got, "an explicit include clears the hint")
}

// The marker's tag-prefix reaches Discover as a hint, so a provider can narrow
// discovery server-side; a marker without one leaves the hint unset.
func TestRunTagPrefixHintReachesDiscover(t *testing.T) {
	var got string
	provider.Register(fakeProvider{
		name:      "prefixhint",
		tagPrefix: &got,
		candidates: []model.Candidate{
			{Version: "api/v1.5.0"},
			candidate(t, "1.3.0"),
		},
	})

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=prefixhint repository=x/y tag-prefix=api/\nversion: v1.4.0\n",
	})
	_, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, "api/", got, "a tag-prefix marker hints its prefix")

	dir = write(t, map[string]string{
		"app.txt": "# clover: provider=prefixhint repository=x/y\nversion: 1.2.0\n",
	})
	_, err = pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Empty(t, got, "no tag-prefix sets no hint")
}

// The current version reaches Discover as a floor hint when selection cannot
// pick below it, so a version-ordered provider can stop paging early; a
// downgrade rule or a tag-prefix suppresses the floor.
func TestRunVersionFloorHintReachesDiscover(t *testing.T) {
	var got string
	provider.Register(fakeProvider{
		name:  "floorhint",
		floor: &got,
		candidates: []model.Candidate{
			candidate(t, "1.3.0"),
			{Version: "api/v1.5.0"},
		},
	})

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=floorhint repository=x/y\nversion: 1.2.0\n",
	})
	_, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, "1.2.0", got, "the current version is the floor")

	dir = write(t, map[string]string{
		"app.txt": "# clover: provider=floorhint repository=x/y downgrade=true\nversion: 1.2.0\n",
	})
	_, err = pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Empty(t, got, "a directive downgrade rule suppresses the floor")

	dir = write(t, map[string]string{
		"app.txt": "# clover: provider=floorhint repository=x/y tag-prefix=api/\nversion: v1.4.0\n",
	})
	_, err = pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Empty(t, got, "a tag-prefix suppresses the floor")
}

// A repository's run.deep default drives a deep lookup for its own markers,
// without any CLI override - the per-root toggle path.
func TestRunConfigDeepReachesDiscover(t *testing.T) {
	var got bool
	provider.Register(fakeProvider{
		name:       "deepcfg",
		deep:       &got,
		candidates: []model.Candidate{candidate(t, "1.3.0")},
	})

	dir := writeRepo(t, "run:\n  deep: true\n", map[string]string{
		"app.txt": "# clover: provider=deepcfg repository=x/y\nversion: 1.2.0\n",
	})

	_, err := pipeline.Run(context.Background(), []string{dir},
		pipeline.WithConfig(config.NewResolver(nil, "", false)))
	require.NoError(t, err)
	require.True(t, got, "run.deep in the repo config drives a deep lookup")
}

// A tag-prefix scopes selection to one component of a monorepo and renders just
// the version: only api/* tags are considered, the newest wins, and the api/
// prefix is stripped on render (keeping the line's v prefix).
func TestRunTagPrefixSelectsComponent(t *testing.T) {
	provider.Register(fakeProvider{
		name: "monorepo",
		candidates: []model.Candidate{
			{Version: "api/v1.4.0"},
			{Version: "api/v1.5.0"},
			{Version: "worker/v2.0.0"},
			{Version: "web/v3.1.0"},
		},
	})

	dir := write(t, map[string]string{
		"versions.yaml": "# clover: provider=monorepo repository=x/y tag-prefix=api/\n" +
			"version: v1.4.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	r := files[0].Results[0]
	require.NoError(t, r.Err)
	require.Equal(t, "version: v1.5.0", r.NewLine,
		"newest api/ component, prefix stripped, v preserved - not worker/v2 or web/v3")
	require.Equal(t, "v1.5.0", r.Written)
}

// A provider implementing Linker has its resolved candidate's URL captured on
// the result, so the report can hyperlink the reported version; a provider that
// supplies no URL leaves it empty.
func TestRunResolvedURL(t *testing.T) {
	chosen := candidate(t, "1.3.0")
	chosen.Ref = "v1.3.0"
	provider.Register(fakeProvider{
		name:       "linkfake",
		link:       "https://example.test/releases/tag/",
		candidates: []model.Candidate{chosen},
	})
	provider.Register(fakeProvider{
		name:       "nolinkfake",
		candidates: []model.Candidate{candidate(t, "1.3.0")},
	})
	bare := candidate(t, "1.3.0")
	bare.Ref = "1.3.0"
	provider.Register(fakeProvider{
		name:       "barefake",
		link:       "https://example.test/releases/tag/",
		candidates: []model.Candidate{bare},
	})

	dir := write(t, map[string]string{
		"linked.txt":   "# clover: provider=linkfake repository=x/y\nversion: 1.2.0\n",
		"unlinked.txt": "# clover: provider=nolinkfake repository=x/y\nversion: 1.2.0\n",
		"bare.txt":     "# clover: provider=barefake repository=x/y\nversion: 1.2.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)

	byName := map[string]pipeline.Result{}
	for _, f := range files {
		byName[filepath.Base(f.Path)] = f.Results[0]
	}

	require.Equal(t, "https://example.test/releases/tag/v1.3.0", byName["linked.txt"].ResolvedURL)
	require.Empty(t, byName["unlinked.txt"].ResolvedURL, "no URL when the provider supplies none")

	// The from URL links the current version, inferring its ref's "v" prefix from
	// the resolved ref (v1.3.0) even though the line carries a bare 1.2.0.
	require.Equal(t, "https://example.test/releases/tag/v1.2.0", byName["linked.txt"].CurrentURL)
	require.Empty(t, byName["unlinked.txt"].CurrentURL, "no URL when the provider supplies none")

	// A prefixless resolved ref (1.3.0) infers a prefixless from ref (1.2.0).
	require.Equal(t, "https://example.test/releases/tag/1.2.0", byName["bare.txt"].CurrentURL)
}

func TestScanSkipsConfiguredExcludes(t *testing.T) {
	dir := writeRepo(t,
		"paths:\n  exclude:\n    - ignored/**\n    - \"[\"\n",
		map[string]string{
			"keep.yaml":         "# clover: provider=github repository=keep/repo\nversion: 1.0.0\n",
			"ignored/drop.yaml": "# clover: provider=github repository=drop/repo\nversion: 1.0.0\n",
		})
	t.Chdir(dir)

	files, _, err := pipeline.Scan(
		context.Background(),
		[]string{"."},
		pipeline.WithConfig(config.NewResolver(nil, "", false)),
	)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "keep.yaml", filepath.Base(files[0].Path))
}

func TestScanExcludeDoubleStarMatchesNestedDirs(t *testing.T) {
	dir := writeRepo(t,
		"paths:\n  exclude:\n    - \"**/generated/**\"\n",
		map[string]string{
			"src/service/keep.yaml": "# clover: provider=github repository=keep/repo\nversion: 1.0.0\n",
			"src/service/generated/drop.yaml": "# clover: provider=github repository=drop/repo\n" +
				"version: 1.0.0\n",
			"tools/build/service/generated/drop.yaml": "# clover: provider=github repository=drop/repo\n" +
				"version: 1.0.0\n",
		})
	t.Chdir(dir)

	files, _, err := pipeline.Scan(
		context.Background(),
		[]string{"."},
		pipeline.WithConfig(config.NewResolver(nil, "", false)),
	)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "keep.yaml", filepath.Base(files[0].Path))
}

// repoAt marks dir as a repository root carrying the given .clover.yaml and
// files, for tests spanning several repositories under one parent.
func repoAt(t *testing.T, dir, cloverYAML string, files map[string]string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".clover.yaml"), []byte(cloverYAML), 0o644))
	for name, body := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
}

const gateDirective = "# clover: provider=github repository=x/y\nversion: 1.0.0\n"

// A scan spanning several repositories under one parent skips a repository whose
// required-version the running clover does not satisfy, while its siblings are
// scanned normally.
func TestScanGatesUnsatisfiedRequiredVersion(t *testing.T) {
	parent := t.TempDir()
	repoAt(t, filepath.Join(parent, "ok"), "", map[string]string{"app.yaml": gateDirective})
	repoAt(t, filepath.Join(parent, "blocked"),
		"required-version: \">=9.0.0\"\n", map[string]string{"app.yaml": gateDirective})

	files, _, err := pipeline.Scan(
		context.Background(),
		[]string{parent},
		pipeline.WithConfig(config.NewResolver(nil, "", false)),
		pipeline.WithVersion("1.0.0"),
	)
	require.NoError(t, err)
	require.Len(t, files, 1, "the blocked repository's file is dropped")
	require.Equal(t, filepath.Join(parent, "ok", "app.yaml"), files[0].Path)
}

// A malformed project config is a hard error - a bug to fix - not a benign skip.
func TestScanRejectsMalformedConfig(t *testing.T) {
	dir := writeRepo(t,
		"required-version: \"not a constraint!!\"\n",
		map[string]string{"app.yaml": gateDirective})

	_, _, err := pipeline.Scan(
		context.Background(),
		[]string{dir},
		pipeline.WithConfig(config.NewResolver(nil, "", false)),
		pipeline.WithVersion("1.0.0"),
	)
	require.Error(t, err)
}

// A malformed config is rejected even in a repo carrying no directive file: the
// walk visits the bad .clover.yaml, so the resolver records the load error and
// the scan surfaces it rather than reporting "no comments" and exiting 0.
func TestScanRejectsMalformedConfigWithoutDirectives(t *testing.T) {
	dir := t.TempDir()
	repoAt(t, dir, "required-version: \"not a constraint!!\"\n", nil)

	_, _, err := pipeline.Scan(
		context.Background(),
		[]string{dir},
		pipeline.WithConfig(config.NewResolver(nil, "", false)),
		pipeline.WithVersion("1.0.0"),
	)
	require.Error(t, err)
}

func TestRunNoCandidateIsSentinel(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "nomatch",
		candidates: []model.Candidate{candidate(t, "3.0.0")},
	})

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=nomatch repository=x/y constraint=minor\nversion: 1.2.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	resErr := files[0].Results[0].Err
	require.ErrorIs(t, resErr, pipeline.ErrNoCandidate,
		"a minor-ceiling constraint rejects the only candidate (a major bump)")
	require.EqualError(
		t,
		resErr,
		"no candidate satisfies the rule: no version satisfies the constraint",
		"the error names the dominant rejection reason",
	)
}

func TestRunMalformedDirectiveErrors(t *testing.T) {
	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=\"unterminated\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Len(t, files[0].Results, 1)
	require.Error(t, files[0].Results[0].Err)
	require.Equal(t, 0, files[0].Results[0].Marker.Line)
	require.Equal(t, 0, files[0].Results[0].Marker.Target)
}

func TestRunTruncationSinkReceivesNotices(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "trunc",
		truncate:   true,
		candidates: []model.Candidate{candidate(t, "1.3.0")},
	})

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=trunc repository=x/y\nversion: 1.2.0\n",
	})

	var noted []provider.Truncation
	files, err := pipeline.Run(context.Background(), []string{dir},
		pipeline.WithTruncationSink(func(t provider.Truncation) { noted = append(noted, t) }))
	require.NoError(t, err)
	require.Equal(t, []provider.Truncation{{Resource: "trunc", URL: "https://trunc"}}, noted)
	require.False(t, files[0].Results[0].Truncated,
		"a lexically-ordered provider feeds the blanket sink, not the per-marker flag")
}

// TestRunRecencyTruncationGatesNoCandidate verifies a recency-ordered provider's
// truncation is recorded per-marker (for the gated --deep hint) and kept out of
// the run-wide blanket sink.
func TestRunRecencyTruncationGatesNoCandidate(t *testing.T) {
	provider.Register(recencyProvider{fakeProvider{
		name:       "recent",
		truncate:   true,
		candidates: []model.Candidate{candidate(t, "3.0.0")}, // a major bump
	}})

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=recent repository=x/y constraint=minor\nversion: 1.2.0\n",
	})

	var noted []provider.Truncation
	files, err := pipeline.Run(context.Background(), []string{dir},
		pipeline.WithTruncationSink(func(t provider.Truncation) { noted = append(noted, t) }))
	require.NoError(t, err)

	r := files[0].Results[0]
	require.ErrorIs(t, r.Err, pipeline.ErrNoCandidate,
		"the minor ceiling rejects the only candidate, a major bump")
	require.True(t, r.Truncated,
		"a recency-ordered truncated lookup records truncation per-marker")
	require.Empty(t, noted,
		"a recency-ordered source does not feed the blanket truncation sink")
}

func TestRunResolvesDigestPin(t *testing.T) {
	oldDigest := "sha256:" + strings.Repeat("a", 64)
	newDigest := "sha256:" + strings.Repeat("b", 64)
	provider.Register(fakeProvider{
		name:       "docker",
		digest:     newDigest,
		candidates: []model.Candidate{candidate(t, "1.2.0")},
	})

	dir := write(t, map[string]string{
		"Dockerfile": "# clover: provider=docker repository=x/y\nFROM x/y:1.0.0@" + oldDigest + "\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	r := files[0].Results[0]
	require.NoError(t, r.Err)
	require.True(t, r.Changed)
	require.Equal(t, "FROM x/y:1.2.0@"+newDigest, r.NewLine,
		"tag and digest update together")
}

// variantTag builds a candidate for a variant tag (1.31.2-alpine), whose numeric
// core is what parses - mirroring how the docker provider splits the variant off
// before parsing the semver.
func variantTag(t *testing.T, tag string) model.Candidate {
	t.Helper()
	base, _ := version.SplitVariant(tag)
	semver, err := version.Parse(base)
	require.NoError(t, err)
	return model.Candidate{Version: tag, Semver: semver}
}

// A plain target line must not wander onto a variant tag, even when the variant
// carries a strictly higher version: variantInclude keeps selection on the
// located tag's shape, so a bare line picks plain 1.31.2 over the newer
// 1.32.0-alpine - which would otherwise render plain (1.32.0) while pinning the
// alpine digest. The higher variant version is what isolates this from the
// equal-version tie-break. Regression test for the tag/digest mismatch.
func TestRunPlainTagDoesNotPickVariant(t *testing.T) {
	plainDigest := "sha256:" + strings.Repeat("c", 64)
	alpineDigest := "sha256:" + strings.Repeat("d", 64)
	provider.Register(fakeProvider{
		name: "docker",
		digestByTag: map[string]string{
			"1.31.2":        plainDigest,
			"1.32.0-alpine": alpineDigest,
		},
		candidates: []model.Candidate{
			candidate(t, "1.31.2"),
			variantTag(t, "1.32.0-alpine"),
		},
	})

	old := "sha256:" + strings.Repeat("a", 64)
	dir := write(t, map[string]string{
		"Dockerfile": "# clover: provider=docker repository=x/y\nFROM x/y:1.0.0@" + old + "\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	r := files[0].Results[0]
	require.NoError(t, r.Err)
	require.Equal(t, "FROM x/y:1.31.2@"+plainDigest, r.NewLine,
		"plain line stays plain over a newer variant, with the plain tag's digest")
}

// A rogue upstream tag that is not version-shaped (norwoodj/helm-docs once
// published a 19.0614) parses and orders above every real version, but restyle
// cannot write it faithfully: it would pad the tag to 19.0614.0 - a version
// that exists nowhere - and the next run's Locate would reject the line.
// Regression test that selection passes over it to the real latest.
func TestRunSkipsUnshapedTag(t *testing.T) {
	provider.Register(fakeProvider{
		name: "rogue",
		candidates: []model.Candidate{
			candidate(t, "19.0614"),
			candidate(t, "1.14.3"),
		},
	})

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=rogue repository=x/y\nhelm-docs 1.14.2\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	r := files[0].Results[0]
	require.NoError(t, r.Err)
	require.Equal(t, "helm-docs 1.14.3", r.NewLine,
		"the unshaped 19.0614 tag is passed over for the real latest")
}

// When an explicit include forces a variant the plain line lacks, restyle still
// renders the plain tag - so the digest must be resolved for that rendered tag,
// not the raw selected candidate. Regression test that the pinned digest always
// describes the tag actually written.
func TestRunDigestFollowsRenderedTag(t *testing.T) {
	plainDigest := "sha256:" + strings.Repeat("c", 64)
	alpineDigest := "sha256:" + strings.Repeat("d", 64)
	provider.Register(fakeProvider{
		name: "docker",
		digestByTag: map[string]string{
			"1.31.2":        plainDigest,
			"1.31.2-alpine": alpineDigest,
		},
		candidates: []model.Candidate{variantTag(t, "1.31.2-alpine")},
	})

	old := "sha256:" + strings.Repeat("a", 64)
	dir := write(t, map[string]string{
		"Dockerfile": "# clover: provider=docker repository=x/y include=*-alpine\n" +
			"FROM x/y:1.0.0@" + old + "\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	r := files[0].Results[0]
	require.NoError(t, r.Err)
	require.Equal(t, "FROM x/y:1.31.2@"+plainDigest, r.NewLine,
		"digest matches the rendered plain tag, not the alpine candidate")
	require.Equal(t, "1.31.2-alpine", r.Resolved, "resolved keeps the raw candidate")
	require.Equal(
		t,
		"1.31.2",
		r.Written,
		"written is the rendered plain tag, what the report shows",
	)
}

func TestRunResolvesSha256Follower(t *testing.T) {
	sum := strings.Repeat("e", 64)
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, sum+"  tool_1.2.0_linux_amd64.tar.gz\n"+
			strings.Repeat("f", 64)+"  tool_1.2.0_windows.zip\n")
	}))
	t.Cleanup(srv.Close)

	provider.Register(
		fakeProvider{name: "fake", candidates: []model.Candidate{candidate(t, "1.2.0")}},
	)

	old := strings.Repeat("a", 64)
	dir := write(t, map[string]string{
		"versions.env": "# clover: provider=fake id=tool repository=x/y\n" +
			"VERSION=1.0.0\n" +
			"# clover: from=tool value=sha256 sha256-url=" + srv.URL +
			"/v<version>/checksums.txt pattern=*linux_amd64*\n" +
			"SHA256=" + old + "\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)

	var follower pipeline.Result
	for _, r := range files[0].Results {
		if strings.HasPrefix(r.NewLine, "SHA256=") {
			follower = r
		}
	}
	require.NoError(t, follower.Err)
	require.Equal(
		t,
		"SHA256="+sum,
		follower.NewLine,
		"the follower fetched the producer version's sha256",
	)
	require.Equal(t, "/v1.2.0/checksums.txt", gotPath, "<version> is the bare producer version")
}

// sha256FollowerEnv writes a producer pinned at version, followed by a sha256
// side value already holding have, against a checksum server returning serve.
// hit reports whether the server was reached.
func sha256FollowerEnv(t *testing.T, version, have, serve string) (string, *bool) {
	t.Helper()
	reached := new(false)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*reached = true
		_, _ = io.WriteString(w, serve+"  tool_linux_amd64.tar.gz\n")
	}))
	t.Cleanup(srv.Close)

	dir := write(t, map[string]string{
		"versions.env": "# clover: provider=holdfake id=tool repository=x/y\n" +
			"VERSION=" + version + "\n" +
			"# clover: from=tool value=sha256 sha256-url=" + srv.URL +
			"/v<version>/checksums.txt pattern=*linux_amd64*\n" +
			"SHA256=" + have + "\n",
	})
	return dir, reached
}

// follower returns the SHA256= result from a single-file run.
func sha256Result(t *testing.T, files []pipeline.FileResult) pipeline.Result {
	t.Helper()
	for _, r := range files[0].Results {
		if strings.HasPrefix(r.NewLine, "SHA256=") {
			return r
		}
	}
	t.Fatal("no SHA256= follower result")
	return pipeline.Result{}
}

func TestRunHoldsSha256WhenVersionUnchanged(t *testing.T) {
	provider.Register(
		fakeProvider{name: "holdfake", candidates: []model.Candidate{candidate(t, "1.2.0")}},
	)

	old := strings.Repeat("a", 64)
	dir, hit := sha256FollowerEnv(t, "1.2.0", old, strings.Repeat("b", 64))

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)

	follower := sha256Result(t, files)
	require.NoError(t, follower.Err)
	require.False(t, follower.Skipped, "the producer resolved, so the follower ran the hold path")
	require.False(t, follower.Changed, "an unchanged version holds its pinned digest")
	require.Equal(t, "SHA256="+old, follower.NewLine)
	require.False(t, *hit, "the held digest is not even fetched")
}

func TestRunForceRepinsSha256WhenVersionUnchanged(t *testing.T) {
	provider.Register(
		fakeProvider{name: "holdfake", candidates: []model.Candidate{candidate(t, "1.2.0")}},
	)

	old, moved := strings.Repeat("a", 64), strings.Repeat("b", 64)
	dir, hit := sha256FollowerEnv(t, "1.2.0", old, moved)

	files, err := pipeline.Run(context.Background(), []string{dir}, pipeline.WithForce(new(true)))
	require.NoError(t, err)

	follower := sha256Result(t, files)
	require.NoError(t, follower.Err)
	require.True(t, follower.Changed, "--force re-pins even when the version is unchanged")
	require.Equal(t, "SHA256="+moved, follower.NewLine)
	require.True(t, *hit)
}

func TestRunPopulatesSha256PlaceholderWhenVersionUnchanged(t *testing.T) {
	provider.Register(
		fakeProvider{name: "holdfake", candidates: []model.Candidate{candidate(t, "1.2.0")}},
	)

	sum := strings.Repeat("e", 64)
	dir, _ := sha256FollowerEnv(t, "1.2.0", strings.Repeat("0", 64), sum)

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)

	follower := sha256Result(t, files)
	require.NoError(t, follower.Err)
	require.True(t, follower.Changed, "an all-zero placeholder is unpinned and populates")
	require.Equal(t, "SHA256="+sum, follower.NewLine)
}

func TestRunHoldsSha256AcrossVersionPrefix(t *testing.T) {
	// The line carries a "v" prefix while the candidate is bare: the hold must
	// normalize the prefix, or a raw == would treat them as a version change.
	provider.Register(
		fakeProvider{name: "holdfake", candidates: []model.Candidate{candidate(t, "1.2.0")}},
	)

	old := strings.Repeat("a", 64)
	dir, hit := sha256FollowerEnv(t, "v1.2.0", old, strings.Repeat("b", 64))

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)

	follower := sha256Result(t, files)
	require.NoError(t, follower.Err)
	require.False(t, follower.Changed, "v1.2.0 and 1.2.0 are the same version")
	require.Equal(t, "SHA256="+old, follower.NewLine)
	require.False(t, *hit)
}

func TestRunRefreshesSha256ForAnchoredProducer(t *testing.T) {
	// A manual (anchored) producer publishes Old==New unconditionally, so the
	// hold must not apply or a deliberate manual bump would never re-pin.
	provider.Register(manual.New())

	sum := strings.Repeat("e", 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, sum+"  tool_linux_amd64.tar.gz\n")
	}))
	t.Cleanup(srv.Close)

	old := strings.Repeat("a", 64)
	dir := write(t, map[string]string{
		"versions.env": "# clover: provider=manual id=tool\n" +
			"VERSION=1.2.0\n" +
			"# clover: from=tool value=sha256 sha256-url=" + srv.URL +
			"/v<version>/checksums.txt pattern=*linux_amd64*\n" +
			"SHA256=" + old + "\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)

	follower := sha256Result(t, files)
	require.NoError(t, follower.Err)
	require.True(t, follower.Changed, "a manual producer's digest still refreshes")
	require.Equal(t, "SHA256="+sum, follower.NewLine)
}

func TestRunHoldsCommitWhenVersionUnchanged(t *testing.T) {
	const moved = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	pinned := strings.Repeat("a", 40)
	// The producer's tag was force-moved to a new commit while its version held.
	register := func() {
		provider.Register(fakeProvider{name: "holdcommit", candidates: []model.Candidate{
			{Version: "2.0.0", Semver: mustSemver(t, "2.0.0"), Commit: moved},
		}})
	}
	env := func() string {
		return write(t, map[string]string{
			"a.txt": "# clover: provider=holdcommit repository=x/y id=app\nlead: 2.0.0\n",
			"b.txt": "# clover: from=app value=commit find=pin-<commit>\n" +
				"image: pin-" + pinned + "\n",
		})
	}

	t.Run("held", func(t *testing.T) {
		register()
		files, err := pipeline.Run(context.Background(), []string{env()})
		require.NoError(t, err)
		require.Equal(t, "image: pin-"+pinned, files[1].Results[0].NewLine,
			"an unchanged version holds the committed pin")
		require.False(t, files[1].Results[0].Changed)
		require.Equal(t, moved, files[1].Results[0].Moved,
			"a moved tag under a held pin is surfaced for warning")
	})

	t.Run("force", func(t *testing.T) {
		register()
		files, err := pipeline.Run(
			context.Background(),
			[]string{env()},
			pipeline.WithForce(new(true)),
		)
		require.NoError(t, err)
		require.Equal(t, "image: pin-"+moved, files[1].Results[0].NewLine,
			"--force re-pins the moved commit")
		require.Empty(t, files[1].Results[0].Moved,
			"a re-pinned commit reports no held move")
	})
}

// A floating producer (track=) is expected to move, so a held follower of it
// reports no move - the warning is reserved for an unexpectedly force-moved pin.
func TestRunHeldFloatingCommitDoesNotWarn(t *testing.T) {
	const moved = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	pinned := strings.Repeat("a", 40)
	provider.Register(fakeProvider{
		name:      "floatcommit",
		tagCommit: map[string]string{"main": moved},
	})
	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=floatcommit repository=x/y id=app track=main\nlead: main\n",
		"b.txt": "# clover: from=app value=commit find=pin-<commit>\n" +
			"image: pin-" + pinned + "\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, "image: pin-"+pinned, files[1].Results[0].NewLine,
		"a floating producer still holds the committed pin")
	require.Empty(t, files[1].Results[0].Moved,
		"a tracked ref's expected move is not warned")
}

func TestRunResolvesSha256FromAssetDigest(t *testing.T) {
	sum := strings.Repeat("e", 64)
	cand := candidate(t, "1.2.0")
	cand.Assets = []model.Asset{
		{Name: "tool_1.2.0_linux_amd64.tar.gz", Digest: "sha256:" + sum},
		{Name: "tool_1.2.0_windows.zip", Digest: "sha256:" + strings.Repeat("f", 64)},
	}
	provider.Register(fakeProvider{name: "assetfake", candidates: []model.Candidate{cand}})

	old := strings.Repeat("a", 64)
	dir := write(t, map[string]string{
		"versions.env": "# clover: provider=assetfake id=tool repository=x/y\n" +
			"VERSION=1.0.0\n" +
			"# clover: from=tool value=sha256 pattern=*linux_amd64*\n" +
			"SHA256=" + old + "\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)

	var follower pipeline.Result
	for _, r := range files[0].Results {
		if strings.HasPrefix(r.NewLine, "SHA256=") {
			follower = r
		}
	}
	require.NoError(t, follower.Err)
	require.Equal(t, "SHA256="+sum, follower.NewLine,
		"auto sources the asset digest with no sha256-url and no fetch")
}

func TestRunFindReplace(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "frfake",
		candidates: []model.Candidate{candidate(t, "1.3.0")},
	})

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=frfake repository=x/y find=tool-<version>-linux\n" +
			"image: tool-1.2.0-linux\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	r := files[0].Results[0]
	require.NoError(t, r.Err)
	require.True(t, r.Changed)
	require.Equal(t, "image: tool-1.3.0-linux", r.NewLine,
		"find locates the version; in-place render keeps the literal context")
}

func TestRunFollowerCommitFindReplace(t *testing.T) {
	const commit = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	provider.Register(fakeProvider{
		name: "leadcommit",
		candidates: []model.Candidate{
			{Version: "2.0.0", Semver: mustSemver(t, "2.0.0"), Commit: commit},
		},
	})

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=leadcommit repository=x/y id=app\nlead: 1.0.0\n",
		"b.txt": "# clover: from=app value=commit find=pin-<commit>\n" +
			"image: pin-0000000000000000000000000000000000000000\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 2)

	// The follower's <commit> token resolves from the typed candidate, not the
	// captured literal: without followerCandidate the commit would not splice.
	require.Equal(t, "image: pin-"+commit, files[1].Results[0].NewLine)
}

func TestRunFollowerVersionComponentFindReplace(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "leadver",
		candidates: []model.Candidate{candidate(t, "2.4.7")},
	})

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=leadver repository=x/y id=app\nlead: 1.0.0\n",
		"b.txt": "# clover: from=app value=version find=series-<major.minor>\n" +
			"image: series-1.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 2)

	// <major.minor> needs the resolved value parsed back to semver - the default
	// branch of followerCandidate does that, so the component token resolves.
	require.Equal(t, "image: series-2.4", files[1].Results[0].NewLine)
}

// mustSemver parses tag or fails the test.
func mustSemver(t *testing.T, tag string) *version.Version {
	t.Helper()
	v, err := version.Parse(tag)
	require.NoError(t, err)
	return v
}

func TestRunDockerTagAnchorsOnImageRef(t *testing.T) {
	// A fake registered as "docker" routes the FROM/image: lines to DockerTag.
	provider.Register(fakeProvider{
		name:       "docker",
		candidates: []model.Candidate{candidate(t, "2.1.0")},
	})

	tests := []struct {
		name string
		file string
		line string
		want string
	}{
		{
			name: "ported registry",
			file: "Dockerfile",
			line: "FROM localhost:5000/team/api:2.0.1",
			want: "FROM localhost:5000/team/api:2.1.0",
		},
		{
			name: "ecr registry host with account id and region",
			file: "deploy.yaml",
			line: "  image: 123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api:2.0.1",
			want: "  image: 123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api:2.1.0",
		},
		{
			name: "plain hub image",
			file: "Dockerfile",
			line: "FROM library/api:2.0.1",
			want: "FROM library/api:2.1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := write(t, map[string]string{
				tt.file: "# clover: provider=docker repository=team/api\n" + tt.line + "\n",
			})

			files, err := pipeline.Run(context.Background(), []string{dir})
			require.NoError(t, err)
			r := files[0].Results[0]
			require.NoError(
				t,
				r.Err,
				"the image ref anchors the tag, so the registry is not ambiguous",
			)
			require.Equal(t, tt.want, r.NewLine)
		})
	}
}

func TestRunDockerVariantSelectsSameVariant(t *testing.T) {
	// Upstream mixes bare and -alpine tags. A marker on an -alpine image must
	// pick the newest -alpine, never a bare 1.29 (which would render 1.29-alpine,
	// a tag that may not exist).
	provider.Register(fakeProvider{
		name: "docker",
		candidates: []model.Candidate{
			dockerCandidate(t, "1.27-alpine"),
			dockerCandidate(t, "1.29-alpine"),
			dockerCandidate(t, "1.29"),
			dockerCandidate(t, "1.30"),
		},
	})

	dir := write(t, map[string]string{
		"Dockerfile": "# clover: provider=docker repository=library/nginx\nFROM nginx:1.27-alpine\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	r := files[0].Results[0]
	require.NoError(t, r.Err)
	require.Equal(t, "FROM nginx:1.29-alpine", r.NewLine,
		"the variant include keeps only -alpine tags and orders them by core")
}

func TestRunDockerVariantNoMatchFailsLoud(t *testing.T) {
	// Only bare tags upstream, but the marker is on an -alpine image: rather than
	// bump to a non-existent 1.29-alpine, selection finds no candidate.
	provider.Register(fakeProvider{
		name: "docker",
		candidates: []model.Candidate{
			dockerCandidate(t, "1.29"),
			dockerCandidate(t, "1.30"),
		},
	})

	dir := write(t, map[string]string{
		"Dockerfile": "# clover: provider=docker repository=library/nginx\nFROM nginx:1.27-alpine\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.ErrorIs(t, files[0].Results[0].Err, pipeline.ErrNoCandidate,
		"no -alpine candidate exists, so selection fails loud rather than guess")
}

// dockerCandidate parses tag the way the docker provider does: a variant suffix
// is stripped before parsing so the tag orders by its numeric core.
func dockerCandidate(t *testing.T, tag string) model.Candidate {
	t.Helper()
	base, _ := version.SplitVariant(tag)
	semver, err := version.Parse(base)
	require.NoError(t, err)
	return model.Candidate{Version: tag, Semver: semver}
}

func TestRunVerifiesActionPinCommit(t *testing.T) {
	const good = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const bad = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	cand := candidate(t, "1.0.0")
	cand.Commit = good
	provider.Register(fakeProvider{name: "github", candidates: []model.Candidate{cand}})

	t.Run("mismatch", func(t *testing.T) {
		dir := write(t, map[string]string{
			".github/workflows/ci.yml": "# clover: provider=github repository=actions/checkout\n" +
				"  - uses: actions/checkout@" + bad + " # v1.0.0\n",
		})
		files, err := pipeline.Run(context.Background(), []string{dir})
		require.NoError(t, err)
		require.EqualError(t, files[0].Results[0].Verify,
			"pinned "+bad+" but 1.0.0 upstream is "+good)
	})

	t.Run("match", func(t *testing.T) {
		dir := write(t, map[string]string{
			".github/workflows/ci.yml": "# clover: provider=github repository=actions/checkout\n" +
				"  - uses: actions/checkout@" + good + " # v1.0.0\n",
		})
		files, err := pipeline.Run(context.Background(), []string{dir})
		require.NoError(t, err)
		require.NoError(t, files[0].Results[0].Verify, "a matching pin verifies clean")
	})
}

func TestRunVerifiesDigestPin(t *testing.T) {
	newDigest := "sha256:" + strings.Repeat("b", 64)
	provider.Register(fakeProvider{
		name:       "docker",
		digest:     newDigest,
		candidates: []model.Candidate{candidate(t, "1.0.0")},
	})

	// Up-to-date tag, but the committed digest differs from the registry's.
	oldDigest := "sha256:" + strings.Repeat("a", 64)
	dir := write(t, map[string]string{
		"Dockerfile": "# clover: provider=docker repository=x/y\nFROM x/y:1.0.0@" + oldDigest + "\n",
	})
	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.EqualError(t, files[0].Results[0].Verify,
		"pinned "+oldDigest+" but 1.0.0 upstream is "+newDigest)
}

func TestRunVerifySkippedOnBump(t *testing.T) {
	const good = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const old = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	cand := candidate(t, "2.0.0")
	cand.Commit = good
	provider.Register(fakeProvider{name: "github", candidates: []model.Candidate{cand}})

	// A version bump rewrites the pin from the chosen tag, so the old SHA is not
	// verified - the new pin is correct by construction.
	dir := write(t, map[string]string{
		".github/workflows/ci.yml": "# clover: provider=github repository=actions/checkout\n" +
			"  - uses: actions/checkout@" + old + " # v1.0.0\n",
	})
	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	r := files[0].Results[0]
	require.NoError(t, r.Verify, "a bump does not verify the superseded pin")
	require.True(t, r.Changed)
}

func TestRunVerifyBranchOnTrunk(t *testing.T) {
	const good = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const old = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	cand := candidate(t, "2.0.0")
	cand.Commit = good
	provider.Register(fakeProvider{
		name:         "github",
		candidates:   []model.Candidate{cand},
		defaultBr:    "main",
		commitBranch: map[string]string{good: "main"},
	})

	dir := write(t, map[string]string{
		".github/workflows/ci.yml": "# clover: provider=github repository=actions/checkout\n" +
			"  - uses: actions/checkout@" + old + " # v1.0.0\n",
	})
	on := true
	files, err := pipeline.Run(context.Background(), []string{dir}, pipeline.WithVerify(&on))
	require.NoError(t, err)
	require.NoError(t, files[0].Results[0].Verify, "the resolved commit is on the default branch")
}

func TestRunVerifyBranchOffTrunk(t *testing.T) {
	const good = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const old = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	cand := candidate(t, "2.0.0")
	cand.Commit = good
	provider.Register(fakeProvider{
		name:         "github",
		candidates:   []model.Candidate{cand},
		defaultBr:    "main",
		commitBranch: map[string]string{good: "feature"}, // not the default branch
	})

	dir := write(t, map[string]string{
		".github/workflows/ci.yml": "# clover: provider=github repository=actions/checkout\n" +
			"  - uses: actions/checkout@" + old + " # v1.0.0\n",
	})
	on := true
	files, err := pipeline.Run(context.Background(), []string{dir}, pipeline.WithVerify(&on))
	require.NoError(t, err)
	require.EqualError(t, files[0].Results[0].Verify,
		"commit "+good+" for 2.0.0 is not on an allowed branch")
}

func TestRunVerifyBranchGlobDirective(t *testing.T) {
	const good = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const old = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	cand := candidate(t, "2.0.0")
	cand.Commit = good
	provider.Register(fakeProvider{
		name:         "github",
		candidates:   []model.Candidate{cand},
		defaultBr:    "main",
		branches:     []provider.Branch{{Name: "main"}, {Name: "release-1.0"}},
		commitBranch: map[string]string{good: "release-1.0"}, // a release branch, not main
	})

	// verify-branch= enables the deep check (no run flag) and widens the allowed
	// set to release branches; the value is a glob by default, like include.
	dir := write(t, map[string]string{
		".github/workflows/ci.yml": "# clover: provider=github repository=x/y verify-branch=release-*\n" +
			"  - uses: x/y@" + old + " # v1.0.0\n",
	})
	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.NoError(t, files[0].Results[0].Verify, "the glob admits the release branch")
}

func TestRunVerifyBranchRegexDirective(t *testing.T) {
	const good = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const old = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	cand := candidate(t, "2.0.0")
	cand.Commit = good
	provider.Register(fakeProvider{
		name:         "github",
		candidates:   []model.Candidate{cand},
		defaultBr:    "main",
		branches:     []provider.Branch{{Name: "main"}, {Name: "release-1.0"}},
		commitBranch: map[string]string{good: "release-1.0"},
	})

	// A /regex/ value is matched as a regex, like include.
	dir := write(t, map[string]string{
		".github/workflows/ci.yml": "# clover: provider=github repository=x/y verify-branch=/release-[0-9.]+/\n" +
			"  - uses: x/y@" + old + " # v1.0.0\n",
	})
	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.NoError(t, files[0].Results[0].Verify, "the regex admits the release branch")
}

// candidate parses tag into a candidate the selection chain can order.
func candidate(t *testing.T, tag string) model.Candidate {
	t.Helper()
	semver, err := version.Parse(tag)
	require.NoError(t, err)
	return model.Candidate{Version: tag, Semver: semver}
}

// write lays out files under a fresh temp dir and returns its path.
func write(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	return dir
}

// writeRepo writes files into a temp dir marked as a repository root (a .git
// marker) and carrying a .clover.yaml of the given project config, so a per-root
// config resolver governs the scanned tree.
func writeRepo(t *testing.T, cloverYAML string, files map[string]string) string {
	t.Helper()
	files[".git"] = "gitdir: .\n"
	files[".clover.yaml"] = cloverYAML
	return write(t, files)
}

func TestRunResolvesProducer(t *testing.T) {
	provider.Register(fakeProvider{
		name: "fake",
		candidates: []model.Candidate{
			candidate(t, "1.2.0"),
			candidate(t, "1.3.0"),
			candidate(t, "1.2.5"),
		},
	})

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=fake repository=x/y\nversion: 1.2.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)

	results := files[0].Results
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	require.False(t, results[0].Skipped)
	require.Equal(t, "1.2.0", results[0].Current)
	require.Equal(t, "1.3.0", results[0].Resolved)
	require.True(t, results[0].Changed)
	require.Equal(t, "version: 1.3.0", results[0].NewLine)

	require.Equal(
		t,
		[]string{"# clover: provider=fake repository=x/y", "version: 1.3.0", ""},
		files[0].Rewritten(),
	)
}

func TestRunPreservesStyle(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "styled",
		candidates: []model.Candidate{candidate(t, "1.4.0")},
	})

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=styled repository=x/y\nimage: v1.2\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)

	// v-prefix and two-component precision are preserved from the target line.
	require.Equal(t, "image: v1.4", files[0].Results[0].NewLine)
}

func TestRunFollower(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "lead",
		candidates: []model.Candidate{candidate(t, "2.0.0")},
	})

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=lead repository=x/y id=app\nlead: 1.0.0\n",
		"b.txt": "# clover: from=app value=version\nfollower: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 2)

	// a.txt sorts before b.txt; the producer resolves, then the follower reuses it.
	require.Equal(t, "2.0.0", files[0].Results[0].Resolved)
	require.Equal(t, "2.0.0", files[1].Results[0].Resolved)
	require.Equal(t, "follower: 2.0.0", files[1].Results[0].NewLine)
}

// TestRunToPinsExactVersion confirms WithTo rewrites the marker to the named
// version even when it is older than the current one and the directive's rules
// would have rejected it, and that a pin missing upstream fails with the
// listing detail.
func TestRunToPinsExactVersion(t *testing.T) {
	provider.Register(fakeProvider{
		name: "pinned",
		candidates: []model.Candidate{
			candidate(t, "1.0.0"),
			candidate(t, "1.5.0"),
			candidate(t, "2.0.0"),
		},
	})

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=pinned repository=x/y constraint=patch\nversion: 1.5.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir}, pipeline.WithTo("1.0.0"))
	require.NoError(t, err)
	require.NoError(t, files[0].Results[0].Err)
	require.Equal(t, "1.0.0", files[0].Results[0].Resolved)
	require.Equal(t, "version: 1.0.0", files[0].Results[0].NewLine)

	files, err = pipeline.Run(context.Background(), []string{dir}, pipeline.WithTo("3.0.0"))
	require.NoError(t, err)
	require.EqualError(
		t,
		files[0].Results[0].Err,
		"no candidate satisfies the rule: the requested version is not in the upstream listing",
	)
}

// TestRunScopedRules confirms a run.rules entry applies its defaults only to
// the markers its selectors match: the rule's prerelease opt-in frees the
// matching provider's marker while the other file's marker stays on stable.
func TestRunScopedRules(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "ruled",
		candidates: []model.Candidate{candidate(t, "1.0.0"), candidate(t, "2.0.0-rc.1")},
	})
	provider.Register(fakeProvider{
		name:       "unruled",
		candidates: []model.Candidate{candidate(t, "1.0.0"), candidate(t, "2.0.0-rc.1")},
	})

	dir := writeRepo(t,
		"run:\n  rules:\n    - providers: [ruled]\n      prerelease: true\n",
		map[string]string{
			"a.txt": "# clover: provider=ruled repository=x/y\nversion: 1.0.0\n",
			"b.txt": "# clover: provider=unruled repository=x/y\nversion: 1.0.0\n",
		})

	files, err := pipeline.Run(context.Background(), []string{dir},
		pipeline.WithConfig(config.NewResolver(nil, "", false)))
	require.NoError(t, err)
	require.Len(t, files, 2)

	require.Equal(t, "2.0.0-rc.1", files[0].Results[0].Resolved)
	require.Equal(t, "1.0.0", files[1].Results[0].Resolved)
}

// TestRunDowngradeOverride confirms the run-level flag overrides the
// per-directive downgrade rule: nil leaves the directive in force, true
// forces a downgrade the directive did not permit, and false blocks one it did.
func TestRunDowngradeOverride(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "downflag",
		candidates: []model.Candidate{candidate(t, "1.0.0")}, // only a lower version upstream
	})
	provider.Register(fakeProvider{
		name:       "downrule",
		candidates: []model.Candidate{candidate(t, "1.0.0")},
	})

	// Directive silent on downgrade: default refuses, the flag forces it.
	noRule := write(t, map[string]string{
		"app.txt": "# clover: provider=downflag repository=x/y\nversion: 2.0.0\n",
	})
	files, err := pipeline.Run(context.Background(), []string{noRule})
	require.NoError(t, err)
	require.Error(t, files[0].Results[0].Err, "downgrade refused by default")

	files, err = pipeline.Run(context.Background(), []string{noRule},
		pipeline.WithDowngrade(new(true)))
	require.NoError(t, err)
	require.True(t, files[0].Results[0].Changed)
	require.Equal(t, "1.0.0", files[0].Results[0].Resolved, "flag forced the downgrade")

	// Directive allows downgrade: nil keeps it, false overrides to block.
	withRule := write(t, map[string]string{
		"app.txt": "# clover: provider=downrule repository=x/y downgrade=true\nversion: 2.0.0\n",
	})
	files, err = pipeline.Run(context.Background(), []string{withRule})
	require.NoError(t, err)
	require.Equal(t, "1.0.0", files[0].Results[0].Resolved, "directive allows the downgrade")

	files, err = pipeline.Run(context.Background(), []string{withRule},
		pipeline.WithDowngrade(new(false)))
	require.NoError(t, err)
	require.Error(t, files[0].Results[0].Err, "flag overrode the directive to block")
}

// TestRunPrereleaseOverride confirms WithPrerelease(true) lets a marker select a
// prerelease the per-directive rule would otherwise exclude.
func TestRunPrereleaseOverride(t *testing.T) {
	provider.Register(fakeProvider{
		name: "preflag",
		candidates: []model.Candidate{
			candidate(t, "1.0.0"),
			candidate(t, "2.0.0-rc.1"),
		},
	})

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=preflag repository=x/y\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.False(t, files[0].Results[0].Changed, "prereleases excluded by default")

	files, err = pipeline.Run(context.Background(), []string{dir},
		pipeline.WithPrerelease(new(true)))
	require.NoError(t, err)
	require.True(t, files[0].Results[0].Changed)
	require.Equal(t, "2.0.0-rc.1", files[0].Results[0].Resolved, "flag allowed the prerelease")
}

func TestRunUnknownProviderErrors(t *testing.T) {
	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=nope repository=x/y\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Error(t, files[0].Results[0].Err)
	require.False(t, files[0].Results[0].Changed)
}

// TestRunUnresolvedAutoErrors confirms a provider=auto marker whose target line
// matches no inference rule fails with a message pointing at the fix, not a
// confusing "unknown provider auto".
func TestRunUnresolvedAutoErrors(t *testing.T) {
	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=auto\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.EqualError(
		t,
		files[0].Results[0].Err,
		`could not infer a provider for the target line - set "provider" explicitly`,
	)
	require.False(t, files[0].Results[0].Changed)
}

func TestRunDanglingFollowSkips(t *testing.T) {
	dir := write(t, map[string]string{
		"app.txt": "# clover: from=missing value=version\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.True(t, files[0].Results[0].Skipped)
	// The reason names the bare id the user wrote, not the internal namespaced key.
	require.Equal(t, `unknown from "missing"`, files[0].Results[0].Reason)
}

func TestRunAmbiguousTargetErrors(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "ambig",
		candidates: []model.Candidate{candidate(t, "1.3.0")},
	})

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=ambig repository=x/y\nrange 1.0.0 to 2.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Error(t, files[0].Results[0].Err)
}
