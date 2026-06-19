package match_test

import (
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/stretchr/testify/require"
)

func TestDockerTagRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		line      string
		candidate model.Candidate
		raw       string
		want      string
	}{
		{
			name:      "plain hub image",
			line:      "FROM nginx:1.27",
			candidate: model.Candidate{Version: "1.29"},
			raw:       "1.27",
			want:      "FROM nginx:1.29",
		},
		{
			name:      "ported registry is not mistaken for the version",
			line:      "FROM localhost:5000/team/api:2.0.1",
			candidate: model.Candidate{Version: "2.1.0"},
			raw:       "2.0.1",
			want:      "FROM localhost:5000/team/api:2.1.0",
		},
		{
			name:      "ECR registry host with account id and region",
			line:      "    image: 123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api:2.0.1",
			candidate: model.Candidate{Version: "2.1.0"},
			raw:       "2.0.1",
			want:      "    image: 123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api:2.1.0",
		},
		{
			name:      "preserves the v prefix and precision",
			line:      "FROM ghcr.io/owner/img:v1.2",
			candidate: model.Candidate{Version: "1.3.0"},
			raw:       "v1.2",
			want:      "FROM ghcr.io/owner/img:v1.3",
		},
	}

	rw := match.NewDockerTag()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			located, err := rw.Locate(tt.line)
			require.NoError(t, err)
			require.Equal(t, tt.raw, located.Raw)
			require.False(t, located.NeedsDigest(), "a tag-only ref carries no digest")

			out, changed, err := rw.Render(tt.line, located, tt.candidate)
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, tt.want, out)
		})
	}
}

func TestDockerTagLocateErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		line    string
		wantErr string
	}{
		{"no tag", "FROM nginx", "image has no tag to anchor the version"},
		{"non-version tag", "FROM nginx:latest", "image tag is not a single version"},
	}

	rw := match.NewDockerTag()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := rw.Locate(tt.line)
			require.EqualError(t, err, tt.wantErr)
		})
	}
}
