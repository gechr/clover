package match_test

import (
	"strings"
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/stretchr/testify/require"
)

func TestDockerPinRender(t *testing.T) {
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
			name:      "dockerfile FROM tag and digest",
			line:      "FROM nginx:1.27@" + oldDigest,
			candidate: model.Candidate{Version: "1.29", Digest: newDigest},
			raw:       "1.27",
			want:      "FROM nginx:1.29@" + newDigest,
		},
		{
			name:      "compose image with a ported registry",
			line:      "    image: localhost:5000/team/api:2.0.1@" + oldDigest,
			candidate: model.Candidate{Version: "2.1.0", Digest: newDigest},
			raw:       "2.0.1",
			want:      "    image: localhost:5000/team/api:2.1.0@" + newDigest,
		},
		{
			name:      "preserves the v prefix",
			line:      "FROM ghcr.io/owner/img:v1.2.3@" + oldDigest,
			candidate: model.Candidate{Version: "1.3.0", Digest: newDigest},
			raw:       "v1.2.3",
			want:      "FROM ghcr.io/owner/img:v1.3.0@" + newDigest,
		},
	}

	rw := match.NewDockerPin()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			located, err := rw.Locate(tt.line)
			require.NoError(t, err)
			require.Equal(t, tt.raw, located.Current())
			require.True(t, located.NeedsDigest())

			out, changed, err := located.Render(tt.line, tt.candidate)
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, tt.want, out)
		})
	}
}

// Rendered must report exactly the tag Render writes, so the pipeline resolves a
// digest for that tag rather than the raw candidate. It restyles the candidate
// onto the located shape: a variant is stripped to match a plain line, and the
// core follows the line's precision.
func TestDockerPinRendered(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("a", 64)

	tests := []struct {
		name      string
		line      string
		candidate model.Candidate
		want      string
	}{
		{
			name:      "plain line strips a candidate variant",
			line:      "FROM nginx:1.27.0@" + digest,
			candidate: model.Candidate{Version: "1.31.2-alpine"},
			want:      "1.31.2",
		},
		{
			name:      "variant line keeps the variant",
			line:      "FROM nginx:1.27.0-alpine@" + digest,
			candidate: model.Candidate{Version: "1.31.2-alpine"},
			want:      "1.31.2-alpine",
		},
		{
			name:      "core follows the line's precision",
			line:      "FROM nginx:1.27@" + digest,
			candidate: model.Candidate{Version: "1.31.2"},
			want:      "1.31",
		},
	}

	rw := match.NewDockerPin()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			located, err := rw.Locate(tt.line)
			require.NoError(t, err)

			rendered, ok := located.(match.Renderer)
			require.True(t, ok, "dockerPinLocated implements match.Renderer")
			require.Equal(t, tt.want, rendered.Rendered(tt.candidate))
		})
	}
}

func TestDockerPinLocateErrors(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("a", 64)
	tests := []struct {
		name    string
		line    string
		wantErr string
	}{
		{"not pinned", "FROM nginx:1.27", "image is not digest-pinned by @sha256"},
		{"short digest", "FROM nginx:1.27@sha256:abc", "image pin requires a full sha256 digest"},
		{
			"non-sha256",
			"FROM nginx:1.27@md5:" + strings.Repeat("a", 64),
			"image pin digest must be sha256",
		},
		{"no tag", "FROM nginx@" + digest, "image has no tag to anchor the version"},
	}

	rw := match.NewDockerPin()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := rw.Locate(tt.line)
			require.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestDockerPinRenderRequiresDigest(t *testing.T) {
	t.Parallel()

	rw := match.NewDockerPin()
	line := "FROM nginx:1.27@sha256:" + strings.Repeat("a", 64)
	located, err := rw.Locate(line)
	require.NoError(t, err)

	_, _, err = located.Render(line, model.Candidate{Version: "1.29"}) // no digest
	require.EqualError(
		t,
		err,
		`candidate has no sha256 digest to pin, got ""`,
		"never half-updates",
	)
}
