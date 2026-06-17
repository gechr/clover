package comment_test

import (
	"testing"

	"github.com/gechr/cusp/internal/comment"
	"github.com/stretchr/testify/require"
)

func TestFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		wantLine []string
	}{
		{name: "yaml is hash", path: "config.yaml", wantLine: []string{"#"}},
		{name: "fish is hash", path: "config.fish", wantLine: []string{"#"}},
		{name: "dockerfile by name", path: "Dockerfile", wantLine: []string{"#"}},
		{name: "dockerfile suffixed", path: "deploy/Dockerfile.prod", wantLine: []string{"#"}},
		{name: "makefile by name", path: "Makefile", wantLine: []string{"#"}},
		{name: "go is slash", path: "internal/main.go", wantLine: []string{"//"}},
		{name: "terraform is hcl", path: "main.tf", wantLine: []string{"#", "//"}},
		{name: "html is block only", path: "index.html", wantLine: nil},
		{name: "sql is dash", path: "schema.sql", wantLine: []string{"--"}},
		{name: "extension case-insensitive", path: "CONFIG.YAML", wantLine: []string{"#"}},
		{name: "unknown falls back to generic", path: "mystery.xyz", wantLine: []string{"#", "//"}},
		{
			name:     "no extension falls back to generic",
			path:     "LICENSE",
			wantLine: []string{"#", "//"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.wantLine, comment.For(tt.path).Line)
		})
	}
}

func TestSyntaxBody(t *testing.T) {
	t.Parallel()

	var (
		hash  = comment.Syntax{Line: []string{"#"}}
		slash = comment.Syntax{
			Line:   []string{"//"},
			Blocks: []comment.Block{{Open: "/*", Close: "*/"}},
		}
		hcl = comment.Syntax{
			Line:   []string{"#", "//"},
			Blocks: []comment.Block{{Open: "/*", Close: "*/"}},
		}
		xml = comment.Syntax{Blocks: []comment.Block{{Open: "<!--", Close: "-->"}}}
	)

	tests := []struct {
		name     string
		syntax   comment.Syntax
		line     string
		wantBody string
		wantOK   bool
	}{
		{
			name:     "hash trailing",
			syntax:   hash,
			line:     "FROM nginx:1.25 # cusp: x",
			wantBody: " cusp: x",
			wantOK:   true,
		},
		{
			name:     "hash own line",
			syntax:   hash,
			line:     "# cusp: x",
			wantBody: " cusp: x",
			wantOK:   true,
		},
		{name: "no comment", syntax: hash, line: "FROM nginx:1.25", wantOK: false},
		{
			name:     "slash line",
			syntax:   slash,
			line:     "const v = 1 // cusp: x",
			wantBody: " cusp: x",
			wantOK:   true,
		},
		{
			name:     "slash block same line",
			syntax:   slash,
			line:     "/* cusp: x */",
			wantBody: " cusp: x ",
			wantOK:   true,
		},
		{
			name:     "block open no close",
			syntax:   xml,
			line:     "<!-- cusp: x",
			wantBody: " cusp: x",
			wantOK:   true,
		},
		{
			name:     "block closed",
			syntax:   xml,
			line:     "<!-- cusp: x -->",
			wantBody: " cusp: x ",
			wantOK:   true,
		},
		{
			name:     "earliest marker wins",
			syntax:   hcl,
			line:     "a # b // c",
			wantBody: " b // c",
			wantOK:   true,
		},
		{
			name:     "block beats later line",
			syntax:   slash,
			line:     "/* x */ // y",
			wantBody: " x ",
			wantOK:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body, ok := tt.syntax.Body(tt.line)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.wantBody, body)
		})
	}
}
