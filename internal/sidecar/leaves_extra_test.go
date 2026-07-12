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

// TestLeavesRepeatedKeyAcrossObjects confirms the same key name in sibling
// objects - the package-lock shape, where every entry carries "version" - is
// not mistaken for a duplicated key.
func TestLeavesRepeatedKeyAcrossObjects(t *testing.T) {
	t.Parallel()

	const src = `{
  "a": { "version": "1.0.0" },
  "b": { "version": "2.0.0" }
}`
	leaves, err := sidecar.Leaves([]byte(src))
	require.NoError(t, err)
	require.Len(t, leaves, 2)
	require.Equal(t, `.["a"]["version"]`, leaves[0].JQ)
	require.Equal(t, `.["b"]["version"]`, leaves[1].JQ)
}

// TestLeavesTrailingContent confirms content after the top-level value is
// rejected with json.Unmarshal's own message.
func TestLeavesTrailingContent(t *testing.T) {
	t.Parallel()

	_, err := sidecar.Leaves([]byte(`{"a": "1"} trailing`))
	require.EqualError(t, err, "invalid character 't' after top-level value")
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
