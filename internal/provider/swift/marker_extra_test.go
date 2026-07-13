package swift_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/swift"
	"github.com/stretchr/testify/require"
)

func TestDatedMarker(t *testing.T) {
	t.Parallel()

	p := swift.New()
	d, ok := any(p).(provider.Dater)
	require.True(t, ok, "swift implements provider.Dater")
	d.Dated()
}
