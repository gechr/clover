package placeholder_test

import (
	"testing"

	"github.com/gechr/clover/internal/placeholder"
	"github.com/stretchr/testify/require"
)

func TestCompile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		glob       string
		line       string
		wantTokens []string
		wantValue  string // text of capture group 1, "" when no match expected
	}{
		{"version token", "<version>", "FROM nginx:1.27", []string{"version"}, "1.27"},
		{
			"literal context",
			"catalyst-<version>-linux",
			"x catalyst-1.2.3-linux y",
			[]string{"version"},
			"1.2.3",
		},
		{
			"star and two tokens",
			"<major.minor.patch>*<hex>.tgz",
			"tool-1.2.3-deadbeef.tgz",
			[]string{"major.minor.patch", "hex"},
			"1.2.3",
		},
		{"dot is literal", "app.<major.minor>", "app.1.27", []string{"major.minor"}, "1.27"},
		{"no match", "<version>", "no version here", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			re, tokens, err := placeholder.Compile(tt.glob)
			require.NoError(t, err)
			if tt.wantTokens != nil {
				require.Equal(t, tt.wantTokens, tokens)
			}

			m := re.FindStringSubmatch(tt.line)
			if tt.wantValue == "" {
				require.Nil(t, m)
				return
			}
			require.Equal(t, tt.wantValue, m[1])
		})
	}
}

func TestCompileErrors(t *testing.T) {
	t.Parallel()

	_, _, err := placeholder.Compile("<bogus>")
	require.ErrorContains(t, err, "unknown token <bogus>")

	// A stray, unclosed angle bracket is literal, not an error.
	_, _, err = placeholder.Compile("a < b")
	require.NoError(t, err)
}

func TestTokenBoundaries(t *testing.T) {
	t.Parallel()

	// Only the tight inner <version> is a token; the extra brackets are literal.
	re, tokens, err := placeholder.Compile("<<<<<version>>>>>>")
	require.NoError(t, err)
	require.Equal(t, []string{"version"}, tokens)
	require.True(t, re.MatchString("<<<<1.2.3>>>>>"))

	// A leading or trailing dot is not a token, so it stays literal text.
	require.False(t, placeholder.HasToken("<.version>"))
	require.False(t, placeholder.HasToken("<version.>"))
	require.True(t, placeholder.HasToken("<major.minor>"))
}

func TestExpand(t *testing.T) {
	t.Parallel()

	values := map[string]string{"version": "1.5.0", "commit": "abc123", "major.minor": "1.5"}
	require.Equal(t, "@abc123 # 1.5.0", placeholder.Expand("@<commit> # <version>", values))
	require.Equal(t, "1.5", placeholder.Expand("<major.minor>", values))
	require.Equal(t, "<sha256>", placeholder.Expand("<sha256>", values), "unprovided tokens stay")
	require.True(t, placeholder.HasToken("<sha256>"))
	require.False(t, placeholder.HasToken("1.5.0"))
}
