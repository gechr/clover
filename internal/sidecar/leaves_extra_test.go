package sidecar_test

import (
	"testing"

	"github.com/gechr/clover/internal/sidecar"
	"github.com/stretchr/testify/require"
)

// TestLeavesShadowedAncestorDropped confirms a leaf under a duplicated ancestor
// key is dropped when the resolver would descend the surviving (last-wins)
// member, whose value at the earlier path type-mismatches on re-resolution. The
// first "a" is an object, the second a string, so the path .["a"]["b"] no longer
// resolves to a string and that leaf is not emitted.
func TestLeavesShadowedAncestorDropped(t *testing.T) {
	t.Parallel()

	src := []byte("{\n  \"a\": { \"b\": \"nginx:1.27\" },\n  \"a\": \"redis:7.2\"\n}")

	leaves, err := sidecar.Leaves(src)
	require.NoError(t, err)
	require.Equal(t, []sidecar.Leaf{
		{Key: "a", Value: "redis:7.2", Line: 2, JQ: `.["a"]`},
	}, leaves, "the shadowed .a.b leaf is dropped; only the surviving .a scalar remains")
}

// TestLeavesRootScalar confirms a JSON document that is a bare string scalar
// yields a single leaf with an empty key (the path has no final key segment) and
// the identity locator.
func TestLeavesRootScalar(t *testing.T) {
	t.Parallel()

	leaves, err := sidecar.Leaves([]byte(`"just-a-string"`))
	require.NoError(t, err)
	require.Equal(t, []sidecar.Leaf{
		{Key: "", Value: "just-a-string", Line: 0, JQ: "."},
	}, leaves)
}
