package match

import (
	"testing"

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
