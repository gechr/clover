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
				directive.KV{Key: "repo", Value: "owner/name"},
				directive.KV{Key: "id", Value: "nginx"},
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
