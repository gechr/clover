package directive_test

import (
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/stretchr/testify/require"
)

func TestParseNotFound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "no keyword", body: " FROM nginx:1.25"},
		{name: "keyword not leading", body: " note about clover: not a directive"},
		{name: "auto keyword not leading", body: " see @clover for details"},
		{name: "auto keyword unclosed", body: "@cloverfield is a film"},
		{name: "auto keyword embedded in a word", body: "@cloverfield"},
		{name: "auto keyword with prose", body: "@clover please review this"},
		{name: "keyword with prose", body: "clover run updates the line below"},
		{name: "empty", body: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, found, err := directive.Parse(tt.body)
			require.NoError(t, err)
			require.False(t, found)
		})
	}
}

func TestParsePairs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []directive.KV
	}{
		{
			name: "own line",
			body: " clover: provider=github constraint=minor",
			want: []directive.KV{
				{Key: "provider", Value: "github"},
				{Key: "constraint", Value: "minor"},
			},
		},
		{
			name: "bare auto shorthand",
			body: " @clover",
			want: []directive.KV{{Key: "provider", Value: "auto"}},
		},
		{
			name: "bare auto shorthand with trailing space",
			body: "@clover ",
			want: []directive.KV{{Key: "provider", Value: "auto"}},
		},
		{
			name: "auto shorthand with pairs",
			body: "@clover: constraint=minor",
			want: []directive.KV{
				{Key: "provider", Value: "auto"},
				{Key: "constraint", Value: "minor"},
			},
		},
		{
			name: "auto shorthand with empty pairs",
			body: "@clover:",
			want: []directive.KV{{Key: "provider", Value: "auto"}},
		},
		{
			name: "explicit provider wins over auto shorthand",
			body: "@clover: provider=github",
			want: []directive.KV{{Key: "provider", Value: "github"}},
		},
		{
			name: "trailing comment body",
			body: " clover: image=nginx",
			want: []directive.KV{{Key: "image", Value: "nginx"}},
		},
		{
			name: "explicit booleans",
			body: "clover: disabled=true force=false",
			want: []directive.KV{{Key: "disabled", Value: "true"}, {Key: "force", Value: "false"}},
		},
		{
			name: "double-quoted value with spaces",
			body: `clover: include="/foo bar/"`,
			want: []directive.KV{{Key: "include", Value: "/foo bar/"}},
		},
		{
			name: "single-quoted value with spaces",
			body: `clover: include='/foo bar/'`,
			want: []directive.KV{{Key: "include", Value: "/foo bar/"}},
		},
		{
			name: "backtick-quoted value with spaces",
			body: "clover: include=`/foo bar/`",
			want: []directive.KV{{Key: "include", Value: "/foo bar/"}},
		},
		{
			name: "other quote chars literal inside",
			body: `clover: msg="it's a 'test'"`,
			want: []directive.KV{{Key: "msg", Value: "it's a 'test'"}},
		},
		{
			name: "regex self-delimits without quotes",
			body: "clover: include=/foo bar/",
			want: []directive.KV{{Key: "include", Value: "/foo bar/"}},
		},
		{
			name: "regex keeps escaped slash",
			body: `clover: include=/a\/b/`,
			want: []directive.KV{{Key: "include", Value: `/a\/b/`}},
		},
		{
			name: "slash literal mid bare value",
			body: "clover: repository=owner/name",
			want: []directive.KV{{Key: "repository", Value: "owner/name"}},
		},
		{
			name: "apostrophe literal mid bare value",
			body: "clover: msg=don't",
			want: []directive.KV{{Key: "msg", Value: "don't"}},
		},
		{
			name: "bare range constraint",
			body: "clover: constraint=>=1.2,<2.0",
			want: []directive.KV{{Key: "constraint", Value: ">=1.2,<2.0"}},
		},
		{
			name: "only first equals splits",
			body: "clover: x=a=b",
			want: []directive.KV{{Key: "x", Value: "a=b"}},
		},
		{
			name: "repeated keys preserved in order",
			body: "clover: include=a include=b",
			want: []directive.KV{{Key: "include", Value: "a"}, {Key: "include", Value: "b"}},
		},
		{
			name: "block comment body trimmed",
			body: " clover: disabled=true ",
			want: []directive.KV{{Key: "disabled", Value: "true"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d, found, err := directive.Parse(tt.body)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, tt.want, d.Pairs)
		})
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "unterminated quote", body: `clover: include="oops`},
		{name: "mismatched quote", body: `clover: x='a"`},
		{name: "unterminated regex", body: "clover: include=/oops"},
		{name: "bare key without value", body: "clover: disabled"},
		{name: "bare key among pairs", body: "clover: provider=github disabled"},
		{name: "auto shorthand with prose after colon", body: "@clover: pins this line"},
		{name: "auto shorthand missing colon before pairs", body: "@clover constraint=minor"},
		{name: "auto shorthand detached colon", body: "@clover : constraint=minor"},
		{name: "keyword missing colon before pairs", body: "clover foo=bar"},
		{name: "keyword detached colon", body: "clover : foo=bar"},
		{name: "empty key", body: "clover: =foo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, found, err := directive.Parse(tt.body)
			require.Error(t, err)
			require.True(t, found)
		})
	}
}
