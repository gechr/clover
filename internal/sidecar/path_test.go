package sidecar_test

import (
	"strings"
	"testing"

	"github.com/gechr/clover/internal/sidecar"
	"github.com/stretchr/testify/require"
)

func TestParsePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr string
		want []any
	}{
		{"root", ".", []any{}},
		{"dot ident", ".a", []any{"a"}},
		{"dotted chain", ".a.b.c", []any{"a", "b", "c"}},
		{"underscore ident", "._x.y_2", []any{"_x", "y_2"}},
		{"bracket key", `.["k"]`, []any{"k"}},
		{"bracket key with spaces", `.["k with spaces"]`, []any{"k with spaces"}},
		{"bracket key with escaped quote", `.["k\"q"]`, []any{`k"q`}},
		{"bracket key with escaped backslash", `.["k\\q"]`, []any{`k\q`}},
		{"bracket key with tab escape", `.["k\there"]`, []any{"k\there"}},
		{"empty key", `.[""]`, []any{""}},
		{"index", ".[0]", []any{0}},
		{"negative index", ".[-1]", []any{-1}},
		{"lone zero", ".[0].x", []any{0, "x"}},
		{"mixed chain", `.a["b c"][2].d[-3]`, []any{"a", "b c", 2, "d", -3}},
		{"dot then bracket", `.dependencies["left-pad"]`, []any{"dependencies", "left-pad"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path, ok := sidecar.ParsePath(tc.expr)
			require.True(t, ok)
			require.Equal(t, tc.want, path)
		})
	}
}

func TestParsePathRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr string
	}{
		{"empty", ""},
		{"no leading dot", "a"},
		{"bare pipe", ".a | .b"},
		{"optional chaining", ".a?"},
		{"double dot", "..a"},
		{"trailing dot", ".a."},
		{"wildcard", ".[]"},
		{"slice", ".[1:2]"},
		{"unterminated bracket", ".a["},
		{"unterminated string", `.["k]`},
		{"missing close after string", `.["k" ]`},
		{"trailing garbage after bracket", `.["a"]x!`},
		{"leading zero index", ".[00]"},
		{"plus index", ".[+1]"},
		{"spaced index", ".[ 0]"},
		{"float index", ".[1.5]"},
		{"function call", ".a|first"},
		{"parenthesized", "(.a)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, ok := sidecar.ParsePath(tc.expr)
			require.False(t, ok)
		})
	}
}

// TestLocatorStructuralMismatchUsesGojqError confirms a literal path that
// mis-indexes the document's shape reports gojq's evaluation error, exactly as
// the pre-index resolution did.
func TestLocatorStructuralMismatchUsesGojqError(t *testing.T) {
	t.Parallel()

	locator := sidecar.NewLocator(strings.Split(depsJSON, "\n"))

	_, err := locator.Locate(jqDirective(".name.x"))
	require.EqualError(t, err, `"jq" locator: expected an object but got: string ("app")`)

	_, err = locator.Locate(jqDirective(".versions.k"))
	require.EqualError(
		t,
		err,
		`"jq" locator: expected an object but got: array (["1.0.0","2.0.0"])`,
	)

	_, err = locator.Locate(jqDirective(".dependencies[0]"))
	require.EqualError(
		t,
		err,
		`"jq" locator: expected an array but got: object ({"left-pad":"1.3.0","reac ...})`,
	)
}
