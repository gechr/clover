package comment_test

import (
	"testing"

	"github.com/gechr/clover/internal/comment"
	"github.com/stretchr/testify/require"
)

func TestRender(t *testing.T) {
	tests := []struct {
		name string
		path string
		line string
		body string
		want string
	}{
		{
			name: "hash line comment",
			path: "Dockerfile",
			line: "# clover: provider=github",
			body: "clover: provider=github repo=a/b",
			want: "# clover: provider=github repo=a/b",
		},
		{
			name: "preserves indentation",
			path: "x.py",
			line: "    # clover: a=1",
			body: "clover: a=1 b=2",
			want: "    # clover: a=1 b=2",
		},
		{
			name: "normalises spacing after marker",
			path: "x.py",
			line: "#    clover: a=1",
			body: "clover: a=1",
			want: "# clover: a=1",
		},
		{
			name: "preserves trailing code before slash comment",
			path: "main.go",
			line: "code() // clover: a=1",
			body: "clover: a=1 b=2",
			want: "code() // clover: a=1 b=2",
		},
		{
			name: "html block comment",
			path: "index.html",
			line: "<!-- clover: a=1 -->",
			body: "clover: a=1 b=2",
			want: "<!-- clover: a=1 b=2 -->",
		},
		{
			name: "block comment normalises inner spacing",
			path: "index.html",
			line: "<!--clover: a=1-->",
			body: "clover: a=1",
			want: "<!-- clover: a=1 -->",
		},
		{
			name: "block preserves trailing after close",
			path: "index.html",
			line: "<!-- clover: a=1 --> trailing",
			body: "clover: a=1",
			want: "<!-- clover: a=1 --> trailing",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := comment.For(tc.path).Render(tc.line, tc.body)
			require.True(t, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestRenderNoComment(t *testing.T) {
	got, ok := comment.For("main.go").Render("just code", "clover: a=1")
	require.False(t, ok)
	require.Equal(t, "just code", got)
}

// TestRenderRoundTripsBody confirms Render and Body are inverses: the body read
// back from a rendered line equals the body that was written.
func TestRenderRoundTripsBody(t *testing.T) {
	lines := map[string]string{
		"Dockerfile": "# clover: x=1",
		"main.go":    "code // clover: x=1",
		"index.html": "<!-- clover: x=1 -->",
		"styles.css": "/* clover: x=1 */",
	}
	for path, line := range lines {
		t.Run(path, func(t *testing.T) {
			syntax := comment.For(path)
			rendered, ok := syntax.Render(line, "clover: a=1 b=2")
			require.True(t, ok)
			body, ok := syntax.Body(rendered)
			require.True(t, ok)
			require.Equal(t, "clover: a=1 b=2", trim(body))
		})
	}
}

// trim removes the single leading/trailing space Render adds around a block body.
func trim(s string) string {
	if len(s) > 0 && s[0] == ' ' {
		s = s[1:]
	}
	if len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}
