package match_test

import (
	"strings"
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/stretchr/testify/require"
)

func TestHashRender(t *testing.T) {
	t.Parallel()

	old64 := strings.Repeat("a", 64)
	new64 := strings.Repeat("b", 64)
	old40 := strings.Repeat("c", 40)
	new40 := strings.Repeat("d", 40)

	tests := []struct {
		name     string
		line     string
		resolved string
		want     string
	}{
		{
			name:     "sha256 assignment",
			line:     "TOOL_SHA256=" + old64,
			resolved: new64,
			want:     "TOOL_SHA256=" + new64,
		},
		{
			name:     "commit pin in quotes",
			line:     `  rev = "` + old40 + `"`,
			resolved: new40,
			want:     `  rev = "` + new40 + `"`,
		},
	}

	rw := match.NewHash()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			located, err := rw.Locate(tt.line)
			require.NoError(t, err)
			out, changed, err := located.Render(tt.line, model.Candidate{Version: tt.resolved})
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, tt.want, out)
		})
	}
}

func TestHashLocateErrors(t *testing.T) {
	t.Parallel()

	rw := match.NewHash()

	_, err := rw.Locate("VERSION=1.2.3")
	require.EqualError(t, err, "no commit or sha256 hash on the target line")

	twoHashes := "a=" + strings.Repeat("a", 64) + " b=" + strings.Repeat("b", 64)
	_, err = rw.Locate(twoHashes)
	require.EqualError(t, err, "multiple hashes on the line, so the target is ambiguous")
}

func TestForRoutesFollowerHashes(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"commit", "sha256"} {
		rw := match.For(
			match.Context{Line: "X=" + strings.Repeat("a", 64), Provider: "follow", Value: value},
		)
		require.IsType(t, match.Hash{}, rw, value)
	}
}

// A follower must reach the hash rewriter wherever its target line sits. The
// dispatch table is ordered, so a route whose guards a follower's context
// happens to satisfy would shadow it - and a follower's target line is a bare
// hex token, which is exactly what a secure-pin route matches. Pinning the
// combination (follower context, a path some route guards, a line some route
// matches) is what a per-route test cannot see.
func TestFollowerDispatchIsNotShadowed(t *testing.T) {
	t.Parallel()

	const sha = "552baf822992936134cbd31a38f69c8cfe7c0f05"

	tests := []struct {
		name  string
		path  string
		line  string
		value string
	}{
		{
			// The frozen pre-commit route matches a bare hex rev in this very file.
			name:  "commit follower on a pre-commit rev line",
			path:  ".pre-commit-config.yaml",
			line:  "  rev: " + sha,
			value: "commit",
		},
		{
			name:  "sha256 follower on a pre-commit line",
			path:  ".pre-commit-config.yaml",
			line:  "  rev: " + strings.Repeat("a", 64),
			value: "sha256",
		},
		{
			// A workflow line a uses: route would otherwise claim.
			name:  "commit follower on a workflow uses: line",
			path:  ".github/workflows/ci.yml",
			line:  "      uses: owner/repo@" + sha,
			value: "commit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rw := match.For(match.Context{
				Path:     tt.path,
				Line:     tt.line,
				Provider: "follow",
				Value:    tt.value,
			})
			require.IsType(t, match.Hash{}, rw)
		})
	}
}
