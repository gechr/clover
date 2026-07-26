package mode_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/mode"
	"github.com/stretchr/testify/require"
)

// claimSkips returns the claim-pass skips among a file's diagnostics, which also
// carry unrelated shape skips (a bare FROM with no tag).
func claimSkips(skips []mode.AnnotateSkip) []string {
	var reasons []string
	for _, skip := range skips {
		if strings.Contains(skip.Reason, `id "go"`) {
			reasons = append(reasons, skip.Reason)
		}
	}
	return reasons
}

// claimReason is the reason a dropped follower carries, naming the file that
// holds the id it needed.
func claimReason(holder string) string {
	return `id "go" is claimed by ` + holder +
		` - write id=/from= by hand to pair with this file's pin`
}

// pairDockerfile is a file whose GO_VERSION/GO_SHA256 variables pair: the
// version line earns `id=go` and the sum line a follower naming it.
const pairDockerfile = "FROM alpine\n" +
	"ARG GO_VERSION=1.24.0\n" +
	"ARG GO_SHA256=0000000000000000000000000000000000000000000000000000000000000000\n" +
	"RUN curl -fsSLO https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz\n"

// followerComment is the annotation the sum line of [pairDockerfile] earns.
const followerComment = "# clover: from=go value=sha256 pattern=go<version>.linux-amd64.tar.gz"

// Two files pairing the same tool both want `id=go`, and the parallel phase
// proposes each blind to the other. The claim pass settles it in path order: the
// first file keeps the pair, the second is annotated bare - its version stays
// tracked - and its follower is dropped with a reason naming the holder. Nothing
// annotate writes may fail the very next lint, so which file wins must not
// depend on scheduling.
func TestAnnotateClaimsPairIDsInPathOrder(t *testing.T) {
	root := annotateTree(t, map[string]string{
		"a/Dockerfile": pairDockerfile,
		"b/Dockerfile": pairDockerfile,
	})

	summary := annotate(t, root, false, false)
	require.Len(t, summary.Files, 2)

	first, second := summary.Files[0], summary.Files[1]
	require.True(t, strings.HasSuffix(first.Path, "a/Dockerfile"))

	require.Len(t, first.Changes, 2, "the first file keeps the whole pair")
	require.Equal(t, "# @clover: id=go", first.Changes[0].Line)
	require.Equal(t, followerComment, first.Changes[1].Line)

	require.Len(t, second.Changes, 1, "the second file keeps only the version pin")
	require.Equal(t, "# @clover", second.Changes[0].Line, "its producer degrades to bare")
	require.Equal(t,
		[]string{claimReason(filepath.Join(root, "a/Dockerfile"))},
		claimSkips(second.Skips),
		"and its follower is dropped with a reason naming the holder")
}

// The claim set is seeded with every id already written in a scanned file, so a
// hand-written `id=go` outranks an inferred claim however early the claimant
// sorts: without the seed, `a/Dockerfile` would claim first by path order and
// annotate would manufacture the exact written-vs-inferred collision the pass
// exists to prevent.
func TestAnnotateSeedsClaimsFromWrittenIDs(t *testing.T) {
	root := annotateTree(t, map[string]string{
		"a/Dockerfile": pairDockerfile,
		"z/Dockerfile": "FROM alpine\n" +
			"# clover: provider=go id=go\n" +
			"ARG GO_VERSION=1.24.0\n",
	})

	summary := annotate(t, root, false, false)
	require.Len(t, summary.Files, 2)

	first := summary.Files[0]
	require.True(t, strings.HasSuffix(first.Path, "a/Dockerfile"))
	require.Len(t, first.Changes, 1, "the written id wins, so only the version pin lands")
	require.Equal(t, "# @clover", first.Changes[0].Line)
	require.Equal(t,
		[]string{claimReason(filepath.Join(root, "z/Dockerfile"))},
		claimSkips(first.Skips))
}

