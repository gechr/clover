package version_test

import (
	"testing"

	"github.com/gechr/clover/internal/version"
	"github.com/stretchr/testify/require"
)

func TestCompare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    string
		b    string
		want int // sign of a vs b
	}{
		{name: "equal", a: "1.2.3", b: "1.2.3", want: 0},
		{name: "padded equal", a: "1.2", b: "1.2.0", want: 0},
		{name: "patch", a: "1.2.3", b: "1.2.4", want: -1},
		{name: "multi-digit minor", a: "1.9.0", b: "1.13.0", want: -1},
		{name: "alpha before beta", a: "1.0.0-alpha", b: "1.0.0-beta", want: -1},
		{name: "beta before rc", a: "1.0.0-beta", b: "1.0.0-rc", want: -1},
		{name: "rc before release", a: "1.0.0-rc", b: "1.0.0", want: -1},
		{name: "natural undotted prerelease", a: "1.0.0-beta9", b: "1.0.0-beta10", want: -1},
		{name: "natural dotted prerelease", a: "1.0.0-rc.9", b: "1.0.0-rc.10", want: -1},
		{name: "cross-family prerelease", a: "1.0.0-alpha2", b: "1.0.0-beta1", want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a, err := version.Parse(tt.a)
			require.NoError(t, err)
			b, err := version.Parse(tt.b)
			require.NoError(t, err)

			require.Equal(t, tt.want, version.Compare(a, b))
			require.Equal(t, -tt.want, version.Compare(b, a), "compare is antisymmetric")
		})
	}
}
