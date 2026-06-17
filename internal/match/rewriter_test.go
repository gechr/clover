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
