package match_test

import (
	"strings"
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/stretchr/testify/require"
)

func TestDockerTrackRender(t *testing.T) {
	t.Parallel()

	oldDigest := "sha256:" + strings.Repeat("a", 64)
	newDigest := "sha256:" + strings.Repeat("b", 64)

	tests := []struct {
		name      string
		line      string
		candidate model.Candidate
		raw       string
		want      string
	}{
		{
			name:      "floating tag keeps its name, refreshes the digest",
			line:      "FROM redis:latest@" + oldDigest,
			candidate: model.Candidate{Version: "latest", Digest: newDigest},
			raw:       "latest",
			want:      "FROM redis:latest@" + newDigest,
		},
		{
			name:      "compose nonroot with a ported registry",
			line:      "    image: localhost:5000/team/api:nonroot@" + oldDigest,
			candidate: model.Candidate{Version: "nonroot", Digest: newDigest},
			raw:       "nonroot",
			want:      "    image: localhost:5000/team/api:nonroot@" + newDigest,
		},
		{
			name:      "explicit ref rewrites the tag too",
			line:      "FROM redis:latest@" + oldDigest,
			candidate: model.Candidate{Version: "edge", Digest: newDigest},
			raw:       "latest",
			want:      "FROM redis:edge@" + newDigest,
		},
	}

	rw := match.NewDockerTrack()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			located, err := rw.Locate(tt.line)
			require.NoError(t, err)
			require.Equal(t, tt.raw, located.Current())
			require.Nil(t, located.Semver())
			require.True(t, located.NeedsDigest())

			out, changed, err := located.Render(tt.line, tt.candidate)
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, tt.want, out)
		})
	}
}

func TestDockerTrackLocateErrors(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("a", 64)
	tests := []struct {
		name    string
		line    string
		wantErr string
	}{
		{"not pinned", "FROM redis:latest", "image is not digest-pinned by @sha256"},
		{"short digest", "FROM redis:latest@sha256:abc", "image pin requires a full sha256 digest"},
		{
			"non-sha256",
			"FROM redis:latest@md5:" + strings.Repeat("a", 64),
			"image pin digest must be sha256",
		},
		{"no tag", "FROM redis@" + digest, "image has no tag to track"},
	}

	rw := match.NewDockerTrack()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := rw.Locate(tt.line)
			require.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestDockerTrackRenderRequiresDigest(t *testing.T) {
	t.Parallel()

	rw := match.NewDockerTrack()
	line := "FROM redis:latest@sha256:" + strings.Repeat("a", 64)
	located, err := rw.Locate(line)
	require.NoError(t, err)

	_, _, err = located.Render(line, model.Candidate{Version: "latest"}) // no digest
	require.EqualError(
		t,
		err,
		`candidate has no sha256 digest to pin, got ""`,
		"never half-updates",
	)
}
