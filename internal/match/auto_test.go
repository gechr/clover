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
			name: "quoted action pin",
			path: ".github/workflows/ci.yml",
			line: `      uses: "actions/checkout@` + sha + `" # v4.1.0`,
			want: match.Inference{Provider: "github", Repository: "actions/checkout"},
			ok:   true,
		},
		{
			name: "tag-pinned action",
			path: ".github/workflows/ci.yml",
			line: "      - uses: actions/checkout@v4",
			want: match.Inference{Provider: "github", Repository: "actions/checkout"},
			ok:   true,
		},
		{
			name: "full-semver tag-pinned action",
			path: ".github/workflows/ci.yml",
			line: "      uses: hashicorp/setup-terraform@3.1.2",
			want: match.Inference{Provider: "github", Repository: "hashicorp/setup-terraform"},
			ok:   true,
		},
		{
			name: "tag-pinned reusable workflow",
			path: ".github/workflows/ci.yaml",
			line: "    uses: octo-org/example/.github/workflows/reusable.yml@v1.2.3",
			want: match.Inference{Provider: "github", Repository: "octo-org/example"},
			ok:   true,
		},
		{
			name: "branch-pinned action is not matched",
			path: ".github/workflows/ci.yml",
			line: "      uses: actions/checkout@main",
			ok:   false,
		},
		{
			name: "local action is not matched",
			path: ".github/workflows/ci.yml",
			line: "      uses: ./.github/actions/setup",
			ok:   false,
		},
		{
			name: "container job image",
			path: ".github/workflows/ci.yml",
			line: "      - uses: docker://alpine:3.20",
			want: match.Inference{Provider: "docker", Repository: "alpine"},
			ok:   true,
		},
		{
			name: "registry-qualified container job image with digest",
			path: ".github/workflows/ci.yml",
			line: "      uses: docker://ghcr.io/owner/tool:1.2.3@sha256:abc",
			want: match.Inference{
				Provider:   "docker",
				Registry:   "ghcr.io",
				Repository: "owner/tool",
			},
			ok: true,
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
			name: "quoted image with an inline comment",
			path: "docker-compose.yml",
			line: `    image: "nginx:1.27" # pinned`,
			want: match.Inference{Provider: "docker", Repository: "nginx"},
			ok:   true,
		},
		{
			name: "unquoted image with an inline comment",
			path: "docker-compose.yml",
			line: "    image: redis:7.2 # cache",
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
			name: "gitlab component include",
			path: ".gitlab-ci.yml",
			line: "  - component: gitlab.com/components/opentofu/full-pipeline@2.0.1",
			want: match.Inference{Provider: "gitlab", Repository: "components/opentofu"},
			ok:   true,
		},
		{
			name: "gitlab component with a nested project path",
			path: ".gitlab-ci.yml",
			line: "  - component: gitlab.com/group/subgroup/project/lint@1.0.0",
			want: match.Inference{Provider: "gitlab", Repository: "group/subgroup/project"},
			ok:   true,
		},
		{
			name: "gitlab component on a self-managed host",
			path: "ci/pipeline.yaml",
			line: "  - component: gitlab.example.com/org/proj/deploy@3.1.4",
			want: match.Inference{
				Provider:   "gitlab",
				Host:       "gitlab.example.com",
				Repository: "org/proj",
			},
			ok: true,
		},
		{
			name: "gitlab component behind a CI variable has no repository",
			path: ".gitlab-ci.yml",
			line: "  - component: $CI_SERVER_FQDN/org/proj/deploy@3.1.4",
			want: match.Inference{Provider: "gitlab"},
			ok:   true,
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
