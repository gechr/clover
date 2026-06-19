package github_test

import (
	"testing"

	"github.com/gechr/clover/internal/report/github"
	"github.com/stretchr/testify/require"
)

func TestAnnotations(t *testing.T) {
	t.Parallel()

	require.Equal(t, "::error file=main.go,line=2::boom", github.Error("main.go", 2, "boom"))
	require.Equal(t, "::warning file=main.go,line=2::stale", github.Warning("main.go", 2, "stale"))
	require.Equal(t, "::notice file=main.go,line=2::fyi", github.Notice("main.go", 2, "fyi"))
}

func TestLocationOmitted(t *testing.T) {
	t.Parallel()

	// No file drops the whole location; a non-positive line drops just the line.
	require.Equal(t, "::error::boom", github.Error("", 0, "boom"))
	require.Equal(t, "::error file=main.go::boom", github.Error("main.go", 0, "boom"))
}

func TestEscaping(t *testing.T) {
	t.Parallel()

	// A property escapes : and , (and %, CR, LF); a message escapes %, CR, LF.
	require.Equal(t,
		"::error file=a%3Ab%2Cc.go,line=1::100%25 of lines%0Afailed",
		github.Error("a:b,c.go", 1, "100% of lines\nfailed"),
	)
}
