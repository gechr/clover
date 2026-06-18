package pipeline_test

import (
	"os"
	"path/filepath"
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

	// repoOf returns the repository the marker carries after binding, or "".
	repoOf := func(m pipeline.Marker) string {
		v, _ := m.Directive.Get("repository")
		return v
	}

	tests := []struct {
		name       string
		path       string
		lines      []string
		directive  directive.Directive
		provider   string
		repository string
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
				Found: []scan.Located{{Line: 0, Directive: tt.directive}},
			}
			markers := pipeline.Markers(file, vcs.NewResolver())
			require.Len(t, markers, 1)
			require.Equal(t, tt.provider, markers[0].Provider)
			require.Equal(t, tt.repository, repoOf(markers[0]))
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
