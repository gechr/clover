package pipeline_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/scan"
	"github.com/gechr/clover/internal/vcs"
	"github.com/stretchr/testify/require"
)

func directiveOf(pairs ...directive.KV) directive.Directive {
	return directive.Directive{Pairs: pairs}
}

func TestMarkers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	path := filepath.Join(root, "Dockerfile")

	file := scan.File{
		Path: path,
		Found: []scan.Located{
			{Line: 0, Directive: directiveOf(
				directive.KV{Key: "provider", Value: "github"},
				directive.KV{Key: "repository", Value: "owner/name"},
				directive.KV{Key: "id", Value: "nginx"},
				directive.KV{Key: "tags", Value: "prod,ci"},
			)},
			{Line: 5, Directive: directiveOf(
				directive.KV{Key: "from", Value: "nginx"},
				directive.KV{Key: "value", Value: "commit"},
				directive.KV{Key: "select", Value: "old"},
			)},
		},
	}

	markers := pipeline.Markers(file, vcs.NewResolver())
	require.Len(t, markers, 2)

	producer := markers[0]
	require.Equal(t, "github", producer.Provider)
	require.False(t, producer.IsFollower())
	require.Equal(t, 1, producer.Target, "targets the next line")
	require.Equal(t, root+"\x00nginx", producer.ID)
	require.Empty(t, producer.From)
	require.Equal(t, []string{"prod", "ci"}, producer.Tags, "tags split on commas")

	follower := markers[1]
	require.Equal(t, "follow", follower.Provider, "omitted provider defaults to follow")
	require.True(t, follower.IsFollower())
	require.Equal(t, 6, follower.Target)
	require.Equal(
		t,
		root+"\x00nginx",
		follower.From,
		"from namespaces to the same repo as the producer",
	)
	require.Equal(t, "commit", follower.Value)
	require.Equal(t, "old", follower.Select)
}

// TestMarkersAuto covers provider=auto: a GitHub Actions pin resolves to the
// github provider with the repository inferred from the uses: line, while a
// marker whose target the inference does not recognise stays auto so resolution
// rejects it.
func TestMarkersAuto(t *testing.T) {
	t.Parallel()

	const sha = "a0dfaeb072753c3d48cd4df5fdacfd035b2281bf"
	auto := directiveOf(directive.KV{Key: "provider", Value: "auto"})

	// keyOf returns a directive key the marker carries after binding, or "".
	keyOf := func(m pipeline.Marker, key string) string {
		v, _ := m.Directive.Get(key)
		return v
	}

	tests := []struct {
		name       string
		path       string
		lines      []string
		directive  directive.Directive
		line       int // 0-based line the directive comment sits on
		provider   string
		registry   string
		repository string
		host       string
		pkg        string
		product    string
		source     string
		tagPrefix  string
		track      string
	}{
		{
			name: "infers github and repository from a workflow pin",
			path: ".github/workflows/ci.yaml",
			lines: []string{
				"    # clover: provider=auto",
				"    uses: gechr/actions/.github/workflows/lint.yaml@" + sha + " # v0.2.0",
			},
			directive:  auto,
			provider:   "github",
			repository: "gechr/actions",
		},
		{
			name: "keeps an explicit repository over the inferred one",
			path: ".github/workflows/ci.yaml",
			lines: []string{
				"    # clover: provider=auto repository=owner/override",
				"    uses: gechr/actions/.github/workflows/lint.yaml@" + sha + " # v0.2.0",
			},
			directive: directiveOf(
				directive.KV{Key: "provider", Value: "auto"},
				directive.KV{Key: "repository", Value: "owner/override"},
			),
			provider:   "github",
			repository: "owner/override",
		},
		{
			name: "infers docker and repository from a dockerfile FROM",
			path: "Dockerfile",
			lines: []string{
				"# clover: provider=auto",
				"FROM nginx:1.27",
			},
			directive:  auto,
			provider:   "docker",
			repository: "nginx",
		},
		{
			name: "infers docker registry and repository from a compose image",
			path: "docker-compose.yml",
			lines: []string{
				"    # clover: provider=auto",
				"    image: ghcr.io/owner/api:1.2.0",
			},
			directive:  auto,
			provider:   "docker",
			registry:   "ghcr.io",
			repository: "owner/api",
		},
		{
			name: "infers gitlab host and repository from a component include",
			path: ".gitlab-ci.yml",
			lines: []string{
				"  # clover: provider=auto",
				"  - component: gitlab.example.com/org/proj/deploy@3.1.4",
			},
			directive:  auto,
			provider:   "gitlab",
			repository: "org/proj",
			host:       "gitlab.example.com",
		},
		{
			name: "infers hashicorp product from a mise tool",
			path: ".mise.toml",
			lines: []string{
				"# clover: provider=auto",
				`terraform = "1.9.8"`,
			},
			directive: auto,
			provider:  "hashicorp",
			product:   "terraform",
		},
		{
			name: "infers github repository from a mise backend tool",
			path: "mise.toml",
			lines: []string{
				"# clover: provider=auto",
				`"ubi:owner/tool" = "1.2.3"`,
			},
			directive:  auto,
			provider:   "github",
			repository: "owner/tool",
		},
		{
			name: "infers the go toolchain source from a go.mod go directive",
			path: "go.mod",
			lines: []string{
				"// clover: provider=auto",
				"go 1.23.2",
			},
			directive: auto,
			provider:  "go",
		},
		{
			name: "infers track from a digest-pinned floating tag",
			path: "Dockerfile",
			lines: []string{
				"# clover: provider=auto",
				"FROM gcr.io/distroless/static:nonroot@sha256:" + strings.Repeat("0", 64),
			},
			directive:  auto,
			provider:   "docker",
			registry:   "gcr.io",
			repository: "distroless/static",
			track:      "nonroot",
		},
		{
			name: "infers the registry source from a required_providers block",
			path: "versions.tf",
			lines: []string{
				"terraform {",
				"  required_providers {",
				"    aws = {",
				`      source  = "hashicorp/aws"`,
				"      # clover: provider=auto",
				`      version = "~> 6.39"`,
				"    }",
				"  }",
				"}",
			},
			directive: auto,
			line:      4,
			provider:  "terraform",
			source:    "hashicorp/aws",
		},
		{
			name: "infers the pypi package from a pyproject dependency specifier",
			path: "pyproject.toml",
			lines: []string{
				"# clover: provider=auto",
				`requires = ["uv_build>=0.8.24"]`,
			},
			directive: auto,
			provider:  "pypi",
			pkg:       "uv_build",
		},
		{
			name:       "stays auto when the target is not a recognised pin",
			path:       "README.md",
			lines:      []string{"# clover: provider=auto", "version: 1.2.3"},
			directive:  auto,
			provider:   "auto",
			repository: "",
		},
		{
			name:       "stays auto when the directive is the last line",
			path:       ".github/workflows/ci.yaml",
			lines:      []string{"    # clover: provider=auto"},
			directive:  auto,
			provider:   "auto",
			repository: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			file := scan.File{
				Path:  tt.path,
				Lines: tt.lines,
				Found: []scan.Located{{Line: tt.line, Directive: tt.directive}},
			}
			markers := pipeline.Markers(file, vcs.NewResolver())
			require.Len(t, markers, 1)
			require.Equal(t, tt.provider, markers[0].Provider)
			require.Equal(t, tt.registry, keyOf(markers[0], "registry"))
			require.Equal(t, tt.repository, keyOf(markers[0], "repository"))
			require.Equal(t, tt.host, keyOf(markers[0], "host"))
			require.Equal(t, tt.pkg, keyOf(markers[0], "package"))
			require.Equal(t, tt.product, keyOf(markers[0], "product"))
			require.Equal(t, tt.source, keyOf(markers[0], "source"))
			require.Equal(t, tt.tagPrefix, keyOf(markers[0], "tag-prefix"))
			require.Equal(t, tt.track, keyOf(markers[0], "track"))
		})
	}
}

