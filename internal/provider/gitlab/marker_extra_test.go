package gitlab_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/gitlab"
	"github.com/stretchr/testify/require"
)

func TestRecencyOrderedMarker(t *testing.T) {
	t.Parallel()

	p := gitlab.New()
	r, ok := any(p).(provider.RecencyOrderer)
	require.True(t, ok, "gitlab implements provider.RecencyOrderer")
	r.RecencyOrdered()
}

func TestDatedMarker(t *testing.T) {
	t.Parallel()

	p := gitlab.New()
	d, ok := any(p).(provider.Dater)
	require.True(t, ok, "gitlab implements provider.Dater")
	d.Dated()
}
