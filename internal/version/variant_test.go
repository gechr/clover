package version_test

import (
	"testing"

	"github.com/gechr/clover/internal/version"
	"github.com/stretchr/testify/require"
)

func TestSplitVariant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tag         string
		wantBase    string
		wantVariant string
	}{
		{"1.27-alpine", "1.27", "alpine"},
		{"1.27.0-slim-bookworm", "1.27.0", "slim-bookworm"},
		{"v1.2-alpine3.19", "v1.2", "alpine3.19"},
		{"2.0.0-rc.1", "2.0.0-rc.1", ""}, // a true prerelease is left intact
		{"1.27", "1.27", ""},             // no suffix
		{"latest", "latest", ""},         // not version-shaped, untouched
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			t.Parallel()

			base, variant := version.SplitVariant(tt.tag)
			require.Equal(t, tt.wantBase, base)
			require.Equal(t, tt.wantVariant, variant)
		})
	}
}

func TestVariantTagOutranksPrerelease(t *testing.T) {
	t.Parallel()

	// A variant tag parses to its core, so it is NOT treated as a prerelease and
	// is not dropped by the default prerelease filter (the docker selection bug).
	base, _ := version.SplitVariant("1.28-alpine")
	v, err := version.Parse(base)
	require.NoError(t, err)
	require.Empty(t, v.Prerelease(), "a variant tag has no prerelease segment")

	// A real prerelease still parses as one.
	pre, _ := version.SplitVariant("1.28-rc.1")
	pv, err := version.Parse(pre)
	require.NoError(t, err)
	require.Equal(t, "rc.1", pv.Prerelease())
}
