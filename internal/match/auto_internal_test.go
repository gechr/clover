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
