package regexlit_test

import (
	"testing"

	"github.com/gechr/clover/internal/regexlit"
	"github.com/stretchr/testify/require"
)

func TestIs(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"/foo/", true},
		{"/foo.*/", true},
		{"//", true},          // empty regex
		{`/a\/b/`, true},      // escaped slash does not close
		{"/a/b/", false},      // unescaped internal slash closes early
		{"/foo", false},       // unterminated
		{"foo/", false},       // not opened by a delimiter
		{"/", false},          // a lone slash is a literal slash, not a regex
		{"", false},           // empty
		{"foo", false},        // bare value
		{"owner/name", false}, // internal slash, no leading delimiter
		{`/a\\/`, true},       // escaped backslash, then closing slash
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			require.Equal(t, tc.want, regexlit.Is(tc.in))
		})
	}
}

func TestBody(t *testing.T) {
	tests := []struct {
		in       string
		wantBody string
		wantOK   bool
	}{
		{"/foo/", "foo", true},
		{"//", "", true},
		{`/a\/b/`, `a\/b`, true}, // escape kept intact for the regex engine
		{"/foo", "", false},
		{"plain", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			body, ok := regexlit.Body(tc.in)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.wantBody, body)
		})
	}
}
