package gitea_test

import (
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/gitea"
	"github.com/stretchr/testify/require"
)

func TestNameAndKeys(t *testing.T) {
	t.Parallel()

	p := gitea.New()
	require.Equal(t, "gitea", p.Name())

	keys := p.Keys()
	require.Equal(t, []provider.Key{
		{Name: "repository", Required: true},
		{Name: "host", Required: false},
		{Name: "source", Required: false},
	}, keys)
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
			name:         "defaults to codeberg tags",
			pairs:        []directive.KV{{Key: "repository", Value: "forgejo/forgejo"}},
			wantDescribe: "codeberg.org/forgejo/forgejo (tags)",
		},
		{
			name: "custom host and releases",
			pairs: []directive.KV{
				{Key: "repository", Value: "owner/tool"},
				{Key: "host", Value: "https://git.example.com/"},
				{Key: "source", Value: "releases"},
			},
			wantDescribe: "git.example.com/owner/tool (releases)",
		},
		{
			name:    "missing repository",
			pairs:   nil,
			wantErr: true,
		},
		{
			name:    "repository without owner",
			pairs:   []directive.KV{{Key: "repository", Value: "forgejo"}},
			wantErr: true,
		},
		{
			name:    "repository with nested path",
			pairs:   []directive.KV{{Key: "repository", Value: "group/sub/repo"}},
			wantErr: true,
		},
		{
			name: "empty host",
			pairs: []directive.KV{
				{Key: "repository", Value: "owner/tool"},
				{Key: "host", Value: "https://"},
			},
			wantErr: true,
		},
		{
			name: "bad source",
			pairs: []directive.KV{
				{Key: "repository", Value: "owner/tool"},
				{Key: "source", Value: "commits"},
			},
			wantErr: true,
		},
		{
			name: "asset without releases",
			pairs: []directive.KV{
				{Key: "repository", Value: "owner/tool"},
				{Key: "asset", Value: "*.tar.gz"},
			},
			wantErr: true,
		},
	}

	p := gitea.New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := p.Resource(directiveOf(tt.pairs...))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantDescribe, p.Describe(res))
		})
	}
}

func TestDescribeInvalidResource(t *testing.T) {
	t.Parallel()

	require.Equal(t, "gitea", gitea.New().Describe("not-a-resource"))
}

func TestURL(t *testing.T) {
	t.Parallel()

	p := gitea.New()
	res := resourceFor(t, p, directive.KV{Key: "repository", Value: "forgejo/forgejo"})

	require.Equal(t,
		"https://codeberg.org/forgejo/forgejo/src/tag/v15.0.3",
		p.URL(res, model.Candidate{Version: "v15.0.3", Ref: "v15.0.3"}),
	)
	require.Empty(t, p.URL(res, model.Candidate{}))
	require.Empty(t, p.URL("not-a-resource", model.Candidate{Ref: "v1.0.0"}))
}

// TestNotRecencyOrderer locks the deliberate omission: Gitea orders tags by
// creation date with no version-sort parameter, so the first page is not
// guaranteed to hold the highest version - the provider must not claim the
// recency-ordered capability.
func TestNotRecencyOrderer(t *testing.T) {
	t.Parallel()

	_, ok := any(gitea.New()).(provider.RecencyOrderer)
	require.False(t, ok)
}

// TestAuthenticate covers the present/absent credential reporting: an injected
// token authenticates, its absence degrades to the informational anonymous error.
func TestAuthenticate(t *testing.T) {
	t.Parallel()

	require.NoError(t, gitea.New(gitea.WithToken("tok")).Authenticate(t.Context()))

	// A test transport pins resolution away from ambient env, so no token is found.
	err := gitea.New(gitea.WithTransport(roundTripFunc(nil))).Authenticate(t.Context())
	require.Error(t, err)
}
