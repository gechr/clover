package command_test

import (
	"testing"

	"github.com/gechr/clover/internal/command"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestDeepHintsShallowDeduplicates(t *testing.T) {
	t.Parallel()

	nginx := provider.Truncation{
		Resource: "ghcr.io/owner/nginx",
		URL:      "https://ghcr.io/owner/nginx",
	}
	redis := provider.Truncation{
		Resource: "ghcr.io/owner/redis",
		URL:      "https://ghcr.io/owner/redis",
	}

	// Only shallow lookups feed the truncation sink, so every truncated resource
	// warrants a warning, deduplicated.
	resources := command.DeepHints([]provider.Truncation{nginx, nginx, redis})
	require.Equal(t, []provider.Truncation{nginx, redis}, resources)
}

func TestDeepHintsNoTruncationNoHint(t *testing.T) {
	t.Parallel()

	// Nothing truncated, nothing to suggest - a no-candidate failure explains
	// itself in its own error rather than through a separate hint.
	require.Empty(t, command.DeepHints(nil))
}
