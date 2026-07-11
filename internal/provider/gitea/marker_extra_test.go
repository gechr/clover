package gitea_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/gitea"
	"github.com/stretchr/testify/require"
)

func TestDatedMarker(t *testing.T) {
	t.Parallel()

	p := gitea.New()
	d, ok := any(p).(provider.Dater)
	require.True(t, ok, "gitea implements provider.Dater")
	d.Dated()
}

func TestAuthHintMarker(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		"for higher rate limits and private repositories, "+
			"run `clover login gitea` or set `CLOVER_GITEA_TOKEN`",
		gitea.New().AuthHint())
}
