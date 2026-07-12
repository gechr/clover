package command_test

import (
	"context"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/command"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/output"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// stubProvider is a fake upstream returning canned candidates without touching
// the network, shared by the run, lint, and format command tests.
type stubProvider struct {
	name        string
	keys        []provider.Key
	candidates  []model.Candidate
	discoverErr error
}

func (p stubProvider) Name() string { return p.name }

func (p stubProvider) Color(bool) color.Color { return color.Gray{Y: 0x80} }

func (p stubProvider) Keys() []provider.Key {
	if p.keys != nil {
		return p.keys
	}
	return []provider.Key{{Name: "repository"}}
}

func (p stubProvider) Resource(
	directive.Directive,
) (provider.Resource, error) {
	return p.name, nil
}

func (p stubProvider) Describe(provider.Resource) string { return p.name }

func (p stubProvider) Discover(context.Context, provider.Resource) ([]model.Candidate, error) {
	return p.candidates, p.discoverErr
}

// writeMarker writes a single directive file under a fresh temp dir and returns
// the dir and the file path.
func writeMarker(t *testing.T, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.txt")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return dir, path
}

func readAt(t *testing.T, path string) string {
	t.Helper()
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(got)
}

// TestRunRun drives the run command against fake providers, exercising the
// resolve-and-rewrite path and its early-exit error branches.
func TestRunRun(t *testing.T) {
	t.Setenv("CLOVER_NO_CACHE", "1")
	provider.Register(stubProvider{
		name:       "runok",
		candidates: []model.Candidate{model.NewCandidate("1.5.0")},
	})

	t.Run("dry-run resolves without writing", func(t *testing.T) {
		body := "# clover: provider=runok repository=x/y\nversion: 1.2.0\n"
		dir, path := writeMarker(t, body)
		require.NoError(t, command.RunRun(
			[]string{dir}, true, false, nil, nil, nil, "", nil, resolver(), 4,
		))
		require.Equal(t, body, readAt(t, path), "--dry-run writes nothing")
	})

	t.Run("default rewrites the line", func(t *testing.T) {
		dir, path := writeMarker(t, "# clover: provider=runok repository=x/y\nversion: 1.2.0\n")
		require.NoError(t, command.RunRun(
			[]string{dir}, false, false, nil, nil, nil, "", nil, resolver(), 4,
		))
		require.Equal(
			t,
			"# clover: provider=runok repository=x/y\nversion: 1.5.0\n",
			readAt(t, path),
		)
	})

	t.Run("errored marker fails the run", func(t *testing.T) {
		dir, _ := writeMarker(t, "# clover: provider=runghost repository=x/y\nversion: 1.0.0\n")
		err := command.RunRun([]string{dir}, false, false, nil, nil, nil, "", nil, resolver(), 4)
		require.EqualError(t, err, "1 failed")
	})

	t.Run("github output prints annotations", func(t *testing.T) {
		dir, _ := writeMarker(t, "# clover: provider=runok repository=x/y\nversion: 1.2.0\n")
		gh := output.GitHub
		out := captureStdout(t, func() {
			require.NoError(t, command.RunRun(
				[]string{dir}, true, false, nil, nil, nil, "", &gh, resolver(), 4,
			))
		})
		require.NotEmpty(t, out, "github mode emits machine-readable annotations to stdout")
	})

	t.Run("bad tag errors early", func(t *testing.T) {
		dir := t.TempDir()
		err := command.RunRun(
			[]string{dir}, false, false, nil, nil, []string{"a,b/c"}, "", nil, resolver(), 4,
		)
		require.Error(t, err, "a tag mixing AND and OR is rejected")
	})

	t.Run("bad cooldown errors early", func(t *testing.T) {
		dir := t.TempDir()
		err := command.RunRun(
			[]string{dir}, false, false, nil, nil, nil, "not-a-duration", nil, resolver(), 4,
		)
		require.EqualError(
			t,
			err,
			`invalid --cooldown: "not-a-duration" must be a duration like 2w3d`,
		)
	})

	t.Run("unknown enabled provider errors early", func(t *testing.T) {
		dir := t.TempDir()
		err := command.RunRun(
			[]string{
				dir,
			},
			false,
			false,
			[]string{"nosuchprovider"},
			nil,
			nil,
			"",
			nil,
			resolver(),
			4,
		)
		require.Error(t, err, "an unknown --enable provider is rejected")
	})
}

// TestEnableHTTPCache confirms the disk cache is opened only when enabled, using
// an isolated XDG cache dir so the run never touches the real cache.
func TestEnableHTTPCache(t *testing.T) {
	t.Run("disabled by env creates no dir", func(t *testing.T) {
		cache := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cache)
		t.Setenv("CLOVER_NO_CACHE", "1")

		command.EnableHTTPCache(nil, resolver(), nil)
		_, err := os.Stat(filepath.Join(cache, "clover", "http"))
		require.True(t, os.IsNotExist(err), "CLOVER_NO_CACHE skips opening the disk cache")
	})

	t.Run("enabled opens the disk cache", func(t *testing.T) {
		cache := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cache)
		t.Setenv("CLOVER_NO_CACHE", "")

		command.EnableHTTPCache(nil, resolver(), nil)
		info, err := os.Stat(filepath.Join(cache, "clover", "http"))
		require.NoError(t, err)
		require.True(t, info.IsDir(), "the shared disk cache directory is created")
	})
}

// TestReportDeep drives both --deep hint arms and confirms none panic.
func TestReportDeep(t *testing.T) {
	t.Parallel()

	nginx := provider.Truncation{Resource: "ghcr.io/o/nginx", URL: "https://ghcr.io/o/nginx"}
	gated := pipeline.Result{
		Truncated: true,
		Err:       pipeline.ErrNoCandidate,
		Marker:    pipeline.Marker{File: "app.txt", Target: 1},
	}
	summary := mode.Summary{Outcomes: []mode.Outcome{{
		FileResult: pipeline.FileResult{Results: []pipeline.Result{gated}},
	}}}

	tests := map[string]struct {
		summary   mode.Summary
		truncated []provider.Truncation
	}{
		"empty is silent":  {},
		"both hint kinds":  {summary: summary, truncated: []provider.Truncation{nginx}},
		"duplicates dedup": {truncated: []provider.Truncation{nginx, nginx}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.NotPanics(t, func() { command.ReportDeep(tc.summary, tc.truncated) })
		})
	}
}

// TestReportAuth registers an anonymous and an authenticated provider and drives
// the hint pass over a summary that used both.
func TestReportAuth(t *testing.T) {
	provider.Register(authedProvider{name: "authreportok"})
	provider.Register(authedProvider{name: "authreportanon", err: errTest})

	summary := mode.Summary{Outcomes: []mode.Outcome{{
		FileResult: pipeline.FileResult{Results: []pipeline.Result{
			{Marker: pipeline.Marker{Provider: "authreportok"}},
			{Marker: pipeline.Marker{Provider: "authreportanon"}},
		}},
	}}}

	require.NotPanics(t, func() { command.ReportAuth(context.Background(), summary) })
}
