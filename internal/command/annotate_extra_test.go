package command_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/command"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/docker"
	"github.com/stretchr/testify/require"
)

// writeTree writes files under a fresh temp dir and returns its path.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return root
}

// resolver builds a config resolver with no user or project config.
func resolver() *config.Resolver { return config.NewResolver(nil, "", false) }

// TestRunAnnotate drives the annotate command end to end against a Dockerfile,
// whose FROM line the docker provider recognizes offline (annotate never calls
// Discover). It registers docker so the verify gate can validate the inferred
// provider=auto marker.
func TestRunAnnotate(t *testing.T) {
	provider.Register(docker.New())

	t.Run("preview writes nothing", func(t *testing.T) {
		root := writeTree(t, map[string]string{"Dockerfile": "FROM nginx:1.27\n"})
		require.NoError(t, command.RunAnnotate(
			[]string{root}, nil, nil, nil, false, false, resolver(), 4,
		))
		require.Equal(t, "FROM nginx:1.27\n", readFileAt(t, root, "Dockerfile"),
			"the default preview leaves the file byte-identical")
	})

	t.Run("write inserts the directive", func(t *testing.T) {
		root := writeTree(t, map[string]string{"Dockerfile": "FROM nginx:1.27\n"})
		require.NoError(t, command.RunAnnotate(
			[]string{root}, nil, new(true), nil, false, false, resolver(), 4,
		))
		require.Equal(t, "# @clover\nFROM nginx:1.27\n",
			readFileAt(t, root, "Dockerfile"), "--write applies the annotation")
	})

	t.Run("check with candidates errors", func(t *testing.T) {
		root := writeTree(t, map[string]string{"Dockerfile": "FROM nginx:1.27\n"})
		err := command.RunAnnotate(
			[]string{root}, nil, nil, new(true), false, false, resolver(), 4,
		)
		require.EqualError(t, err, "1 annotation candidate found")
		require.Equal(t, "FROM nginx:1.27\n", readFileAt(t, root, "Dockerfile"),
			"--check never writes")
	})

	t.Run("no candidates returns nil", func(t *testing.T) {
		root := writeTree(t, map[string]string{"notes.txt": "just some prose\n"})
		require.NoError(t, command.RunAnnotate(
			[]string{root}, nil, new(true), nil, false, false, resolver(), 4,
		), "a tree with nothing to annotate is not an error")
	})
}

// TestAnnotateDiscovered drives the discovery-summary log for both arms.
func TestAnnotateDiscovered(t *testing.T) {
	t.Parallel()

	tests := map[string]mode.AnnotateSummary{
		"no candidates warns": {Scanned: 3},
		"candidates counted": {
			Scanned: 3,
			Files: []mode.AnnotateFile{{
				Path:    "Dockerfile",
				Changes: []mode.AnnotateChange{{At: 0, Line: "# @clover"}},
			}},
		},
		"sidecar entries counted": {
			Scanned: 1,
			Files: []mode.AnnotateFile{{
				Path: "versions.json",
				Sidecar: &mode.AnnotateSidecar{
					Path:    "versions.clover.yaml",
					Entries: []mode.SidecarEntryChange{{Target: 0}},
				},
			}},
		},
	}
	for name, summary := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.NotPanics(t, func() { command.AnnotateDiscovered(summary) })
		})
	}
}

// readFileAt reads a file relative to root.
func readFileAt(t *testing.T, root, rel string) string {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, rel))
	require.NoError(t, err)
	return string(got)
}
