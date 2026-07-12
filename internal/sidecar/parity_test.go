package sidecar_test

import (
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/sidecar"
	"github.com/stretchr/testify/require"
)

// TestLocatorMatchesByteWalk resolves every locator Leaves generates through
// both the shared index and the per-entry byte walk, so the fast path can
// never silently drift from the last-value-wins reference resolution.
func TestLocatorMatchesByteWalk(t *testing.T) {
	t.Parallel()

	docs := map[string]string{
		"nested objects and arrays": `{
  "name": "app",
  "dependencies": {
    "react": "18.2.0",
    "left-pad": "1.3.0"
  },
  "matrix": [
    ["a", "b"],
    ["c", "d"]
  ],
  "$schema": "https://example.test/schemas/2.4.14/schema.json"
}`,
		"escaped and non-ASCII keys": `{
  "with \"quote\"": "1.0.0",
  "back\\slash": "2.0.0",
  "tab\there": "3.0.0",
  "naïve": "4.0.0",
  "": "5.0.0"
}`,
		"compact single line": `{"a": "1", "b": {"c": "2"}, "d": ["3", "4"]}`,
		"root array":          `["1.0.0", {"v": "2.0.0"}]`,
		"root scalar":         `"1.0.0"`,
	}

	for name, doc := range docs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source := []byte(doc)
			leaves, err := sidecar.Leaves(source)
			require.NoError(t, err)
			require.NotEmpty(t, leaves)

			locator := sidecar.NewLocator(strings.Split(doc, "\n"))
			for _, leaf := range leaves {
				d := directive.Directive{Pairs: []directive.KV{{Key: "jq", Value: leaf.JQ}}}

				got, err := locator.Locate(d)
				require.NoError(t, err, leaf.JQ)

				want, err := sidecar.ResolveJQLine(source, leaf.JQ)
				require.NoError(t, err, leaf.JQ)

				require.Equal(t, want, got, leaf.JQ)
				require.Equal(t, leaf.Line, got, leaf.JQ)
			}
		})
	}
}
