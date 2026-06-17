package directive_test

import (
	"testing"

	"github.com/gechr/cusp/internal/directive"
	"github.com/stretchr/testify/require"
)

func TestParseNotFound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "no keyword", body: " FROM nginx:1.25"},
		{name: "keyword not leading", body: " note about cusp: not a directive"},
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
			body: " cusp: provider=github constraint=minor",
			want: []directive.KV{
				{Key: "provider", Value: "github"},
				{Key: "constraint", Value: "minor"},
			},
		},
		{
			name: "trailing comment body",
			body: " cusp: image=nginx",
			want: []directive.KV{{Key: "image", Value: "nginx"}},
		},
		{
			name: "explicit booleans",
			body: "cusp: skip=true force=false",
			want: []directive.KV{{Key: "skip", Value: "true"}, {Key: "force", Value: "false"}},
		},
		{
			name: "double-quoted value with spaces",
			body: `cusp: include="/foo bar/"`,
			want: []directive.KV{{Key: "include", Value: "/foo bar/"}},
		},
		{
			name: "single-quoted value with spaces",
			body: `cusp: include='/foo bar/'`,
			want: []directive.KV{{Key: "include", Value: "/foo bar/"}},
		},
		{
			name: "backtick-quoted value with spaces",
			body: "cusp: include=`/foo bar/`",
			want: []directive.KV{{Key: "include", Value: "/foo bar/"}},
		},
		{
			name: "other quote chars literal inside",
			body: `cusp: msg="it's a 'test'"`,
			want: []directive.KV{{Key: "msg", Value: "it's a 'test'"}},
		},
		{
			name: "regex self-delimits without quotes",
			body: "cusp: include=/foo bar/",
			want: []directive.KV{{Key: "include", Value: "/foo bar/"}},
		},
		{
			name: "regex keeps escaped slash",
			body: `cusp: include=/a\/b/`,
			want: []directive.KV{{Key: "include", Value: `/a\/b/`}},
		},
		{
			name: "slash literal mid bare value",
			body: "cusp: repo=owner/name",
			want: []directive.KV{{Key: "repo", Value: "owner/name"}},
		},
		{
			name: "apostrophe literal mid bare value",
			body: "cusp: msg=don't",
			want: []directive.KV{{Key: "msg", Value: "don't"}},
		},
		{
			name: "bare range constraint",
			body: "cusp: constraint=>=1.2,<2.0",
			want: []directive.KV{{Key: "constraint", Value: ">=1.2,<2.0"}},
		},
		{
			name: "only first equals splits",
			body: "cusp: x=a=b",
			want: []directive.KV{{Key: "x", Value: "a=b"}},
		},
		{
			name: "repeated keys preserved in order",
			body: "cusp: include=a include=b",
			want: []directive.KV{{Key: "include", Value: "a"}, {Key: "include", Value: "b"}},
		},
		{
			name: "block comment body trimmed",
			body: " cusp: skip=true ",
			want: []directive.KV{{Key: "skip", Value: "true"}},
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
		{name: "unterminated quote", body: `cusp: include="oops`},
		{name: "mismatched quote", body: `cusp: x='a"`},
		{name: "unterminated regex", body: "cusp: include=/oops"},
		{name: "bare key without value", body: "cusp: skip"},
		{name: "bare key among pairs", body: "cusp: provider=github skip"},
		{name: "empty key", body: "cusp: =foo"},
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
