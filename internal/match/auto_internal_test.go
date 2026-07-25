package match

import (
	"testing"

	"github.com/gechr/clover/internal/pattern"
	"github.com/stretchr/testify/require"
)

func TestYAMLScalar(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in   string
		want string
	}{
		"bare":                  {in: "nginx:1.27", want: "nginx:1.27"},
		"leading space":         {in: "  nginx:1.27", want: "nginx:1.27"},
		"bare inline comment":   {in: "nginx:1.27 # pinned", want: "nginx:1.27"},
		"double quoted":         {in: `"nginx:1.27"`, want: "nginx:1.27"},
		"double quoted comment": {in: `"nginx:1.27" # pinned`, want: "nginx:1.27"},
		"single quoted":         {in: `'nginx:1.27'`, want: "nginx:1.27"},
		"single quoted comment": {in: `'nginx:1.27' # pinned`, want: "nginx:1.27"},
		// A \" is an escaped quote, not the close, so the real closing quote is found.
		"double escaped quote": {in: `"a\"b"`, want: `a"b`},
		"double escaped slash": {in: `"a\\b"`, want: `a\b`},
		// '' is YAML's escape for a literal single quote.
		"single doubled quote": {in: `'it''s'`, want: "it's"},
		// An unterminated quote falls back to the rest of the value.
		"unterminated double": {in: `"nginx:1.27`, want: "nginx:1.27"},
		"unterminated single": {in: `'nginx:1.27`, want: "nginx:1.27"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, yamlScalar(tc.in))
		})
	}
}

func TestAlternation(t *testing.T) {
	t.Parallel()

	require.Equal(t, "go|node", alternation([]string{"go", "node"}))
	require.Equal(
		t,
		`dotnet\.exe|xxh`,
		alternation([]string{"dotnet.exe", "xxh"}),
		"meta chars are quoted",
	)

	// An empty name list yields a group that matches nothing, so a route built
	// from an empty generated map cannot collapse to `()` and claim stray lines.
	require.Equal(t, `[^\s\S]`, alternation(nil))
	p, err := pattern.Compile(`/^\s*"?(` + alternation(nil) + `)"?\s*=\s*"/`)
	require.NoError(t, err)
	require.False(t, p.Matches(`foo = "1.2.3"`))
	require.False(t, p.Matches(` = "1.2.3"`))
}

// TestRouteInferenceNamesItsProvider is the drift guard for the deliberate
// redundancy between a route's provider guard and its inference. The guard
// serves rewriter dispatch (an explicit provider picks the rewriter) and the
// inference serves auto-detection, so the two are separate fields that must
// nonetheless always agree: a route that dispatched one provider and inferred
// another would resolve a marker against an upstream the line never named.
//
// init already asserts the two are present together; this pins the value. Every
// inference names its provider unconditionally, so an empty line is enough to
// read it back without depending on a shape.
func TestRouteInferenceNamesItsProvider(t *testing.T) {
	t.Parallel()

	empty := subject{lines: []string{""}, target: 0}
	for _, r := range routes {
		if r.infer == nil {
			continue
		}
		require.Equal(t, r.when.provider, r.infer(empty).Provider,
			"route %q infers a different provider than it dispatches", r.when.provider)
	}
}
