package pipeline_test

import (
	"testing"

	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/scan"
	"github.com/gechr/x/set"
	"github.com/stretchr/testify/require"
)

// TestInferredMarkersRecognizesUngovernedLines confirms InferredMarkers
// synthesizes a marker only for a recognized, resolvable, ungoverned,
// non-ignored, non-comment line.
func TestInferredMarkersRecognizesUngovernedLines(t *testing.T) {
	provider.Register(fakeProvider{name: "docker"})

	file := scan.File{
		Path: "Dockerfile",
		Lines: []string{
			"FROM alpine:3.20",  // 0: recognized + resolvable, ungoverned -> marker
			"FROM debian:12",    // 1: recognized, but governed -> skipped
			"FROM ubuntu:22.04", // 2: recognized, but ignored -> skipped
			"# a comment line",  // 3: comment -> skipped
			"echo not a pin",    // 4: unrecognized -> skipped
		},
		Ignored: set.New[int](2),
	}
	governed := map[int]bool{1: true}

	markers := pipeline.InferredMarkers(file, governed)
	require.Len(t, markers, 1, "only the ungoverned recognized line yields a marker")

	m := markers[0]
	require.True(t, m.Inferred)
	require.Equal(t, 0, m.Line)
	require.Equal(t, 0, m.Target)
	require.Equal(t, "docker", m.Provider)
	require.Equal(t, "Dockerfile", m.File)
}

// TestInferredMarkersSkipsUnresolvable confirms a recognized line that cannot
// resolve offline (an incomplete reference) is skipped silently.
func TestInferredMarkersSkipsUnresolvable(t *testing.T) {
	file := scan.File{
		Path:  ".gitlab-ci.yml",
		Lines: []string{"  - component: $CI_SERVER_FQDN/org/proj/deploy@3.1.4"},
	}

	require.Empty(t, pipeline.InferredMarkers(file, nil),
		"an incomplete reference is recognized but not synthesized")
}
