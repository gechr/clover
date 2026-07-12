package infer_test

import (
	"context"
	"errors"
	"image/color"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/infer"
	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
	"github.com/stretchr/testify/require"
)

// inferFake is a minimal registered provider whose Resource optionally errors,
// so the offline gate's provider and resource branches are reachable.
type inferFake struct {
	name        string
	resourceErr error
}

func (f inferFake) Name() string { return f.name }

func (f inferFake) Color(bool) color.Color { return color.Gray{Y: 0x80} }

func (f inferFake) Keys() []provider.Key              { return []provider.Key{{Name: "repository"}} }
func (f inferFake) Describe(provider.Resource) string { return f.name }

func (f inferFake) Resource(directive.Directive) (provider.Resource, error) {
	if f.resourceErr != nil {
		return nil, f.resourceErr
	}
	return f.name, nil
}

func (f inferFake) Discover(context.Context, provider.Resource) ([]model.Candidate, error) {
	return nil, nil
}

// okRewriter locates a line cleanly; errRewriter refuses it. Between them they
// drive the gate's Locate-passes and Locate-errors branches.
type okRewriter struct{}

func (okRewriter) Locate(string) (match.Location, error) { return okLocation{}, nil }

type errRewriter struct{}

func (errRewriter) Locate(string) (match.Location, error) {
	return nil, errors.New("locate boom")
}

// okLocation is a stub match.Location the passing rewriter returns; the gate
// discards it, so its methods return zero values.
type okLocation struct{}

func (okLocation) Current() string          { return "" }
func (okLocation) Semver() *version.Version { return nil }
func (okLocation) NeedsDigest() bool        { return false }

func (okLocation) Render(string, model.Candidate) (string, bool, error) {
	return "", false, nil
}

func TestDirective(t *testing.T) {
	t.Parallel()

	// An all-empty inference yields only the provider pair.
	require.Equal(t,
		directive.Directive{Pairs: []directive.KV{
			{Key: constant.DirectiveProvider, Value: "github"},
		}},
		infer.Directive(match.Inference{Provider: "github"}),
	)

	// Every populated field appends its pair in a fixed order.
	full := match.Inference{
		Provider:   "docker",
		Repository: "owner/name",
		Registry:   "ghcr.io",
		Host:       "gitlab.example.com",
		Product:    "terraform",
		Source:     "hashicorp/aws",
		TagPrefix:  "api/",
		Track:      "latest",
	}
	require.Equal(t,
		directive.Directive{Pairs: []directive.KV{
			{Key: constant.DirectiveProvider, Value: "docker"},
			{Key: constant.DirectiveRepository, Value: "owner/name"},
			{Key: constant.DirectiveRegistry, Value: "ghcr.io"},
			{Key: constant.DirectiveHost, Value: "gitlab.example.com"},
			{Key: constant.DirectiveProduct, Value: "terraform"},
			{Key: constant.DirectiveSource, Value: "hashicorp/aws"},
			{Key: constant.RuleTagPrefix, Value: "api/"},
			{Key: constant.DirectiveTrack, Value: "latest"},
		}},
		infer.Directive(full),
	)
}

func TestUnresolved(t *testing.T) {
	// Registers a provider; not parallel-safe.
	provider.Register(inferFake{name: "inferok"})
	provider.Register(inferFake{name: "inferbad", resourceErr: errors.New("bad resource")})

	d := directive.Directive{Pairs: []directive.KV{{Key: "provider", Value: "inferok"}}}

	pass := func() (match.Rewriter, error) { return okRewriter{}, nil }

	require.Equal(t, "unknown provider",
		infer.Unresolved("nosuch-provider", d, "line", pass))

	require.Equal(t, "bad resource",
		infer.Unresolved("inferbad",
			directive.Directive{Pairs: []directive.KV{{Key: "provider", Value: "inferbad"}}},
			"line", pass))

	require.Equal(t, "rewriter boom",
		infer.Unresolved("inferok", d, "line",
			func() (match.Rewriter, error) { return nil, errors.New("rewriter boom") }))

	require.Equal(t, "locate boom",
		infer.Unresolved("inferok", d, "line",
			func() (match.Rewriter, error) { return errRewriter{}, nil }))

	require.Empty(t, infer.Unresolved("inferok", d, "line", pass),
		"a provider that resolves and a rewriter that locates leave no reason")
}

func TestRecognizable(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		path  string
		lines []string
		want  bool
	}{
		"complete docker reference": {
			path: "Dockerfile", lines: []string{"FROM alpine:3.20"}, want: true,
		},
		"incomplete gitlab reference": {
			path:  ".gitlab-ci.yml",
			lines: []string{"  - component: $CI_SERVER_FQDN/org/proj/deploy@3.1.4"},
			want:  false,
		},
		"unrecognized line": {
			path: "README.md", lines: []string{"just some prose"}, want: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, infer.Recognizable(tt.path, tt.lines, 0))
		})
	}
}

func TestRecognize(t *testing.T) {
	// Registers docker; not parallel-safe.
	provider.Register(inferFake{name: constant.ProviderDocker})

	t.Run("unrecognized line", func(t *testing.T) {
		inf, reason, ok := infer.Recognize("README.md", []string{"just prose"}, 0)
		require.False(t, ok)
		require.Empty(t, reason)
		require.Equal(t, match.Inference{}, inf)
	})

	t.Run("recognized but incomplete", func(t *testing.T) {
		lines := []string{"  - component: $CI_SERVER_FQDN/org/proj/deploy@3.1.4"}
		inf, reason, ok := infer.Recognize(".gitlab-ci.yml", lines, 0)
		require.True(t, ok)
		require.Equal(t, "reference has no repository", reason)
		require.Equal(t, "gitlab", inf.Provider)
	})

	t.Run("recognized and resolvable", func(t *testing.T) {
		inf, reason, ok := infer.Recognize("Dockerfile", []string{"FROM alpine:3.20"}, 0)
		require.True(t, ok)
		require.Empty(t, reason, "a registered docker provider resolves the FROM line")
		require.Equal(t, "docker", inf.Provider)
	})

	t.Run("recognized track and resolvable", func(t *testing.T) {
		line := "FROM gcr.io/distroless/static:nonroot@sha256:" + strings.Repeat("0", 64)
		inf, reason, ok := infer.Recognize("Dockerfile", []string{line}, 0)
		require.True(t, ok)
		require.Empty(t, reason, "the tracked floating tag resolves via the docker-track rewriter")
		require.Equal(t, "nonroot", inf.Track)
	})
}
