package match

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurePin_Pinned(t *testing.T) {
	t.Parallel()

	require.Equal(t, "abc123", securePin{pinned: "abc123"}.Pinned())
	require.Empty(t, securePin{}.Pinned())
}
