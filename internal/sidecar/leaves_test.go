package sidecar_test

import (
	"testing"

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
