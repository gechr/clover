package command_test

import (
	"testing"

	"github.com/gechr/clover/internal/command"
	"github.com/stretchr/testify/require"
)

func TestDeepHintsShallowDeduplicates(t *testing.T) {
	t.Parallel()

	// A shallow run warns about each truncated resource, deduplicated.
	resources := command.DeepHints(
		[]string{"docker.io/library/nginx", "docker.io/library/nginx", "github.com/owner/name"},
		false,
	)
	require.Equal(t, []string{"docker.io/library/nginx", "github.com/owner/name"}, resources)
}

func TestDeepHintsDeepSuggestsNothing(t *testing.T) {
	t.Parallel()

	// A deep run already paged to exhaustion, so it never re-suggests --deep.
	require.Empty(t, command.DeepHints([]string{"docker.io/library/nginx"}, true))
}

func TestDeepHintsNoTruncationNoHint(t *testing.T) {
	t.Parallel()

	// Nothing truncated, nothing to suggest - a no-candidate failure explains
	// itself in its own error rather than through a separate hint.
	require.Empty(t, command.DeepHints(nil, false))
}
