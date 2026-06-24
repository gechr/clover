package docker_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider/docker"
	"github.com/stretchr/testify/require"
)

func TestReferenceURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		registry   string
		repository string
		want       string
	}{
		{
			name:       "hub official image links to the underscore namespace",
			repository: "nginx",
			want:       "https://hub.docker.com/_/nginx",
		},
		{
			name:       "hub namespaced image links to the r namespace",
			repository: "bitnami/nginx",
			want:       "https://hub.docker.com/r/bitnami/nginx",
		},
		{
			name:       "other registry links to its host path",
			registry:   "registry.example.com",
			repository: "team/img",
			want:       "https://registry.example.com/team/img",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := docker.ReferenceURL(tt.registry, tt.repository)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
