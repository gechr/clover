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
