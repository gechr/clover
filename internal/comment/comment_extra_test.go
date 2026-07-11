package comment_test

import (
	"testing"

	"github.com/gechr/clover/internal/comment"
	"github.com/stretchr/testify/require"
)

func TestSyntax_IsComment(t *testing.T) {
	t.Parallel()

	syntax := comment.Syntax{
		Line:   []string{"#", "//"},
		Blocks: []comment.Block{{Open: "<!--", Close: "-->"}},
	}

	tests := map[string]struct {
		syntax comment.Syntax
		line   string
		want   bool
	}{
		"line marker":               {syntax: syntax, line: "# clover: ...", want: true},
		"second line marker":        {syntax: syntax, line: "// note", want: true},
		"leading whitespace":        {syntax: syntax, line: "   \t# note", want: true},
		"marker mid-line":           {syntax: syntax, line: "code // note", want: false},
		"empty":                     {syntax: syntax, line: "", want: false},
		"block open":                {syntax: syntax, line: "<!-- note -->", want: true},
		"block open leading spaces": {syntax: syntax, line: "  <!-- note", want: true},
		"zero syntax always false":  {syntax: comment.Syntax{}, line: "# note", want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.syntax.IsComment(tc.line))
		})
	}
}
