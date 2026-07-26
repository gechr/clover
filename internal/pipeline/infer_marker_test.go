package pipeline_test

import (
	"testing"

	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/scan"
	"github.com/gechr/forge/vcs"
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

	markers := pipeline.InferredMarkers(file, governed, vcs.NewResolver())
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

	require.Empty(t, pipeline.InferredMarkers(file, nil, vcs.NewResolver()),
		"an incomplete reference is recognized but not synthesized")
}

// A synthetic marker carries the follow edge its inference read, which a
// hand-filled Marker literal did not: the directive held `from=go` while
// [pipeline.Marker].From stayed empty, so an inferred follower failed with
// `follow: producer "" has not resolved` where the written form resolved. Both
// paths now share one constructor. The assertion is on the resolved outcome
// rather than the directive, since the directive was already correct.
func TestInferredMarkersCarryTheFollowEdge(t *testing.T) {
	// The producer half resolves through the go provider, which this binary does
	// not register; without it the version line fails the recognizer's offline
	// gate and only the follower would be synthesized.
	provider.Register(fakeProvider{name: "go"})

	file := scan.File{
		Path: "Dockerfile",
		Lines: []string{
			"FROM alpine",
			"ARG GO_VERSION=1.24.0",
			"ARG GO_SHA256=0000000000000000000000000000000000000000000000000000000000000000",
			"RUN curl -fsSLO https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz",
		},
	}

	markers := pipeline.InferredMarkers(file, nil, vcs.NewResolver())
	require.Len(t, markers, 2, "both halves of the pair earn a synthetic marker")

	var producer, follower pipeline.Marker
	for _, m := range markers {
		if m.IsFollower() {
			follower = m
		} else {
			producer = m
		}
	}

	require.True(t, follower.IsFollower(), "the sum half follows rather than resolving")
	require.NotEmpty(t, follower.From, "the follower names the producer its pairing found")
	require.Equal(t, "sha256", follower.Value, "and the value kind it projects")
	require.Equal(t, producer.ID, follower.From,
		"the pair's two halves agree on the id, namespaced the same way")
}
