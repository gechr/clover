package match_test

import (
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/stretchr/testify/require"
)

func TestVersionVariables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		line string
		want match.Inference
		ok   bool
	}{
		{
			name: "dockerfile build argument for a native toolchain",
			path: "Dockerfile",
			line: "ARG GO_VERSION=1.24.0",
			want: match.Inference{Provider: "go"},
			ok:   true,
		},
		{
			name: "an ENV in the legacy space-separated spelling",
			path: "Dockerfile",
			line: "ENV MDBOOK_VERSION 0.4.40",
			want: match.Inference{Provider: "github", Repository: "rust-lang/mdBook"},
			ok:   true,
		},
		{
			name: "a registry tool resolves to its repository",
			path: "Dockerfile",
			line: "ENV SOPS_VERSION=3.9.0",
			want: match.Inference{Provider: "github", Repository: "getsops/sops"},
			ok:   true,
		},
		{
			name: "a HashiCorp product names itself",
			path: "Dockerfile",
			line: "ARG TERRAFORM_VERSION=1.9.8",
			want: match.Inference{Provider: "hashicorp", Product: "terraform"},
			ok:   true,
		},
		{
			name: "the same variable as a workflow env value",
			path: ".github/workflows/ci.yml",
			line: "  GO_VERSION: '1.24'",
			want: match.Inference{Provider: "go"},
			ok:   true,
		},
		{
			// The whole prefix must place, never a trailing segment of it: this is a
			// service's API version, not a Node.js pin.
			name: "a trailing segment naming a tool is not a tool",
			path: "Dockerfile",
			line: "ARG API_NODE_VERSION=18.0.0",
			ok:   false,
		},
		{
			// A composite action forwards its inputs under an INPUT_ prefix.
			name: "a forwarded action input is not a python pin",
			path: ".github/workflows/ci.yml",
			line: "  INPUT_PYTHON_VERSION: '3.11'",
			ok:   false,
		},
		{
			name: "a project's own version places nowhere",
			path: "Dockerfile",
			line: "ARG APP_VERSION=2.1.0",
			ok:   false,
		},
		{
			name: "a variable named for a service places nowhere",
			path: "Dockerfile",
			line: "ARG CATALYST_VERSION=0.5.0",
			ok:   false,
		},
		{
			// A judgment pinned as a test rather than left to a generated file's
			// omission: docker is absent from the mise registry map today, so this
			// declines by accident. A DOCKER_VERSION in a Dockerfile more likely
			// pins the Engine or CLI from a distro package than a GitHub release,
			// so if a regeneration ever adds docker to the registry, this failing
			// is the point at which a human decides whether to allow it.
			name: "a docker version is not resolved from the registry",
			path: "Dockerfile",
			line: "ARG DOCKER_VERSION=27.3.1",
			ok:   false,
		},
		{
			name: "a lower-case key is not a variable",
			path: "Dockerfile",
			line: "ARG go_version=1.24.0",
			ok:   false,
		},
		{
			name: "an argument carrying no version is not claimed",
			path: "Dockerfile",
			line: "ARG BASE_IMAGE=alpine",
			ok:   false,
		},
		{
			name: "an unindented YAML key is not a variable",
			path: ".github/workflows/ci.yml",
			line: "GO_VERSION: '1.24'",
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

// A line setting two variables at once is claimed by the inference - the first
// name places - and then refused by the rewriter, which will not guess which of
// two version-shaped tokens the marker meant. The refusal is what makes the
// permissive inference safe, so the division of labour is pinned here.
func TestVersionVariableMultiPair(t *testing.T) {
	t.Parallel()

	const line = "ENV GO_VERSION=1.24.0 NODE_VERSION=22"

	inf, ok := match.Infer("Dockerfile", []string{line}, 0)
	require.True(t, ok)
	require.Equal(t, match.Inference{Provider: "go"}, inf)

	rw := match.For(match.Context{Path: "Dockerfile", Line: line, Provider: inf.Provider})
	_, err := rw.Locate(line)
	require.EqualError(t, err, "multiple version-shaped tokens, so the target is ambiguous")
}

// A build matrix may hold a <TOOL>_VERSION variable just as it holds a setup
// input, and its oldest entry is as deliberate.
func TestVersionVariableMatrix(t *testing.T) {
	t.Parallel()

	workflow := []string{
		"env:",
		"  GO_VERSION: '1.24'",
		"jobs:",
		"  t:",
		"    strategy:",
		"      matrix:",
		"        GO_VERSION: 1.21",
	}

	got, ok := match.Infer(".github/workflows/ci.yml", workflow, 1)
	require.True(t, ok)
	require.Equal(t, match.Inference{Provider: "go"}, got)

	_, ok = match.Infer(".github/workflows/ci.yml", workflow, 6)
	require.False(t, ok, "a matrix entry is not a pin")
}
