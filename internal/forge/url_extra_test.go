package forge_test

import (
	"testing"

	"github.com/gechr/clover/internal/forge"
	"github.com/stretchr/testify/require"
)

func TestSameOriginMalformedFirst(t *testing.T) {
	t.Parallel()

	// A malformed first URL fails the initial parse before the second is read.
	require.False(t, forge.SameOrigin("://bad", "https://codeberg.org/x"))
}
