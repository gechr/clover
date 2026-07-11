package sidecar_test

import (
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/sidecar"
	"github.com/stretchr/testify/require"
)

// depsJSON is a multi-line JSON fixture exercising nested objects, an array, an
// empty array, and a version embedded in a URL. Line indices (0-based):
//
//	0  {
//	1    "name": "app",
//	2    "dependencies": {
//	3      "react": "18.2.0",
//	4      "left-pad": "1.3.0"
//	5    },
//	6    "versions": [
//	7      "1.0.0",
//	8      "2.0.0"
//	9    ],
//	10   "empty": [],
//	11   "$schema": "https://example.test/schemas/2.4.14/schema.json"
//	12 }
const depsJSON = `{
  "name": "app",
  "dependencies": {
    "react": "18.2.0",
    "left-pad": "1.3.0"
  },
  "versions": [
    "1.0.0",
    "2.0.0"
  ],
  "empty": [],
  "$schema": "https://example.test/schemas/2.4.14/schema.json"
}`

func jqLocate(t *testing.T, source, expr string) (int, error) {
	t.Helper()
	d := directive.Directive{Pairs: []directive.KV{{Key: "jq", Value: expr}}}
	return sidecar.Locate(strings.Split(source, "\n"), d)
}

func TestLocateJQ(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr string
		want int
	}{
		{"nested object key", ".dependencies.react", 3},
		{"sibling object key", ".dependencies[\"left-pad\"]", 4},
		{"bracketed top key", `.["$schema"]`, 11},
		{"object value lands on its opening brace", ".dependencies", 2},
		{"array element by index", ".versions[1]", 8},
		{"negative array index counts from the end", ".versions[-1]", 8},
		{"negative array index, second from end", ".versions[-2]", 7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			line, err := jqLocate(t, depsJSON, tc.expr)
			require.NoError(t, err)
			require.Equal(t, tc.want, line)
		})
	}
}

func TestLocateJQAmbiguous(t *testing.T) {
	t.Parallel()

	_, err := jqLocate(t, depsJSON, ".versions[]")
	require.EqualError(t, err, "jq matched 2 values - narrow it")
}

func TestLocateJQNoMatch(t *testing.T) {
	t.Parallel()

	_, err := jqLocate(t, depsJSON, ".empty[]")
	require.EqualError(t, err, "jq matched nothing")
}

// A computed (non-path) expression cannot be a locator; gojq reports it at run
// time.
func TestLocateJQNonPath(t *testing.T) {
	t.Parallel()

	_, err := jqLocate(t, depsJSON, ".name | ascii_upcase")
	require.EqualError(t, err, `"jq" locator: invalid path against: string ("APP")`)
}

func TestLocateJQNonJSON(t *testing.T) {
	t.Parallel()

	_, err := jqLocate(t, "not json at all\n", ".version")
	require.EqualError(
		t,
		err,
		`"jq" locator requires a JSON target: invalid character 'o' in literal null (expecting 'u')`,
	)
}

// A duplicated object key is last-value-wins in gojq and encoding/json, so the
// path resolves to the final occurrence - the byte walk must agree, or it would
// rewrite the wrong line.
func TestLocateJQDuplicateKey(t *testing.T) {
	t.Parallel()

	const dup = `{
  "pkg": "1.0.0",
  "pkg": "2.0.0"
}`
	line, err := jqLocate(t, dup, ".pkg")
	require.NoError(t, err)
	require.Equal(t, 2, line, "the last pkg (2.0.0), matching gojq's value")
}

// Last-wins also applies to a duplicated ancestor key: descent follows the final
// matching member at each object level.
func TestLocateJQDuplicateAncestorKey(t *testing.T) {
	t.Parallel()

	const dup = `{
  "dep": { "v": "1.0.0" },
  "dep": { "v": "2.0.0" }
}`
	line, err := jqLocate(t, dup, ".dep.v")
	require.NoError(t, err)
	require.Equal(t, 2, line, "v inside the last dep")
}