// TestMarkersToolTagPrefix confirms bind appends the tag-prefix rule the tool
// map records for a tool key, and that an explicit rule always wins.
func TestMarkersToolTagPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		directive directive.Directive
		tagPrefix string
	}{
		{
			name: "tool with a prefixed upstream appends the rule",
			directive: directiveOf(
				directive.KV{Key: "provider", Value: "github"},
				directive.KV{Key: "tool", Value: "erlang"},
			),
			tagPrefix: "OTP-",
		},
		{
			name: "explicit tag-prefix wins over the map",
			directive: directiveOf(
				directive.KV{Key: "provider", Value: "github"},
				directive.KV{Key: "tool", Value: "erlang"},
				directive.KV{Key: "tag-prefix", Value: "v"},
			),
			tagPrefix: "v",
		},
		{
			name: "tool without a prefixed upstream appends nothing",
			directive: directiveOf(
				directive.KV{Key: "provider", Value: "github"},
				directive.KV{Key: "tool", Value: "ripgrep"},
			),
			tagPrefix: "",
		},
		{
			name: "repository marker appends nothing",
			directive: directiveOf(
				directive.KV{Key: "provider", Value: "github"},
				directive.KV{Key: "repository", Value: "owner/name"},
			),
			tagPrefix: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			file := scan.File{
				Path:  "versions.txt",
				Lines: []string{"# marker comment", "version: 27.2"},
				Found: []scan.Located{{Line: 0, Directive: tt.directive}},
			}
			markers := pipeline.Markers(file, vcs.NewResolver())
			require.Len(t, markers, 1)
			prefix, _ := markers[0].Directive.Get("tag-prefix")
			require.Equal(t, tt.tagPrefix, prefix)
		})
	}
}

func TestMarkersNamespaceIsolatesRepos(t *testing.T) {
	t.Parallel()

	resolver := vcs.NewResolver()
	d := directiveOf(
		directive.KV{Key: "provider", Value: "github"},
		directive.KV{Key: "id", Value: "tool"},
	)

	mk := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
		file := scan.File{
			Path:  filepath.Join(root, "Dockerfile"),
			Found: []scan.Located{{Line: 0, Directive: d}},
		}
		return pipeline.Markers(file, resolver)[0].ID
	}

	first, second := mk(t), mk(t)
	require.NotEqual(t, first, second, "same id in different repos yields distinct namespaced ids")
}
