package oci_test

import (
	"testing"

	"github.com/gechr/clover/internal/oci"
	"github.com/gechr/clover/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

func TestWithRateHeaders(t *testing.T) {
	t.Parallel()

	tests := map[string]ratelimit.Headers{
		"zero": {},
		"populated": {
			Remaining:  "X-RateLimit-Remaining",
			Reset:      "X-RateLimit-Reset",
			ResetKind:  ratelimit.ResetEpoch,
			RetryAfter: "Retry-After",
		},
	}

	for name, headers := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.NotNil(t, oci.New(oci.WithRateHeaders(headers)))
		})
	}
}
