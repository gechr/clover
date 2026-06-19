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
			out, changed, err := rw.Render(tt.line, located, model.Candidate{Version: tt.resolved})
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
	require.EqualError(t, err, "multiple hashes on the line; target is ambiguous")
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
