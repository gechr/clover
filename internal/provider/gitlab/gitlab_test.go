package gitlab_test

import (
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/gitlab"
	"github.com/stretchr/testify/require"
)

func directiveOf(pairs ...directive.KV) directive.Directive {
	return directive.Directive{Pairs: pairs}
}

func TestNameAndKeys(t *testing.T) {
	t.Parallel()

	p := gitlab.New()
	require.Equal(t, "gitlab", p.Name())

	keys := p.Keys()
	require.Len(t, keys, 3)
	require.Equal(t, "repository", keys[0].Name)
	require.True(t, keys[0].Required)
	require.Equal(t, "host", keys[1].Name)
	require.False(t, keys[1].Required)
	require.Equal(t, "source", keys[2].Name)
	require.False(t, keys[2].Required)
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
			name:         "project defaults to tags",
			pairs:        []directive.KV{{Key: "repository", Value: "group/project"}},
			wantDescribe: "gitlab.com/group/project (tags)",
		},
		{
			name: "explicit releases",
			pairs: []directive.KV{
				{Key: "repository", Value: "group/project"},
				{Key: "source", Value: "releases"},
			},
			wantDescribe: "gitlab.com/group/project (releases)",
		},
		{
			name:         "nested groups are allowed",
			pairs:        []directive.KV{{Key: "repository", Value: "group/subgroup/project"}},
			wantDescribe: "gitlab.com/group/subgroup/project (tags)",
		},
		{
			name: "self-managed host override",
			pairs: []directive.KV{
				{Key: "repository", Value: "group/project"},
				{Key: "host", Value: "gitlab.example.com"},
			},
			wantDescribe: "gitlab.example.com/group/project (tags)",
		},
		{
			name: "host normalizes a full URL",
			pairs: []directive.KV{
				{Key: "repository", Value: "group/project"},
				{Key: "host", Value: "https://GitLab.example.com/"},
			},
			wantDescribe: "gitlab.example.com/group/project (tags)",
		},
		{
			name: "invalid host with a path",
			pairs: []directive.KV{
				{Key: "repository", Value: "group/project"},
				{Key: "host", Value: "gitlab.example.com/foo"},
			},
			wantErr: true,
		},
		{name: "missing project", pairs: nil, wantErr: true},
		{
			name:    "project without namespace",
			pairs:   []directive.KV{{Key: "repository", Value: "project"}},
			wantErr: true,
		},
		{
			name:    "empty path segment",
			pairs:   []directive.KV{{Key: "repository", Value: "group//project"}},
			wantErr: true,
		},
		{
			name: "invalid source",
			pairs: []directive.KV{
				{Key: "repository", Value: "group/project"},
				{Key: "source", Value: "branches"},
			},
			wantErr: true,
		},
		{
			name: "asset with releases is accepted",
			pairs: []directive.KV{
				{Key: "repository", Value: "group/project"},
				{Key: "source", Value: "releases"},
				{Key: "asset", Value: "*linux*"},
			},
			wantDescribe: "gitlab.com/group/project (releases)",
		},
		{
			name: "asset requires releases, not the default tags",
			pairs: []directive.KV{
				{Key: "repository", Value: "group/project"},
				{Key: "asset", Value: "*linux*"},
			},
			wantErr: true,
		},
	}

	p := gitlab.New()
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

	require.Equal(t, "gitlab", gitlab.New().Describe("not-a-resource"))
}

func TestURL(t *testing.T) {
	t.Parallel()

	p := gitlab.New()
	res := resourceFor(t, p, directive.KV{Key: "repository", Value: "group/subgroup/project"})

	require.Equal(t,
		"https://gitlab.com/group/subgroup/project/-/tags/v1.4.0",
		p.URL(res, model.Candidate{Ref: "v1.4.0"}),
	)
	require.Empty(t, p.URL(res, model.Candidate{}))
	require.Empty(t, p.URL("not-a-resource", model.Candidate{Ref: "v1.4.0"}))
}

// TestRecencyOrderer locks the capability: the tags endpoint is queried
// newest-first (order_by=updated&sort=desc) and the releases endpoint is
// date-ordered, so the provider claims the recency-ordered capability that routes
// the targeted (not blanket) truncation hint.
func TestRecencyOrderer(t *testing.T) {
	t.Parallel()

	_, ok := any(gitlab.New()).(provider.RecencyOrderer)
	require.True(t, ok)
}
