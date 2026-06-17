package version_test

import (
	"testing"

	"github.com/gechr/cusp/internal/version"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string // canonical String(); ignored when wantErr
		wantErr bool
	}{
		{name: "three part", in: "1.25.0", want: "1.25.0"},
		{name: "v prefix", in: "v1.25.0", want: "1.25.0"},
		{name: "two part pads", in: "1.25", want: "1.25.0"},
		{name: "one part pads", in: "2", want: "2.0.0"},
		{name: "prerelease", in: "2.0.0-rc.1", want: "2.0.0-rc.1"},
		{name: "build metadata", in: "1.2.3+build5", want: "1.2.3+build5"},
		{name: "empty", in: "", wantErr: true},
		{name: "not a version", in: "latest", wantErr: true},
		{name: "calver-ish still parses", in: "2024.01.15", want: "2024.1.15"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := version.Parse(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got.String())
		})
	}
}

// mustParse fails the test if s is not a valid version. Tests construct their
// inputs through the public Parse so they exercise the real entry point.
func mustParse(t *testing.T, s string) *version.Version {
	t.Helper()
	v, err := version.Parse(s)
	require.NoError(t, err)
	return v
}
