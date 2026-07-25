package match_test

import (
	"strings"
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
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
			name: "suffix-style dockerfile FROM a hub image",
			path: "app.Dockerfile",
			line: "FROM nginx:1.27",
			want: match.Inference{Provider: "docker", Repository: "nginx"},
			ok:   true,
		},
		{
			name: "suffix-style containerfile FROM a hub image",
			path: "app.Containerfile",
			line: "FROM nginx:1.27",
			want: match.Inference{Provider: "docker", Repository: "nginx"},
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
			name: "mise pipx tool infers pypi",
			path: ".mise.toml",
			line: `ansible = "9.5.1"`,
			want: match.Inference{Provider: "pypi", Package: "ansible"},
			ok:   true,
		},
		{
			name: "mise pipx tool whose package differs from its name",
			path: ".mise.toml",
			line: `xxh = "0.7.3"`,
			want: match.Inference{Provider: "pypi", Package: "xxh-xxh"},
			ok:   true,
		},
		{
			name: "mise pipx tool in .tool-versions",
			path: "sub/.tool-versions",
			line: "ansible 9.5.1",
			want: match.Inference{Provider: "pypi", Package: "ansible"},
			ok:   true,
		},
		{
			name: "mise npm tool infers npm",
			path: "mise.toml",
			line: `prettier = "3.3.3"`,
			want: match.Inference{Provider: "npm", Package: "prettier"},
			ok:   true,
		},
		{
			name: "mise npm tool with a scoped package",
			path: ".mise.toml",
			line: `ni = "0.23.0"`,
			want: match.Inference{Provider: "npm", Package: "@antfu/ni"},
			ok:   true,
		},
		{
			name: "mise cargo tool infers crates",
			path: ".mise.toml",
			line: `magika = "0.6.2"`,
			want: match.Inference{Provider: "crates", Package: "magika-cli"},
			ok:   true,
		},
		{
			name: "mise mixed-backend tool still infers github",
			path: ".mise.toml",
			line: `ast-grep = "0.38.7"`,
			want: match.Inference{Provider: "github", Repository: "ast-grep/ast-grep"},
			ok:   true,
		},
		{
			name: "mise go toolchain",
			path: ".mise.toml",
			line: `go = "1.23.2"`,
			want: match.Inference{Provider: "go"},
			ok:   true,
		},
		{
			name: "go directive in go.mod",
			path: "sub/go.mod",
			line: "go 1.23.2",
			want: match.Inference{Provider: "go"},
			ok:   true,
		},
		{
			name: "toolchain directive in go.mod",
			path: "sub/go.mod",
			line: "toolchain go1.26.5",
			want: match.Inference{Provider: "go"},
			ok:   true,
		},
		{
			name: "go directive in go.work",
			path: "go.work",
			line: "go 1.23.2",
			want: match.Inference{Provider: "go"},
			ok:   true,
		},
		{
			name: "toolchain directive in go.work",
			path: "go.work",
			line: "toolchain go1.26.5",
			want: match.Inference{Provider: "go"},
			ok:   true,
		},
		{
			name: "mise python runtime",
			path: ".mise.toml",
			line: `python = "3.14.5"`,
			want: match.Inference{Provider: "python"},
			ok:   true,
		},
		{
			name: "target-version in pyproject.toml",
			path: "pyproject.toml",
			line: `target-version = "py314"`,
			want: match.Inference{Provider: "python"},
			ok:   true,
		},
		{
			name: "requires-python floor in pyproject.toml",
			path: "pyproject.toml",
			line: `requires-python = ">=3.14"`,
			want: match.Inference{Provider: "python"},
			ok:   true,
		},
		{
			name: "requires-python outside pyproject.toml is not matched",
			path: "notes.txt",
			line: `requires-python = ">=3.14"`,
			ok:   false,
		},
		{
			name: "build-system requires in pyproject.toml",
			path: "pyproject.toml",
			line: `requires = ["uv_build>=0.8.24"]`,
			want: match.Inference{Provider: "pypi", Package: "uv_build"},
			ok:   true,
		},
		{
			name: "dependency specifier with extras and spaces",
			path: "sub/pyproject.toml",
			line: `  "uvicorn[standard] >= 0.30.0",`,
			want: match.Inference{Provider: "pypi", Package: "uvicorn"},
			ok:   true,
		},
		{
			name: "dependency specifier with a dotted name",
			path: "pyproject.toml",
			line: `  "ruamel.yaml>=0.19.1",`,
			want: match.Inference{Provider: "pypi", Package: "ruamel.yaml"},
			ok:   true,
		},
		{
			// The route matches on the comparison shape, but the name ends in a
			// separator PEP 508 forbids, so no package is read and Missing
			// reports the incomplete reference.
			name: "dependency specifier with an invalid name infers no package",
			path: "pyproject.toml",
			line: `  "uv_>=0.8.24",`,
			want: match.Inference{Provider: "pypi"},
			ok:   true,
		},
		{
			name: "dependency specifier outside pyproject.toml is not matched",
			path: "notes.txt",
			line: `  "uv_build>=0.8.24",`,
			ok:   false,
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
			line: `deno = "2.1.4"`,
			want: match.Inference{Provider: "github", Repository: "denoland/deno"},
			ok:   true,
		},
		{
			name: "mise rust runtime",
			path: ".mise.toml",
			line: `rust = "1.97.0"`,
			want: match.Inference{Provider: "rust"},
			ok:   true,
		},
		{
			name: "bare rust-toolchain pin",
			path: "rust-toolchain",
			line: "1.97.0",
			want: match.Inference{Provider: "rust"},
			ok:   true,
		},
		{
			name: "bare rust-toolchain named channel is not matched",
			path: "rust-toolchain",
			line: "nightly-2026-07-11",
			ok:   false,
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
			name: "mise zig runtime",
			path: ".mise.toml",
			line: `zig = "0.15.2"`,
			want: match.Inference{Provider: "zig"},
			ok:   true,
		},
		{
			name: "mise swift toolchain",
			path: ".mise.toml",
			line: `swift = "6.3.3"`,
			want: match.Inference{Provider: "swift"},
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
			name: "mise nodejs alias",
			path: ".mise.toml",
			line: `nodejs = "24.11.0"`,
			want: match.Inference{Provider: "node"},
			ok:   true,
		},
		{
			name: "mise golang alias",
			path: "mise.toml",
			line: `golang = "1.23.2"`,
			want: match.Inference{Provider: "go"},
			ok:   true,
		},
		{
			name: "tool-versions hashicorp product",
			path: ".tool-versions",
			line: "terraform 1.9.8",
			want: match.Inference{Provider: "hashicorp", Product: "terraform"},
			ok:   true,
		},
		{
			name: "tool-versions node runtime under the asdf name",
			path: "sub/.tool-versions",
			line: "nodejs 24.11.0",
			want: match.Inference{Provider: "node"},
			ok:   true,
		},
		{
			name: "tool-versions go toolchain under the asdf name",
			path: ".tool-versions",
			line: "golang 1.23.2",
			want: match.Inference{Provider: "go"},
			ok:   true,
		},
		{
			name: "tool-versions python runtime",
			path: ".tool-versions",
			line: "python 3.14.5",
			want: match.Inference{Provider: "python"},
			ok:   true,
		},
		{
			name: "python-version pin",
			path: ".python-version",
			line: "3.14.6",
			want: match.Inference{Provider: "python"},
			ok:   true,
		},
		{
			name: "python-version pin in a subdirectory",
			path: "sub/.python-version",
			line: "3.15.0b3",
			want: match.Inference{Provider: "python"},
			ok:   true,
		},
		{
			name: "python-version implementation pin is not matched",
			path: ".python-version",
			line: "pypy3.10-7.3.12",
			ok:   false,
		},
		{
			name: "bare version outside .python-version is not matched",
			path: "notes.txt",
			line: "3.14.6",
			ok:   false,
		},
		{
			name: "tool-versions zig runtime",
			path: ".tool-versions",
			line: "zig 0.15.2",
			want: match.Inference{Provider: "zig"},
			ok:   true,
		},
		{
			name: "tool-versions swift toolchain",
			path: ".tool-versions",
			line: "swift 6.3.3",
			want: match.Inference{Provider: "swift"},
			ok:   true,
		},
		{
			name: "swift-version pin",
			path: ".swift-version",
			line: "6.3.3",
			want: match.Inference{Provider: "swift"},
			ok:   true,
		},
		{
			name: "swift-version pin in a subdirectory",
			path: "sub/.swift-version",
			line: "5.10",
			want: match.Inference{Provider: "swift"},
			ok:   true,
		},
		{
			name: "swift-version snapshot pin is not matched",
			path: ".swift-version",
			line: "6.1-snapshot-2026-06-29",
			ok:   false,
		},
		{
			name: "node-version pin",
			path: ".node-version",
			line: "24.18.0",
			want: match.Inference{Provider: "node"},
			ok:   true,
		},
		{
			name: "node-version v-prefixed pin",
			path: ".node-version",
			line: "v24.18.0",
			want: match.Inference{Provider: "node"},
			ok:   true,
		},
		{
			name: "nvmrc pin",
			path: "sub/.nvmrc",
			line: "24",
			want: match.Inference{Provider: "node"},
			ok:   true,
		},
		{
			name: "node-version alias is not matched",
			path: ".node-version",
			line: "lts/jod",
			ok:   false,
		},
		{
			name: "tool-versions well-known github tool",
			path: ".tool-versions",
			line: "tofu 1.8.5",
			want: match.Inference{Provider: "github", Repository: "opentofu/opentofu"},
			ok:   true,
		},
		{
			name: "tool-versions registry tool",
			path: ".tool-versions",
			line: "ripgrep 14.1.0",
			want: match.Inference{Provider: "github", Repository: "BurntSushi/ripgrep"},
			ok:   true,
		},
		{
			name: "tool-versions core runtime with a tag prefix",
			path: ".tool-versions",
			line: "erlang 27.2",
			want: match.Inference{
				Provider:   "github",
				Repository: "erlang/otp",
				TagPrefix:  "OTP-",
			},
			ok: true,
		},
		{
			name: "tool-versions ubi backend, unquoted",
			path: ".tool-versions",
			line: "ubi:BurntSushi/ripgrep 14.1.0",
			want: match.Inference{Provider: "github", Repository: "BurntSushi/ripgrep"},
			ok:   true,
		},
		{
			name: "tool-versions github backend, unquoted",
			path: ".tool-versions",
			line: "github:cli/cli v2.62.0",
			want: match.Inference{Provider: "github", Repository: "cli/cli"},
			ok:   true,
		},
		{
			name: "tool-versions unknown tool is not matched",
			path: ".tool-versions",
			line: "java 21.0.5",
			ok:   false,
		},
		{
			name: "tool name outside a tool-versions file is not matched",
			path: "notes.txt",
			line: "ripgrep 14.1.0",
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
// TestLookupTool pins the tool-name resolution the github provider's tool key
// and auto-detection share: curated names carry their tag prefix, generated
// registry names resolve bare.
func TestLookupTool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tool       string
		repository string
		tagPrefix  string
		ok         bool
	}{
		{
			name:       "curated name with a tag prefix",
			tool:       "erlang",
			repository: "erlang/otp",
			tagPrefix:  "OTP-",
			ok:         true,
		},
		{
			name:       "curated core runtime",
			tool:       "deno",
			repository: "denoland/deno",
			ok:         true,
		},
		{
			name:       "graduated tool keeps its github mapping",
			tool:       "rust",
			repository: "rust-lang/rust",
			ok:         true,
		},
		{
			name:       "generated registry name",
			tool:       "ripgrep",
			repository: "BurntSushi/ripgrep",
			ok:         true,
		},
		{name: "unknown name", tool: "java"},
		{name: "empty name", tool: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repository, tagPrefix, ok := match.LookupTool(tt.tool)
			require.Equal(t, tt.repository, repository)
			require.Equal(t, tt.tagPrefix, tagPrefix)
			require.Equal(t, tt.ok, ok)
		})
	}
}

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
		"",
		"module \"local\" {",
		`  source  = "../modules/thing"`,
		`  version = "1.0.0"`,
		"}",
		"",
		"module \"git\" {",
		`  source  = "git::https://github.com/org/repo.git?ref=v1.2.3"`,
		`  version = "1.2.3"`,
		"}",
		"",
		"module \"qualified\" {",
		`  source  = "app.terraform.io/acme/vpc/aws"`,
		`  version = "2.0.0"`,
		"}",
		"",
		"module \"shorthand\" {",
		`  source  = "github.com/hashicorp/example"`,
		`  version = "3.0.0"`,
		"}",
		"",
		"module \"devregistry\" {",
		`  source  = "localhost:5000/acme/aws"`,
		`  version = "4.0.0"`,
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
			name:   "module version resolves the module registry address",
			target: 18,
			want: match.Inference{
				Provider: "terraform",
				Source:   "terraform-aws-modules/vpc/aws",
			},
			ok: true,
		},
		{
			// A local module has no registry to resolve against; its version
			// attribute is inert to Terraform itself.
			name:   "a local path module names no registry source",
			target: 23,
			want:   match.Inference{Provider: "terraform"},
			ok:     true,
		},
		{
			// A git source carries its own ref, so the registry never served it.
			name:   "a git-sourced module names no registry source",
			target: 28,
			want:   match.Inference{Provider: "terraform"},
			ok:     true,
		},
		{
			// A host-qualified address needs the host key, which inference does not
			// supply, so the source is left for an explicit annotation.
			name:   "a host-qualified module is left explicit",
			target: 33,
			want:   match.Inference{Provider: "terraform"},
			ok:     true,
		},
		{
			// Terraform reads a forge shorthand as a git source. It is three plain
			// segments like a registry address, so only the dotted first segment
			// tells them apart - without that it would be annotated and then
			// rejected by the provider with advice about a marker nobody wrote.
			name:   "a forge shorthand is a git source, not a registry address",
			target: 38,
			want:   match.Inference{Provider: "terraform"},
			ok:     true,
		},
		{
			// A port colon marks a hostname as surely as a dot does.
			name:   "a host:port registry address is left explicit",
			target: 43,
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

// TestInferHelmDependencies covers the Chart.yaml context-aware inference: a
// dependencies entry's version line whose chart name and repository live on
// sibling fields of the entry.
func TestInferHelmDependencies(t *testing.T) {
	t.Parallel()

	chart := []string{
		"apiVersion: v2",
		"name: mychart",
		"version: 1.0.0",
		`appVersion: "2.3.4"`,
		"dependencies:",
		"  - name: postgresql",
		"    repository: https://charts.bitnami.com/bitnami",
		"    version: 12.1.2",
		"  - name: redis",
		"    version: 17.0.0",
		"    repository: oci://registry-1.docker.io/bitnamicharts",
		"  - name: local",
		"    repository: file://../local",
		"    version: 0.1.0",
	}

	tests := []struct {
		name   string
		target int
		want   match.Inference
		ok     bool
	}{
		{
			name:   "classic repository dependency",
			target: 7,
			want: match.Inference{
				Provider: "helm",
				Chart:    "postgresql",
				Registry: "https://charts.bitnami.com/bitnami",
			},
			ok: true,
		},
		{
			name:   "oci repository with the version above the repository",
			target: 9,
			want: match.Inference{
				Provider: "helm",
				Chart:    "redis",
				Registry: "oci://registry-1.docker.io/bitnamicharts",
			},
			ok: true,
		},
		{
			name:   "local file dependency infers a repository the provider rejects",
			target: 13,
			want: match.Inference{
				Provider: "helm",
				Chart:    "local",
				Registry: "file://../local",
			},
			ok: true,
		},
		{
			name:   "the chart's own top-level version is not a dependency",
			target: 2,
			ok:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := match.Infer("charts/mychart/Chart.yaml", chart, tt.target)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestPreCommitRevs(t *testing.T) {
	t.Parallel()

	config := []string{
		"repos:",
		"- repo: https://github.com/pre-commit/pre-commit-hooks",
		"  rev: v3.2.0",
		"  hooks:",
		"  - id: trailing-whitespace",
		"-   repo: https://github.com/astral-sh/ruff-pre-commit",
		"    hooks:",
		"      - id: ruff",
		"    rev: v0.9.10",
		"- repo: https://gitlab.com/group/subgroup/project.git",
		"  rev: 1.2.0",
		"- repo: git@github.com:owner/name.git",
		"  rev: v1.0.0",
		"- repo: https://codeberg.org/owner/tool",
		"  rev: v2.1.0",
		"- repo: local",
		"  rev: v9.9.9",
		"- repo: https://git.example.com/owner/tool",
		"  rev: v1.0.0",
		"- repo: https://github.com/psf/black",
		"  rev: 552baf822992936134cbd31a38f69c8cfe7c0f05  # frozen: v22.3.0",
		"- repo: https://GitHub.com/Owner/Name",
		"  rev: v1.5.0",
	}

	tests := []struct {
		name   string
		target int
		want   match.Inference
		ok     bool
	}{
		{
			name:   "github hook repository",
			target: 2,
			want:   match.Inference{Provider: "github", Repository: "pre-commit/pre-commit-hooks"},
			ok:     true,
		},
		{
			name:   "deeper list indentation with the rev below the hooks",
			target: 8,
			want:   match.Inference{Provider: "github", Repository: "astral-sh/ruff-pre-commit"},
			ok:     true,
		},
		{
			name:   "gitlab subgroup path keeps every segment",
			target: 10,
			want:   match.Inference{Provider: "gitlab", Repository: "group/subgroup/project"},
			ok:     true,
		},
		{
			name:   "scp-like ssh remote",
			target: 12,
			want:   match.Inference{Provider: "github", Repository: "owner/name"},
			ok:     true,
		},
		{
			name:   "codeberg resolves through gitea's default flavor",
			target: 14,
			want:   match.Inference{Provider: "gitea", Repository: "owner/tool"},
			ok:     true,
		},
		{
			name:   "the local pseudo-repository names no forge",
			target: 16,
			ok:     false,
		},
		{
			name:   "an unrecognized forge host is declined rather than guessed",
			target: 18,
			ok:     false,
		},
		{
			// The smart rewriter would bump the comment's version and leave the SHA
			// pointing at the old commit, so the route must not claim the line.
			name:   "a frozen SHA pin is not claimed",
			target: 20,
			ok:     false,
		},
		{
			// A hostname is case-insensitive, so the forge lookup folds case.
			name:   "a mixed-case host reaches the same forge",
			target: 22,
			want:   match.Inference{Provider: "github", Repository: "Owner/Name"},
			ok:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := match.Infer(".pre-commit-config.yaml", config, tt.target)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSetupInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want match.Inference
		ok   bool
	}{
		{
			name: "go toolchain input",
			line: "          go-version: 1.21",
			want: match.Inference{Provider: "go"},
			ok:   true,
		},
		{
			name: "quoted go version",
			line: `          go-version: "1.24.0"`,
			want: match.Inference{Provider: "go"},
			ok:   true,
		},
		{
			name: "bare major node version",
			line: "          node-version: 22",
			want: match.Inference{Provider: "node"},
			ok:   true,
		},
		{
			name: "single-quoted python version",
			line: "          python-version: '3.13'",
			want: match.Inference{Provider: "python"},
			ok:   true,
		},
		{
			name: "swift toolchain input",
			line: "          swift-version: '6.0.1'",
			want: match.Inference{Provider: "swift"},
			ok:   true,
		},
		{
			name: "underscored terraform input names its product",
			line: "          terraform_version: 1.11.x",
			want: match.Inference{Provider: "hashicorp", Product: "terraform"},
			ok:   true,
		},
		{
			// The sibling input names a file to read the version from, not a version.
			name: "go-version-file is a different input",
			line: "          go-version-file: go.mod",
			ok:   false,
		},
		{
			name: "an expression carries no version to track",
			line: "          go-version: ${{ env.GO_VERSION }}",
			ok:   false,
		},
		{
			// A matrix lists the versions a project supports, so its oldest entry is
			// deliberate; bumping a single-entry list would silently drop support.
			name: "a flow-list matrix is not a pin",
			line: "        python-version: ['3.14']",
			ok:   false,
		},
		{
			name: "a block value leaves nothing on the key's line",
			line: "        node-version:",
			ok:   false,
		},
		{
			// The key must be nested; a top-level one is not a step input.
			name: "an unindented key is not a step input",
			line: "go-version: 1.21",
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := match.Infer(".github/workflows/ci.yml", []string{tt.line}, 0)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

// A build matrix lists the versions a project supports, so its oldest entry is
// deliberate and bumping it would silently drop support. The route's line
// pattern refuses the flow-list spelling, but an include-style matrix spells the
// same fact as a plain scalar, indistinguishable from a step input on the line
// alone - so the inference refuses it by context.
func TestSetupInputMatrix(t *testing.T) {
	t.Parallel()

	workflow := []string{
		"jobs:",
		"  test:",
		"    strategy:",
		"      matrix:",
		"        go-version: 1.21",
		"        include:",
		"          - toxenv: py39",
		"            python-version: '3.9'",
		"    steps:",
		"      - uses: actions/setup-python@v5",
		"        with:",
		"          python-version: '3.14'",
		"      - uses: actions/setup-go@v6",
		"        with:",
		"          go-version: 1.24.0",
	}

	tests := []struct {
		name   string
		target int
		want   match.Inference
		ok     bool
	}{
		{
			name:   "a scalar directly under matrix is not a pin",
			target: 4,
			ok:     false,
		},
		{
			name:   "an include entry's version is not a pin",
			target: 7,
			ok:     false,
		},
		{
			name:   "a step input in the same file still resolves",
			target: 11,
			want:   match.Inference{Provider: "python"},
			ok:     true,
		},
		{
			name:   "and so does one below the matrix",
			target: 14,
			want:   match.Inference{Provider: "go"},
			ok:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := match.Infer(".github/workflows/ci.yml", workflow, tt.target)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

// A setup input may carry a wildcard patch (terraform_version: 1.11.x), whose
// version-shaped token is the 1.11 prefix. Bumping it re-precisions the resolved
// version onto that token and leaves the wildcard in place, so the constraint
// keeps its meaning.
func TestSetupInputWildcard(t *testing.T) {
	t.Parallel()

	const line = "          terraform_version: 1.11.x"

	rw := match.For(match.Context{
		Path:     ".github/workflows/ci.yml",
		Line:     line,
		Provider: "hashicorp",
	})
	located, err := rw.Locate(line)
	require.NoError(t, err)
	require.Equal(t, "1.11", located.Current())

	got, changed, err := located.Render(line, model.Candidate{Version: "1.14.2"})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "          terraform_version: 1.14.x", got)
}