// An id nobody holds is claimed for the follower's own file rather than dropping
// the change: the pairing guarantees the producer is in this same file, just
// already annotated - the shape last week's version-variable feature wrote, so
// this is the common upgrade path. A bare `@clover` producer re-infers the id at
// bind, and the hand-written form of this exact follower lints clean; dropping it
// left the sum line untracked while reporting the file fully annotated.
func TestAnnotateClaimsUnheldIDForGovernedBareProducer(t *testing.T) {
	root := annotateTree(t, map[string]string{
		"Dockerfile": "FROM alpine\n" +
			"# @clover\n" +
			"ARG GO_VERSION=1.24.0\n" +
			"ARG GO_SHA256=0000000000000000000000000000000000000000000000000000000000000000\n" +
			"RUN curl -fsSLO https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz\n",
	})

	summary := annotate(t, root, false, false)
	require.Len(t, summary.Files, 1)
	require.Len(t, summary.Files[0].Changes, 1, "the sum line earns its follower")
	require.Equal(t, followerComment, summary.Files[0].Changes[0].Line)
	require.Empty(t, claimSkips(summary.Files[0].Skips))
}

// Two files both in the governed-bare shape contend for the unheld id: the first
// in path order claims it and keeps its follower, the second drops - at bind the
// zero-written collision hands the id to the same first-in-path-order publisher,
// so the kept follower binds in-file and the outcome matches the runtime tier.
func TestAnnotateUnheldIDClaimIsPathOrdered(t *testing.T) {
	governedBare := "FROM alpine\n" +
		"# @clover\n" +
		"ARG GO_VERSION=1.24.0\n" +
		"ARG GO_SHA256=0000000000000000000000000000000000000000000000000000000000000000\n" +
		"RUN curl -fsSLO https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz\n"
	root := annotateTree(t, map[string]string{
		"a/Dockerfile": governedBare,
		"b/Dockerfile": governedBare,
	})

	summary := annotate(t, root, false, false)
	require.Len(t, summary.Files, 2)

	first, second := summary.Files[0], summary.Files[1]
	require.True(t, strings.HasSuffix(first.Path, "a/Dockerfile"))
	require.Len(t, first.Changes, 1)
	require.Equal(t, followerComment, first.Changes[0].Line)
	require.Empty(t, claimSkips(first.Skips))

	require.Empty(t, second.Changes)
	require.Equal(t,
		[]string{claimReason(filepath.Join(root, "a/Dockerfile"))},
		claimSkips(second.Skips))
}

// A written duplicate is ambiguous wherever its second occurrence sits - same
// file or not - because lint is about to hard-error both publishers. A follower
// proposed against it must not be written, or annotate lands config in a file
// the very next lint rejects.
func TestAnnotateDropsFollowerForSameFileWrittenDuplicate(t *testing.T) {
	root := annotateTree(t, map[string]string{
		"Dockerfile": "FROM alpine\n" +
			"# clover: provider=go id=go\n" +
			"ARG GO_VERSION=1.24.0\n" +
			"# clover: provider=node id=go\n" +
			"ARG NODE_VERSION=22.0.0\n" +
			"ARG GO_SHA256=0000000000000000000000000000000000000000000000000000000000000000\n" +
			"RUN curl -fsSLO https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz\n",
	})

	summary := annotate(t, root, false, false)
	require.Len(t, summary.Files, 1)
	require.Empty(t, summary.Files[0].Changes)
	require.Equal(t,
		[]string{`id "go" is written more than once - rename one, then pair by hand`},
		claimSkips(summary.Files[0].Skips))
}

// A follower binds only to a claim its own file holds - pairing is per-file
// evidence. A written id in the same file is such a claim: the version line is
// already annotated, so only the follower is proposed, and it must survive.
func TestAnnotateKeepsFollowerForSameFileWrittenID(t *testing.T) {
	root := annotateTree(t, map[string]string{
		"Dockerfile": "FROM alpine\n" +
			"# clover: provider=go id=go\n" +
			"ARG GO_VERSION=1.24.0\n" +
			"ARG GO_SHA256=0000000000000000000000000000000000000000000000000000000000000000\n" +
			"RUN curl -fsSLO https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz\n",
	})

	summary := annotate(t, root, false, false)
	require.Len(t, summary.Files, 1)
	require.Len(t, summary.Files[0].Changes, 1)
	require.Equal(t, followerComment, summary.Files[0].Changes[0].Line)
	require.Empty(t, claimSkips(summary.Files[0].Skips),
		"no claim skip for a same-file pairing")
}
