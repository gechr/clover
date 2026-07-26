package pipeline_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// degradeReason is the skip a follower carries when the id its pairing needed is
// held by holder (a file:line).
func degradeReason(holder string) string {
	return `id "go" is claimed by ` + holder +
		` - write id=/from= by hand to follow this file's pin`
}

// A duplicated id the user wrote is a hard error on every written publisher:
// which one a follower binds to is unknowable, and resolving either would guess.
// This is the property the degrade branch is one mis-ordered condition away from
// swallowing, so it is pinned on its own.
func TestValidateWrittenDuplicateIDIsAHardError(t *testing.T) {
	provider.Register(fakeProvider{name: "dupwritten"})

	dir := write(t, map[string]string{
		"a.yaml": "# clover: provider=dupwritten repository=x/y id=shared\nversion: 1.2.3\n",
		"b.yaml": "# clover: provider=dupwritten repository=x/y id=shared\nversion: 1.2.3\n",
	})

	files, err := pipeline.Validate(context.Background(), []string{dir})
	require.NoError(t, err)

	want := `duplicate id "shared" (` + filepath.Join(dir, "a.yaml") + ":1, " +
		filepath.Join(dir, "b.yaml") + ":1): rename one so followers bind unambiguously"
	for _, f := range files {
		for _, r := range f.Results {
			require.EqualError(t, r.Err, want, "each error names every publisher")
		}
	}
}

// An id Clover inferred yields to a written one: the marker keeps resolving -
// the version stays tracked - while the same-file follower whose inferred from
// named it is skipped with a reason, never failed. Verified through --infer,
// the path that manufactures this shape in every file of a clean tree.
func TestValidateInferredIDYieldsToWritten(t *testing.T) {
	provider.Register(fakeProvider{name: "go"})

	dir := write(t, map[string]string{
		// z sorts after b, so path order alone would hand b the id: only the
		// written-outranks-inferred tier keeps z's claim.
		"z.yaml": "# clover: provider=go id=go\nversion: 1.2.3\n",
		"b.Dockerfile": "FROM alpine\n" +
			"ARG GO_VERSION=1.24.0\n" +
			"ARG GO_SHA256=0000000000000000000000000000000000000000000000000000000000000000\n" +
			"RUN curl -fsSLO https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz\n",
	})

	files, err := pipeline.Validate(
		context.Background(),
		[]string{dir},
		pipeline.WithInfer(true),
		pipeline.WithRequireDirective(false),
	)
	require.NoError(t, err)

	var producers, skips int
	for _, f := range files {
		for _, r := range f.Results {
			require.NoError(t, r.Err, "no marker fails over an id Clover manufactured")
			switch {
			case r.Marker.IsFollower():
				require.True(t, r.Skipped, "the degraded pair's follower skips")
				require.Equal(t, degradeReason(filepath.Join(dir, "z.yaml")+":1"), r.Reason)
				skips++
			default:
				require.False(t, r.Skipped, "every producer still resolves")
				if strings.HasSuffix(f.Path, "b.Dockerfile") {
					require.Empty(t, r.Marker.ID,
						"the degraded producer stops publishing - two publishers under "+
							"one id is the last-writer-wins ambiguity this exists to end")
				} else {
					require.NotEmpty(t, r.Marker.ID, "the written publisher keeps its id")
				}
				producers++
			}
		}
	}
	require.Equal(t, 2, producers, "the written publisher and the degraded one")
	require.Equal(t, 1, skips)
}

// Among inferred publishers alone the first in path order survives, so the
// outcome is deterministic and idempotent rather than scheduling-dependent.
func TestValidateInferredCollisionFirstPathWins(t *testing.T) {
	provider.Register(fakeProvider{name: "go"})

	pair := "FROM alpine\n" +
		"ARG GO_VERSION=1.24.0\n" +
		"ARG GO_SHA256=0000000000000000000000000000000000000000000000000000000000000000\n" +
		"RUN curl -fsSLO https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz\n"
	dir := write(t, map[string]string{"a.Dockerfile": pair, "b.Dockerfile": pair})

	files, err := pipeline.Validate(
		context.Background(),
		[]string{dir},
		pipeline.WithInfer(true),
		pipeline.WithRequireDirective(false),
	)
	require.NoError(t, err)

	for _, f := range files {
		for _, r := range f.Results {
			require.NoError(t, r.Err)
			if !r.Marker.IsFollower() {
				continue
			}
			if strings.HasSuffix(f.Path, "a.Dockerfile") {
				require.False(t, r.Skipped, "the first file's pair stays whole")
			} else {
				require.True(t, r.Skipped, "the second file's follower yields")
				require.Equal(t,
					degradeReason(filepath.Join(dir, "a.Dockerfile")+":2"), r.Reason)
			}
		}
	}
}
