package mode

import (
	"testing"

	"github.com/gechr/clover/internal/sidecar"
	"github.com/stretchr/testify/require"
)

func TestRecognizedLeaf(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		leaf sidecar.Leaf
		want bool
	}{
		"array image leaf": {
			leaf: sidecar.Leaf{Value: "nginx:1.27"},
			want: true,
		},
		"plain value": {
			leaf: sidecar.Leaf{Value: "20"},
			want: false,
		},
		"object image leaf": {
			leaf: sidecar.Leaf{Key: "image", Value: "nginx:1.27"},
			want: true,
		},
		"docker missing repository": {
			leaf: sidecar.Leaf{Value: ":1.27"},
			want: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, recognizedLeaf(tt.leaf))
		})
	}
}
