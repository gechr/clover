package github_test

import (
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider/github"
	"github.com/stretchr/testify/require"
)

func TestURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		candidate model.Candidate
		want      string
	}{
		{
			name:      "tag ref",
			candidate: model.Candidate{Ref: "v1.2.3"},
			want:      "https://github.com/owner/name/releases/tag/v1.2.3",
		},
		{
			name:      "no ref",
			candidate: model.Candidate{Version: "v1.2.3"},
			want:      "",
		},
	}

	p := github.New()
	res, err := p.Resource(directive.Directive{
		Pairs: []directive.KV{{Key: "repository", Value: "owner/name"}},
	})
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, p.URL(res, tt.candidate))
		})
	}
}
