package match_test

import (
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/stretchr/testify/require"
)

// TestForFallsBackToSmart confirms the dispatch table returns the smart rewriter
// for an ordinary line - the only route until format-specific rewriters land.
func TestForFallsBackToSmart(t *testing.T) {
	t.Parallel()

	rw := match.For(match.Context{
		Path:     "Dockerfile",
		Line:     "FROM nginx:1.27.0",
		Provider: "github",
	})
	require.IsType(t, match.Smart{}, rw)
}

// TestMiseFile confirms both file shapes mise reads tool pins from count as
// mise files, so a bare single-number pin gets major precision in either.
func TestMiseFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{path: ".mise.toml", want: true},
		{path: "sub/mise.toml", want: true},
		{path: ".tool-versions", want: true},
		{path: "sub/.tool-versions", want: true},
		{path: "Cargo.toml", want: false},
		{path: "tool-versions", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, match.MiseFile(tt.path))
		})
	}
}

// TestForContainerJobUses confirms a workflow container job's uses: docker://
// reference routes to the docker rewriters, not the action ones: digest-pinned
// to docker-pin, tag-only to docker-tag.
func TestForContainerJobUses(t *testing.T) {
	t.Parallel()

	digest := "      - uses: docker://alpine:3.20@sha256:0123456789012345678901234567890123456789012345678901234567890123"
	pinned := match.For(
		match.Context{Path: ".github/workflows/ci.yml", Line: digest, Provider: "docker"},
	)
	require.IsType(t, match.DockerPin{}, pinned)

	tagOnly := match.For(match.Context{
		Path:     ".github/workflows/ci.yml",
		Line:     "      - uses: docker://alpine:3.20",
		Provider: "docker",
	})
	require.IsType(t, match.DockerTag{}, tagOnly)
}

// TestForDigestPinnedDocker confirms a digest-pinned docker line routes to the
// docker-pin rewriter, while a tag-only one routes to the docker-tag rewriter.
func TestForDigestPinnedDocker(t *testing.T) {
	t.Parallel()

	digest := "FROM nginx:1.27@sha256:0123456789012345678901234567890123456789012345678901234567890123"
	pinned := match.For(match.Context{Path: "Dockerfile", Line: digest, Provider: "docker"})
	require.IsType(t, match.DockerPin{}, pinned)

	tagOnly := match.For(
		match.Context{Path: "Dockerfile", Line: "FROM nginx:1.27", Provider: "docker"},
	)
	require.IsType(t, match.DockerTag{}, tagOnly)
}
