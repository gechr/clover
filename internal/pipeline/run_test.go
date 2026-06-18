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

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
	"github.com/stretchr/testify/require"
)

// fakeProvider is a registered provider that returns canned candidates without
// touching the network, so a run resolves deterministically in tests.
type fakeProvider struct {
	name       string
	candidates []model.Candidate
	err        error
	deep       *bool  // when set, Discover records whether a deep lookup was requested
	truncate   bool   // when set, Discover reports a truncated lookup
	digest     string // the digest the Digester capability returns
}

func (f fakeProvider) Digest(context.Context, provider.Resource, string) (string, error) {
	return f.digest, nil
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
	if f.truncate {
		provider.NoteTruncated(ctx, f.name)
	}
	return f.candidates, f.err
}

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

	_, err = pipeline.Run(context.Background(), []string{dir}, pipeline.WithDeep(true))
	require.NoError(t, err)
	require.True(t, got, "WithDeep(true) reaches the provider's Discover")
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
	require.ErrorIs(t, files[0].Results[0].Err, pipeline.ErrNoCandidate,
		"a minor-ceiling constraint rejects the only candidate (a major bump)")
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

	var noted []string
	_, err := pipeline.Run(context.Background(), []string{dir},
		pipeline.WithTruncationSink(func(r string) { noted = append(noted, r) }))
	require.NoError(t, err)
	require.Equal(t, []string{"trunc"}, noted)
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

func TestRunResolvesSha256Follower(t *testing.T) {
	sum := strings.Repeat("e", 64)
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, sum+"  tool_1.2.0_linux_amd64.tar.gz\n"+
			strings.Repeat("f", 64)+"  tool_1.2.0_windows.zip\n")
	}))
	defer srv.Close()

	provider.Register(
		fakeProvider{name: "fake", candidates: []model.Candidate{candidate(t, "1.2.0")}},
	)

	old := strings.Repeat("a", 64)
	dir := write(t, map[string]string{
		"versions.env": "# clover: provider=fake id=tool repository=x/y\n" +
			"VERSION=1.0.0\n" +
			"# clover: from=tool value=sha256 sha256-url=" + srv.URL +
			"/v{version}/checksums.txt pattern=*linux_amd64*\n" +
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
	require.Equal(t, "/v1.2.0/checksums.txt", gotPath, "{version} is the bare producer version")
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

// TestRunAllowDowngradeOverride confirms the run-level flag overrides the
// per-directive allow-downgrade rule: nil leaves the directive in force, true
// forces a downgrade the directive did not permit, and false blocks one it did.
func TestRunAllowDowngradeOverride(t *testing.T) {
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
		pipeline.WithAllowDowngrade(new(true)))
	require.NoError(t, err)
	require.True(t, files[0].Results[0].Changed)
	require.Equal(t, "1.0.0", files[0].Results[0].Resolved, "flag forced the downgrade")

	// Directive allows downgrade: nil keeps it, false overrides to block.
	withRule := write(t, map[string]string{
		"app.txt": "# clover: provider=downrule repository=x/y allow-downgrade=true\nversion: 2.0.0\n",
	})
	files, err = pipeline.Run(context.Background(), []string{withRule})
	require.NoError(t, err)
	require.Equal(t, "1.0.0", files[0].Results[0].Resolved, "directive allows the downgrade")

	files, err = pipeline.Run(context.Background(), []string{withRule},
		pipeline.WithAllowDowngrade(new(false)))
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
	require.ErrorContains(t, files[0].Results[0].Err, "could not infer a provider")
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
