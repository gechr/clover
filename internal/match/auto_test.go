package match_test

import (
	"strings"
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
			name: "digest-pinned floating tag infers track",
			path: "Dockerfile",
			line: "FROM gcr.io/distroless/static:nonroot@sha256:" + strings.Repeat("0", 64),
			want: match.Inference{
				Provider:   "docker",
				Registry:   "gcr.io",
				Repository: "distroless/static",
				Track:      "nonroot",
			},
			ok: true,
		},
		{
			name: "digest-pinned floating tag in an image mapping",
			path: "k8s/deploy.yaml",
			line: "        image: nginx:latest@sha256:" + strings.Repeat("0", 64),
			want: match.Inference{Provider: "docker", Repository: "nginx", Track: "latest"},
			ok:   true,
		},
		{
			name: "tag-only floating tag infers no track",
			path: "Dockerfile",
			line: "FROM ubuntu:latest",
			want: match.Inference{Provider: "docker", Repository: "ubuntu"},
			ok:   true,
		},
		{
			name: "digest-pinned tag with digits infers no track",
			path: "Dockerfile",
			line: "FROM mcr.microsoft.com/windows:ltsc2022@sha256:" + strings.Repeat("0", 64),
			want: match.Inference{
				Provider:   "docker",
				Registry:   "mcr.microsoft.com",
				Repository: "windows",
			},
			ok: true,
		},
		{
			name: "digest-pinned tag with a trailing hyphen infers no track",
			path: "Dockerfile",
			line: "FROM example/img:beta-@sha256:" + strings.Repeat("0", 64),
			want: match.Inference{Provider: "docker", Repository: "example/img"},
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
			name: "terraform required_version constraint",
			path: "infra/versions.tf",
			line: `  required_version = "~> 1.11.0"`,
			want: match.Inference{Provider: "hashicorp", Product: "terraform"},
			ok:   true,
		},
		{
			name: "required_version in a tofu file tracks OpenTofu releases",
			path: "infra/versions.tofu",
			line: `  required_version = "~> 1.8.0"`,
			want: match.Inference{Provider: "github", Repository: "opentofu/opentofu"},
			ok:   true,
		},
		{
			name: "required_version outside a terraform file is not matched",
			path: "notes.txt",
			line: `required_version = "~> 1.11.0"`,
			ok:   false,
		},
		{
			name: "mise hashicorp product",
			path: ".mise.toml",
			line: `terraform = "1.9.8"`,
			want: match.Inference{Provider: "hashicorp", Product: "terraform"},
			ok:   true,
		},
		{
			name: "mise hashicorp product in the undotted file",
			path: "sub/mise.toml",
			line: `vault = "1.18.0"`,
			want: match.Inference{Provider: "hashicorp", Product: "vault"},
			ok:   true,
		},
		{
			name: "mise node runtime",
			path: ".mise.toml",
			line: `node = "24.11.0"`,
			want: match.Inference{Provider: "node"},
			ok:   true,
		},
		{
			name: "mise github backend",
			path: "mise.toml",
			line: `"github:cli/cli" = "v2.62.0"`,
			want: match.Inference{Provider: "github", Repository: "cli/cli"},
			ok:   true,
		},
		{
			name: "mise ubi backend with an option qualifier",
			path: ".mise.toml",
			line: `"ubi:BurntSushi/ripgrep[exe=rg]" = "14.1.0"`,
			want: match.Inference{Provider: "github", Repository: "BurntSushi/ripgrep"},
			ok:   true,
		},
		{
			name: "mise well-known github tool",
			path: ".mise.toml",
			line: `tofu = "1.8.5"`,
			want: match.Inference{Provider: "github", Repository: "opentofu/opentofu"},
			ok:   true,
		},
		{
			name: "mise go toolchain",
			path: ".mise.toml",
			line: `go = "1.23.2"`,
			want: match.Inference{
				Provider:   "github",
				Repository: "golang/go",
				TagPrefix:  "go",
			},
			ok: true,
		},
		{
			name: "go directive in go.mod",
			path: "sub/go.mod",
			line: "go 1.23.2",
			want: match.Inference{
				Provider:   "github",
				Repository: "golang/go",
				TagPrefix:  "go",
			},
			ok: true,
		},
		{
			name: "go.mod module directive is not matched",
			path: "go.mod",
			line: "module github.com/owner/repo",
			ok:   false,
		},
		{
			name: "go directive outside go.mod is not matched",
			path: "notes.txt",
			line: "go 1.23.2",
			ok:   false,
		},
		{
			name: "mise registry tool",
			path: ".mise.toml",
			line: `ripgrep = "14.1.0"`,
			want: match.Inference{Provider: "github", Repository: "BurntSushi/ripgrep"},
			ok:   true,
		},
		{
			name: "mise registry tool alias",
			path: "mise.toml",
			line: `rg = "14.1.0"`,
			want: match.Inference{Provider: "github", Repository: "BurntSushi/ripgrep"},
			ok:   true,
		},
		{
			name: "mise core runtime",
			path: ".mise.toml",
			line: `python = "3.13.1"`,
			want: match.Inference{Provider: "github", Repository: "python/cpython"},
			ok:   true,
		},
		{
			name: "mise core runtime with a tag prefix",
			path: ".mise.toml",
			line: `erlang = "27.2"`,
			want: match.Inference{
				Provider:   "github",
				Repository: "erlang/otp",
				TagPrefix:  "OTP-",
			},
			ok: true,
		},
		{
			name: "mise codeberg runtime",
			path: ".mise.toml",
			line: `zig = "0.15.2"`,
			want: match.Inference{Provider: "gitea", Repository: "ziglang/zig"},
			ok:   true,
		},
		{
			name: "mise unknown tool is not matched",
			path: ".mise.toml",
			line: `java = "21.0.5"`,
			ok:   false,
		},
		{
			name: "mise key outside a mise file is not matched",
			path: "Cargo.toml",
			line: `terraform = "1.9.8"`,
			ok:   false,
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

			got, ok := match.Infer(tt.path, []string{tt.line}, 0)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestInferTerraformProviders covers the one context-aware inference: a
// required_providers version line whose source address lives on a sibling line
// of its HCL block.
func TestInferTerraformProviders(t *testing.T) {
	t.Parallel()

	block := []string{
		"terraform {",
		`  required_version = "~> 1.11.0"`,
		"",
		"  required_providers {",
		"    aws = {",
		`      source  = "hashicorp/aws"`,
		`      version = "~> 6.39"`,
		"    }",
		"    random = {",
		`      version = "3.6.0"`,
		`      source  = "hashicorp/random"`,
		"    }",
		`    shorthand = "~> 2.0"`,
		"  }",
		"}",
		"",
		"module \"vpc\" {",
		`  source  = "terraform-aws-modules/vpc/aws"`,
		`  version = "5.8.1"`,
		"}",
	}

	tests := []struct {
		name   string
		target int
		want   match.Inference
		ok     bool
	}{
		{
			name:   "entry with source above the version",
			target: 6,
			want:   match.Inference{Provider: "terraform", Source: "hashicorp/aws"},
			ok:     true,
		},
		{
			name:   "entry with source below the version",
			target: 9,
			want:   match.Inference{Provider: "terraform", Source: "hashicorp/random"},
			ok:     true,
		},
		{
			name:   "shorthand entry line is not a version key",
			target: 12,
			ok:     false,
		},
		{
			name:   "module version pins a module, not a provider",
			target: 18,
			want:   match.Inference{Provider: "terraform"},
			ok:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := match.Infer("infra/versions.tf", block, tt.target)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}

	t.Run("a tofu file resolves against the OpenTofu registry", func(t *testing.T) {
		t.Parallel()

		got, ok := match.Infer("infra/versions.tofu", block, 6)
		require.True(t, ok)
		require.Equal(
			t,
			match.Inference{Provider: "opentofu", Source: "hashicorp/aws"},
			got,
		)
	})
}
