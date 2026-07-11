package rust_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/rust"
	"github.com/stretchr/testify/require"
)

func TestDatedMarker(t *testing.T) {
	t.Parallel()

	p := rust.New()
	d, ok := any(p).(provider.Dater)
	require.True(t, ok, "rust implements provider.Dater")
	d.Dated()
}
