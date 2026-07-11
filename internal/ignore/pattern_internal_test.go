package ignore

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCharClass(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		glob     string
		open     int
		want     string
		wantNext int
	}{
		"simple":           {glob: "[abc]", want: "[abc]", wantNext: 4},
		"range":            {glob: "[a-z]", want: "[a-z]", wantNext: 4},
		"bang negation":    {glob: "[!abc]", want: "[^abc]", wantNext: 5},
		"caret negation":   {glob: "[^abc]", want: "[^abc]", wantNext: 5},
		"leading bracket":  {glob: "[]a]", want: `[\]a]`, wantNext: 3},
		"backslash escape": {glob: `[\d]`, want: `[\\d]`, wantNext: 3},
		"unterminated":     {glob: "[abc", want: `\[`, wantNext: 0},
		"non-zero open":    {glob: "x[ab]", open: 1, want: "[ab]", wantNext: 4},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, next := charClass([]rune(tt.glob), tt.open)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantNext, next)
		})
	}
}
