package match_test

import (
	"strings"
	"testing"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/stretchr/testify/require"
)

func TestGuardedDelegatesAfterMatch(t *testing.T) {
	t.Parallel()

	oldHex := strings.Repeat("a", 64)
	newDigest := constant.DigestSha256 + strings.Repeat("b", 64)
	rw, err := match.NewGuarded("x/y:<version>", match.NewDockerPin())
	require.NoError(t, err)

	line := `"image": "x/y:1.0.0` + constant.DockerDigestMarker + oldHex + `"`
	located, err := rw.Locate(line)
	require.NoError(t, err)
	require.True(t, located.NeedsDigest())

	got, changed, err := located.Render(line, model.Candidate{
		Version: "1.2.0",
		Digest:  newDigest,
	})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, `"image": "x/y:1.2.0@`+newDigest+`"`, got)
}

func TestGuardedRejectsDriftedLine(t *testing.T) {
	t.Parallel()

	rw, err := match.NewGuarded("x/y:<version>", match.NewDockerPin())
	require.NoError(t, err)

	_, err = rw.Locate(`"image": "other/app:1.0.0` + constant.DockerDigestMarker +
		strings.Repeat("a", 64) + `"`)
	require.EqualError(t, err, "find pattern did not match the target line")
}
