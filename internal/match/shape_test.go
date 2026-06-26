package match_test

import (
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/stretchr/testify/require"
)

func TestFindSingleToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want match.Token
	}{
		{
			name: "bare three part",
			line: "FROM nginx:1.27.0",
			want: match.Token{Span: match.Span{Start: 11, End: 17}, Core: "1.27.0"},
		},
		{
			name: "v prefix",
			line: "tag: v1.26.0",
			want: match.Token{Span: match.Span{Start: 5, End: 12}, Prefix: "v", Core: "1.26.0"},
		},
		{
			name: "two component",
			line: "image: redis:7.2",
			want: match.Token{Span: match.Span{Start: 13, End: 16}, Core: "7.2"},
		},
		{
			name: "single component",
			line: "node:20",
			want: match.Token{Span: match.Span{Start: 5, End: 7}, Core: "20"},
		},
		{
			name: "variant suffix",
			line: "FROM nginx:1.27-alpine",
			want: match.Token{Span: match.Span{Start: 11, End: 22}, Core: "1.27", Suffix: "alpine"},
		},
		{
			name: "compound variant suffix",
			line: "python:3.12-slim-bookworm",
			want: match.Token{
				Span:   match.Span{Start: 7, End: 25},
				Core:   "3.12",
				Suffix: "slim-bookworm",
			},
		},
		{
			name: "variant with its own version",
			line: "nginx:1.27-alpine3.19",
			want: match.Token{
				Span:   match.Span{Start: 6, End: 21},
				Core:   "1.27",
				Suffix: "alpine3.19",
			},
		},
		{
			name: "prerelease",
			line: "v2.0.0-rc.1",
			want: match.Token{
				Span:       match.Span{Start: 0, End: 11},
				Prefix:     "v",
				Core:       "2.0.0",
				Prerelease: "rc.1",
			},
		},
		{
			name: "build metadata",
			line: "1.2.3+build5",
			want: match.Token{Span: match.Span{Start: 0, End: 12}, Core: "1.2.3", Build: "build5"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := match.Find(tt.line)
			require.Len(t, got, 1)
			require.Equal(t, tt.want, got[0])
		})
	}
}

func TestFindRejects(t *testing.T) {
	t.Parallel()

	// Lines whose only version-ish text is not cleanly version-shaped, so Find
	// returns nothing (the smart rewriter then fails loud on zero matches).
	tests := []struct {
		name string
		line string
	}{
		{name: "four component", line: "build 1.2.3.4 here"},
		{name: "calver leading zero", line: "released 2024.01.15"},
		{name: "version in image name", line: "FROM eclipse-temurin:java25"},
		{name: "commit sha", line: "pin 9e91cb1a2b here"},
		{name: "digits run into letters", line: "value 1.2abc end"},
		{name: "no version", line: "FROM nginx:latest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Empty(t, match.Find(tt.line))
		})
	}
}

func TestFindMultiple(t *testing.T) {
	t.Parallel()

	// The smart rewriter uses the count to decide; here Find surfaces both so an
	// ambiguous line can be rejected upstream.
	got := match.Find("from 1.2.3 to 1.3.0")
	require.Len(t, got, 2)
	require.Equal(t, "1.2.3", got[0].Core)
	require.Equal(t, "1.3.0", got[1].Core)
}

func TestFindFourPartNoSubMatch(t *testing.T) {
	t.Parallel()

	// 1.2.3.4 must not yield a 1.2.3 sub-token: the trailing .4 disqualifies it
	// and the .4 itself cannot start a token (preceded by a dot).
	require.Empty(t, match.Find("x 1.2.3.4 y"))
}
