package match_test

import (
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/stretchr/testify/require"
)

func TestInfer(t *testing.T) {
	t.Parallel()

	const sha = "a0dfaeb072753c3d48cd4df5fdacfd035b2281bf"

	tests := []struct {
		name string
		path string
		line string
		want match.Inference
		ok   bool
	}{
		{
			name: "reusable workflow pin",
			path: ".github/workflows/ci.yaml",
			line: "    uses: gechr/actions/.github/workflows/lint.yaml@" + sha + " # v0.2.0",
			want: match.Inference{Provider: "github", Repository: "gechr/actions"},
			ok:   true,
		},
		{
			name: "plain action pin",
			path: "repo/.github/workflows/ci.yml",
			line: "      uses: actions/checkout@" + sha + " # v4",
			want: match.Inference{Provider: "github", Repository: "actions/checkout"},
			ok:   true,
		},
		{
			name: "dockerfile FROM a hub image",
			path: "Dockerfile",
			line: "FROM nginx:1.27",
			want: match.Inference{Provider: "docker", Repository: "nginx"},
			ok:   true,
		},
		{
			name: "dockerfile FROM with platform flag and stage name",
			path: "build/Dockerfile.dev",
			line: "FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS build",
			want: match.Inference{Provider: "docker", Repository: "golang"},
			ok:   true,
		},
		{
			name: "dockerfile FROM a registry-qualified image with digest",
			path: "Containerfile",
			line: "FROM ghcr.io/owner/img:1.2.0@sha256:abc",
			want: match.Inference{Provider: "docker", Registry: "ghcr.io", Repository: "owner/img"},
			ok:   true,
		},
		{
			name: "compose image mapping",
			path: "docker-compose.yml",
			line: "    image: redis:7.2",
			want: match.Inference{Provider: "docker", Repository: "redis"},
			ok:   true,
		},
		{
			name: "kubernetes image with a ported registry",
			path: "k8s/deploy.yaml",
			line: "        image: localhost:5000/team/api:2.0.1",
			want: match.Inference{
				Provider:   "docker",
				Registry:   "localhost:5000",
				Repository: "team/api",
			},
			ok: true,
		},
		{
			name: "workflow file but not a uses line",
			path: ".github/workflows/ci.yaml",
			line: "    with:",
			ok:   false,
		},
		{
			name: "FROM outside a dockerfile",
			path: "notes.txt",
			line: "FROM nginx:1.27",
			ok:   false,
		},
		{
			name: "image key without the trailing space is not matched",
			path: "values.yaml",
			line: "    customimage: nginx:1.27",
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := match.Infer(tt.path, tt.line)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}
