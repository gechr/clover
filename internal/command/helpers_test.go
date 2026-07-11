package command_test

import (
	"testing"
	"time"

	"github.com/gechr/clover/internal/command"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/tag"
	"github.com/stretchr/testify/require"
)

func TestRoots(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		paths []string
		want  []string
	}{
		"nil defaults to dot":   {paths: nil, want: []string{"."}},
		"empty defaults to dot": {paths: []string{}, want: []string{"."}},
		"single kept":           {paths: []string{"a"}, want: []string{"a"}},
		"order preserved":       {paths: []string{"b", "a", "c"}, want: []string{"b", "a", "c"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, command.Roots(tc.paths))
		})
	}
}

func TestCooldownOverride(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		raw     string
		want    time.Duration
		nilWant bool
		wantErr bool
	}{
		"empty is no override": {raw: "", nilWant: true},
		"zero disables":        {raw: "0", want: 0},
		"hours":                {raw: "72h", want: 72 * time.Hour},
		"weeks and days":       {raw: "2w3d", want: (2*7 + 3) * 24 * time.Hour},
		"garbage errors":       {raw: "not-a-duration", wantErr: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := command.CooldownOverride(tc.raw)
			if tc.wantErr {
				require.Error(t, err)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			if tc.nilWant {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tc.want, *got)
		})
	}
}

func TestFailuresErrorMessage(t *testing.T) {
	t.Parallel()

	require.EqualError(t, command.FailuresError(3), "3 failed")
	require.EqualError(t, command.FailuresError(1), "1 failed")
}

// erroredSummary builds a summary whose single outcome carries one result per
// element, each errored when its bool is true.
func erroredSummary(errored ...bool) mode.Summary {
	results := make([]pipeline.Result, len(errored))
	for i, e := range errored {
		if e {
			results[i] = pipeline.Result{Err: errTest}
		}
	}
	return mode.Summary{Outcomes: []mode.Outcome{{
		FileResult: pipeline.FileResult{Results: results},
	}}}
}

var errTest = testError("boom")

type testError string

func (e testError) Error() string { return string(e) }

func TestRunErr(t *testing.T) {
	t.Parallel()

	require.NoError(t, command.RunErr(mode.Summary{}), "empty summary has no failures")
	require.NoError(t, command.RunErr(erroredSummary(false, false)), "only clean results")

	err := command.RunErr(erroredSummary(true, false, true, true))
	require.EqualError(t, err, "3 failed")
}

func TestUsedProviders(t *testing.T) {
	t.Parallel()

	summary := func(names ...string) mode.Summary {
		results := make([]pipeline.Result, len(names))
		for i, n := range names {
			results[i] = pipeline.Result{Marker: pipeline.Marker{Provider: n}}
		}
		return mode.Summary{Outcomes: []mode.Outcome{{
			FileResult: pipeline.FileResult{Results: results},
		}}}
	}

	tests := map[string]struct {
		names []string
		want  []string
	}{
		"empty yields nil": {names: nil, want: nil},
		"dedup and sort": {
			names: []string{"github", "docker", "github"},
			want:  []string{"docker", "github"},
		},
		"empty and follow excluded": {
			names: []string{"", constant.ProviderFollow, "npm"},
			want:  []string{"npm"},
		},
		"natural order": {
			names: []string{"p10", "p2", "p1"},
			want:  []string{"p1", "p2", "p10"},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, command.UsedProviders(summary(tc.names...)))
		})
	}
}

func TestConfirmDeep(t *testing.T) {
	t.Parallel()

	require.True(t, command.ConfirmDeep(true), "--yes always proceeds")
	require.True(t, command.ConfirmDeep(false), "non-interactive session proceeds without a prompt")
}

func TestTagFilter(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		tags    []string
		want    tag.Filter
		wantErr bool
	}{
		"empty is empty filter": {tags: nil, want: tag.Filter{}},
		"and terms":             {tags: []string{"a,b"}, want: tag.Filter{All: []string{"a", "b"}}},
		"or terms":              {tags: []string{"a/b"}, want: tag.Filter{Any: []string{"a", "b"}}},
		"mixed rejected":        {tags: []string{"a,b/c"}, wantErr: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := command.TagFilter(tc.tags)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestProviderFilter registers a fake provider, so it must not run in parallel
// (the registry is global) alongside a parallel body.
func TestProviderFilter(t *testing.T) {
	provider.Register(authedProvider{name: "cfilterfake"})

	empty, err := command.ProviderFilter(nil, nil)
	require.NoError(t, err)
	require.True(t, empty.Empty(), "no selection is an empty filter")

	enabled, err := command.ProviderFilter([]string{"cfilterfake"}, nil)
	require.NoError(t, err)
	require.False(t, enabled.Empty())

	_, err = command.ProviderFilter([]string{"nosuchprovider"}, nil)
	require.Error(t, err, "an unknown provider is rejected")
}

// TestNewResolver uses t.Setenv, so it must not run in parallel.
func TestNewResolver(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	resolver, err := command.NewResolver("", true)
	require.NoError(t, err)
	require.NotNil(t, resolver, "--no-config skips all IO and still returns a resolver")

	_, err = command.NewResolver("/nonexistent/clover.yml", false)
	require.Error(t, err, "an explicit --config path that does not exist fails fast")
}
