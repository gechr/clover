package hashicorp_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/hashicorp"
	"github.com/stretchr/testify/require"
)

func TestRecencyOrderedMarker(t *testing.T) {
	t.Parallel()

	p := hashicorp.New()
	r, ok := any(p).(provider.RecencyOrderer)
	require.True(t, ok, "hashicorp implements provider.RecencyOrderer")
	r.RecencyOrdered()
}

func TestDatedMarker(t *testing.T) {
	t.Parallel()

	p := hashicorp.New()
	d, ok := any(p).(provider.Dater)
	require.True(t, ok, "hashicorp implements provider.Dater")
	d.Dated()
}
