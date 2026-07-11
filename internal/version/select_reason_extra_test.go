package version_test

import (
	"testing"

	"github.com/gechr/clover/internal/version"
	"github.com/stretchr/testify/require"
)

func TestReason_String(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		reason version.Reason
		want   string
	}{
		"eligible":   {reason: version.ReasonEligible, want: "eligible"},
		"unparsable": {reason: version.ReasonUnparsable, want: "unparsable"},
		"filtered":   {reason: version.ReasonFiltered, want: "filtered"},
		"scheme":     {reason: version.ReasonScheme, want: "scheme"},
		"no-asset":   {reason: version.ReasonNoAsset, want: "no-asset"},
		"prerelease": {reason: version.ReasonPrerelease, want: "prerelease"},
		"cooldown":   {reason: version.ReasonCooldown, want: "cooldown"},
		"constraint": {reason: version.ReasonConstraint, want: "constraint"},
		"downgrade":  {reason: version.ReasonDowngrade, want: "downgrade"},
		"unknown":    {reason: version.Reason(999), want: "unknown"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, tt.reason.String())
		})
	}
}
