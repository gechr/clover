package sidecar_test

import (
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/sidecar"
	"github.com/stretchr/testify/require"
)

// TestLeavesEnumeratesStringScalars confirms Leaves returns every string leaf
// with a verified jq locator and its line, descending nested objects and arrays,
// in line order. Non-string scalars and array-indexed leaves carry no object key
// and are excluded.
func TestLeavesEnumeratesStringScalars(t *testing.T) {
	t.Parallel()

	src := []byte(`{
  "spec": {
    "containers": [
      { "image": "nginx:1.27" },
      { "image": "ghcr.io/owner/api:1.2.0" }
    ]
  },
  "$schema": "https://example.test/schemas/1.5.3/schema.json",
  "replicas": 3
}`)

	leaves, err := sidecar.Leaves(src)
	require.NoError(t, err)
	require.Equal(t, []sidecar.Leaf{
		{Key: "image", Value: "nginx:1.27", Line: 3, JQ: `.["spec"]["containers"][0]["image"]`},
		{
			Key:   "image",
			Value: "ghcr.io/owner/api:1.2.0",
			Line:  4,
			JQ:    `.["spec"]["containers"][1]["image"]`,
		},
		{
			Key:   "$schema",
			Value: "https://example.test/schemas/1.5.3/schema.json",
			Line:  7,
			JQ:    `.["$schema"]`,
		},
	}, leaves, "a numeric replicas leaf is excluded; each jq locator round-trips to its own line")
}

// TestLeavesSameLineDeterministicOrder confirms two leaves sharing a line are
// returned in document (byte-offset) order, not the map's nondeterministic
// iteration order. The compact source puts both image leaves on line 0.
func TestLeavesSameLineDeterministicOrder(t *testing.T) {
	t.Parallel()

	src := []byte(`{"a":{"image":"nginx:1.27"},"b":{"image":"redis:6.0"}}`)
	for range 50 {
		leaves, err := sidecar.Leaves(src)
		require.NoError(t, err)
		require.Equal(t, []sidecar.Leaf{
			{Key: "image", Value: "nginx:1.27", Line: 0, JQ: `.["a"]["image"]`},
			{Key: "image", Value: "redis:6.0", Line: 0, JQ: `.["b"]["image"]`},
		}, leaves, "same-line leaves keep document order on every run")
	}
}

// TestLeavesControlCharKeyLocator confirms a key carrying a control character
// yields a locator escaped the way jq's lexer accepts (\uXXXX), not Go's \xXX, so
// the generated locator still round-trips through the resolver at apply time. The
// source embeds the key as a JSON unicode escape (a raw control byte is not
// legal JSON).
func TestLeavesControlCharKeyLocator(t *testing.T) {
	t.Parallel()

	src := []byte("{\"\\u0001\":\"nginx:1.27\"}")
	leaves, err := sidecar.Leaves(src)
	require.NoError(t, err)
	require.Len(t, leaves, 1)
	require.Equal(t, ".[\"\\u0001\"]", leaves[0].JQ)

	line, err := sidecar.Locate(
		strings.Split(string(src), "\n"),
		directive.Directive{Pairs: []directive.KV{{Key: "jq", Value: leaves[0].JQ}}},
	)
	require.NoError(t, err)
	require.Equal(t, 0, line)
}

// TestLeavesRejectsNonJSON confirms a target that is not JSON has no locatable
// structure, so Leaves errors rather than guessing.
func TestLeavesRejectsNonJSON(t *testing.T) {
	t.Parallel()

	_, err := sidecar.Leaves([]byte("not: json: at: all\n"))
	require.Error(t, err)
}

// TestLeavesDuplicateKeyLastWins confirms a duplicated object key resolves to its
// final member, matching the last-value-wins semantics of the jq resolver.
func TestLeavesDuplicateKeyLastWins(t *testing.T) {
	t.Parallel()

	src := []byte("{\n  \"image\": \"nginx:1.27\",\n  \"image\": \"redis:7.2\"\n}")

	leaves, err := sidecar.Leaves(src)
	require.NoError(t, err)
	require.Equal(t, []sidecar.Leaf{
		{Key: "image", Value: "redis:7.2", Line: 2, JQ: `.["image"]`},
	}, leaves)
}
