package ratelimit_test

import (
	"testing"
	"time"

	"github.com/gechr/clover/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

func TestError_Error(t *testing.T) {
	t.Parallel()

	t.Run("zero reset", func(t *testing.T) {
		t.Parallel()

		require.EqualError(t, &ratelimit.Error{}, "rate limit exceeded")
	})

	t.Run("with reset", func(t *testing.T) {
		t.Parallel()

		reset := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.FixedZone("UTC+2", 2*3600))
		want := "rate limit exceeded, resets at " + reset.UTC().Format(time.RFC3339)
		require.EqualError(t, &ratelimit.Error{Reset: reset}, want)
	})
}
