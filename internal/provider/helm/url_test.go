package helm_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider/helm"
	"github.com/stretchr/testify/require"
)

func TestReferenceURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		chart    string
		want     string
	}{
		{
			name:     "oci registry links to its host path over https",
			registry: "oci://ghcr.io/charts",
			chart:    "app",
			want:     "https://ghcr.io/charts/app",
		},
		{
			name:     "classic https repository links to the chart under its base",
			registry: "https://charts.example.com/stable",
			chart:    "app",
			want:     "https://charts.example.com/stable/app",
		},
		{
			name:     "classic http repository keeps its scheme",
			registry: "http://charts.example.com",
			chart:    "app",
			want:     "http://charts.example.com/app",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := helm.ReferenceURL(tt.registry, tt.chart)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
