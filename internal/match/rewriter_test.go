package match_test

import (
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/stretchr/testify/require"
)

// TestForFallsBackToSmart confirms the dispatch table returns the smart rewriter
// for an ordinary line - the only route until format-specific rewriters land.
func TestForFallsBackToSmart(t *testing.T) {
	t.Parallel()

	rw := match.For(match.Context{
		Path:     "Dockerfile",
		Line:     "FROM nginx:1.27.0",
		Provider: "github",
	})
	require.IsType(t, match.Smart{}, rw)
}

// TestForGoModToolchain confirms a go.mod toolchain directive routes to the
// find rewriter that anchors on the glued go prefix - the smart rewriter cannot
// see a mid-word version - and that it renders both spellings: a stable bump in
// place, and a dashless rc pin staying dashless.
func TestForGoModToolchain(t *testing.T) {
	t.Parallel()

	rw := match.For(match.Context{
		Path:     "go.mod",
		Line:     "toolchain go1.26.4",
		Provider: "go",
	})
	require.IsType(t, match.FindReplace{}, rw)

	located, err := rw.Locate("toolchain go1.26.4")
	require.NoError(t, err)
	require.Equal(t, "1.26.4", located.Current())

	line, changed, err := located.Render("toolchain go1.26.4", model.Candidate{Version: "1.26.5"})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "toolchain go1.26.5", line)

	// A dashless rc pin round-trips: the capture grammar accepts the glued
	// prerelease and restyle keeps the current spelling.
	located, err = rw.Locate("toolchain go1.27rc1")
	require.NoError(t, err)
	require.Equal(t, "1.27rc1", located.Current())

	line, changed, err = located.Render(
		"toolchain go1.27rc1",
		model.Candidate{Version: "1.27.0-rc2"},
	)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "toolchain go1.27rc2", line)
}

// TestMiseFile confirms both file shapes mise reads tool pins from count as
// mise files, so a bare single-number pin gets major precision in either.
func TestMiseFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{path: ".mise.toml", want: true},
		{path: "sub/mise.toml", want: true},
		{path: "mise.local.toml", want: true},
		{path: ".mise.local.toml", want: true},
		{path: "mise.dev.toml", want: true},
		{path: "mise.dev.local.toml", want: true},
		{path: "mise/config.toml", want: true},
		{path: "sub/.mise/config.toml", want: true},
		{path: ".config/mise.toml", want: true},
		{path: ".config/mise/config.toml", want: true},
		{path: ".config/mise/conf.d/extra.toml", want: true},
		{path: ".tool-versions", want: true},
		{path: "sub/.tool-versions", want: true},
		{path: "Cargo.toml", want: false},
		{path: "mise.lock", want: false},
		{path: "premise.toml", want: false},
		{path: "tool-versions", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, match.MiseFile(tt.path))
		})
	}
}

func TestPythonVersionFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{path: ".python-version", want: true},
		{path: "sub/.python-version", want: true},
		{path: "python-version", want: false},
		{path: ".python-version.clover.yaml", want: false},
		{path: "pyproject.toml", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, match.PythonVersionFile(tt.path))
		})
	}
}

func TestSwiftVersionFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{path: ".swift-version", want: true},
		{path: "sub/.swift-version", want: true},
		{path: "swift-version", want: false},
		{path: ".swift-version.clover.yaml", want: false},
		{path: "Package.swift", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, match.SwiftVersionFile(tt.path))
		})
	}
}

func TestNodeVersionFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{path: ".node-version", want: true},
		{path: ".nvmrc", want: true},
		{path: "sub/.node-version", want: true},
		{path: "sub/.nvmrc", want: true},
		{path: "node-version", want: false},
		{path: ".node-version.clover.yaml", want: false},
		{path: "package.json", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, match.NodeVersionFile(tt.path))
		})
	}
}

// TestForContainerJobUses confirms a workflow container job's uses: docker://
// reference routes to the docker rewriters, not the action ones: digest-pinned
// to docker-pin, tag-only to docker-tag.
func TestForContainerJobUses(t *testing.T) {
	t.Parallel()

	digest := "      - uses: docker://alpine:3.20@sha256:0123456789012345678901234567890123456789012345678901234567890123"
	pinned := match.For(
		match.Context{Path: ".github/workflows/ci.yml", Line: digest, Provider: "docker"},
	)
	require.IsType(t, match.DockerPin{}, pinned)

	tagOnly := match.For(match.Context{
		Path:     ".github/workflows/ci.yml",
		Line:     "      - uses: docker://alpine:3.20",
		Provider: "docker",
	})
	require.IsType(t, match.DockerTag{}, tagOnly)
}

// TestForDigestPinnedDocker confirms a digest-pinned docker line routes to the
// docker-pin rewriter, while a tag-only one routes to the docker-tag rewriter.
func TestForDigestPinnedDocker(t *testing.T) {
	t.Parallel()

	digest := "FROM nginx:1.27@sha256:0123456789012345678901234567890123456789012345678901234567890123"
	pinned := match.For(match.Context{Path: "Dockerfile", Line: digest, Provider: "docker"})
	require.IsType(t, match.DockerPin{}, pinned)

	tagOnly := match.For(
		match.Context{Path: "Dockerfile", Line: "FROM nginx:1.27", Provider: "docker"},
	)
	require.IsType(t, match.DockerTag{}, tagOnly)
}
