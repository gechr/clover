package github_test

import (
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/provider/github"
	"github.com/stretchr/testify/require"
)

func directiveOf(pairs ...directive.KV) directive.Directive {
	return directive.Directive{Pairs: pairs}
}

func TestResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		pairs        []directive.KV
		wantErr      bool
		wantDescribe string
	}{
		{
			name:         "repo defaults to tags",
			pairs:        []directive.KV{{Key: "repository", Value: "owner/name"}},
			wantDescribe: "github.com/owner/name (tags)",
		},
		{
			name: "enterprise host override",
			pairs: []directive.KV{
				{Key: "repository", Value: "owner/name"},
				{Key: "host", Value: "ghe.example.com"},
			},
			wantDescribe: "ghe.example.com/owner/name (tags)",
		},
		{
			name: "host normalizes a full URL",
			pairs: []directive.KV{
				{Key: "repository", Value: "owner/name"},
				{Key: "host", Value: "https://GHE.example.com"},
			},
			wantDescribe: "ghe.example.com/owner/name (tags)",
		},
		{
			name: "invalid host with a path",
			pairs: []directive.KV{
				{Key: "repository", Value: "owner/name"},
				{Key: "host", Value: "ghe.example.com/foo"},
			},
			wantErr: true,
		},
		{
			name: "explicit releases",
			pairs: []directive.KV{
				{Key: "repository", Value: "owner/name"},
				{Key: "source", Value: "releases"},
			},
			wantDescribe: "github.com/owner/name (releases)",
		},
		{name: "missing repo", pairs: nil, wantErr: true},
		{
			name:    "repo without name",
			pairs:   []directive.KV{{Key: "repository", Value: "owner"}},
			wantErr: true,
		},
		{
			name:    "repo with extra segment",
			pairs:   []directive.KV{{Key: "repository", Value: "owner/name/sub"}},
			wantErr: true,
		},
		{
			name: "invalid source",
			pairs: []directive.KV{
				{Key: "repository", Value: "owner/name"},
				{Key: "source", Value: "branches"},
			},
			wantErr: true,
		},
		{
			name: "asset with releases is accepted",
			pairs: []directive.KV{
				{Key: "repository", Value: "owner/name"},
				{Key: "source", Value: "releases"},
				{Key: "asset", Value: "*linux*"},
			},
			wantDescribe: "github.com/owner/name (releases)",
		},
		{
			name: "asset requires releases, not the default tags",
			pairs: []directive.KV{
				{Key: "repository", Value: "owner/name"},
				{Key: "asset", Value: "*linux*"},
			},
			wantErr: true,
		},
		{
			name:         "tool resolves a registry name",
			pairs:        []directive.KV{{Key: "tool", Value: "ripgrep"}},
			wantDescribe: "github.com/BurntSushi/ripgrep (tags)",
		},
		{
			name:         "tool resolves a curated name",
			pairs:        []directive.KV{{Key: "tool", Value: "erlang"}},
			wantDescribe: "github.com/erlang/otp (tags)",
		},
		{
			name: "tool with releases",
			pairs: []directive.KV{
				{Key: "tool", Value: "ripgrep"},
				{Key: "source", Value: "releases"},
			},
			wantDescribe: "github.com/BurntSushi/ripgrep (releases)",
		},
	}

	provider := github.New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := provider.Resource(directiveOf(tt.pairs...))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantDescribe, provider.Describe(res))
		})
	}
}

// TestIdentify confirms the resource id and landing page, including when the
// repository comes from a curated tool name.
func TestIdentify(t *testing.T) {
	t.Parallel()

	p := github.New()
	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "actions/checkout"}))
	require.NoError(t, err)
	id, link := p.Identify(res)
	require.Equal(t, "actions/checkout", id)
	require.Equal(t, "https://github.com/actions/checkout", link)

	res, err = p.Resource(directiveOf(directive.KV{Key: "tool", Value: "ripgrep"}))
	require.NoError(t, err)
	id, link = p.Identify(res)
	require.Equal(t, "BurntSushi/ripgrep", id)
	require.Equal(t, "https://github.com/BurntSushi/ripgrep", link)

	id, link = p.Identify("not a resource")
	require.Empty(t, id)
	require.Empty(t, link)
}

// TestResourceToolErrors pins the exact messages the tool key's validation
// reports, including the typo suggestion.
func TestResourceToolErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pairs   []directive.KV
		wantErr string
	}{
		{
			name: "tool and repository are mutually exclusive",
			pairs: []directive.KV{
				{Key: "tool", Value: "ripgrep"},
				{Key: "repository", Value: "owner/name"},
			},
			wantErr: `github: "repository" and "tool" are mutually exclusive`,
		},
		{
			name:    "neither tool nor repository",
			pairs:   nil,
			wantErr: `github: "repository" or "tool" is required`,
		},
		{
			name:    "unknown tool suggests the closest name",
			pairs:   []directive.KV{{Key: "tool", Value: "riprep"}},
			wantErr: `github: unknown tool "riprep", did you mean "ripgrep"?`,
		},
		{
			name:    "unknown tool without a near name",
			pairs:   []directive.KV{{Key: "tool", Value: "xqzv"}},
			wantErr: `github: unknown tool "xqzv"`,
		},
		{
			name: "tool cannot combine with host",
			pairs: []directive.KV{
				{Key: "tool", Value: "ripgrep"},
				{Key: "host", Value: "ghe.example.com"},
			},
			wantErr: `github: "tool" resolves a github.com repository, remove "host"`,
		},
	}

	provider := github.New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := provider.Resource(directiveOf(tt.pairs...))
			require.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestKeys(t *testing.T) {
	t.Parallel()

	keys := github.New().Keys()
	require.Equal(t, "repository", keys[0].Name)
	require.False(t, keys[0].Required)
	require.Equal(t, "tool", keys[1].Name)
	require.False(t, keys[1].Required)
	require.Equal(t, "host", keys[2].Name)
	require.False(t, keys[2].Required)
	require.Equal(t, "source", keys[3].Name)
	require.False(t, keys[3].Required)
}
