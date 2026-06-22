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

func TestKeys(t *testing.T) {
	t.Parallel()

	keys := github.New().Keys()
	require.Equal(t, "repository", keys[0].Name)
	require.True(t, keys[0].Required)
	require.Equal(t, "source", keys[1].Name)
	require.False(t, keys[1].Required)
}
