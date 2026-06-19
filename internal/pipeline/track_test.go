package pipeline_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestRunTrackDockerRefreshesDigest(t *testing.T) {
	oldDigest := "sha256:" + strings.Repeat("a", 64)
	newDigest := "sha256:" + strings.Repeat("b", 64)
	provider.Register(fakeProvider{name: "docker", digest: newDigest})

	dir := write(t, map[string]string{
		"Dockerfile": "# clover: provider=docker repository=x/y track=*\n" +
			"FROM x/y:latest@" + oldDigest + "\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	r := files[0].Results[0]
	require.NoError(t, r.Err)
	require.True(t, r.Changed)
	require.Equal(t, "latest", r.Current)
	require.Equal(t, "FROM x/y:latest@"+newDigest, r.NewLine,
		"the floating tag stays, only the digest refreshes")
}

func TestRunTrackDockerExplicitRefRewritesTag(t *testing.T) {
	oldDigest := "sha256:" + strings.Repeat("a", 64)
	newDigest := "sha256:" + strings.Repeat("b", 64)
	provider.Register(fakeProvider{name: "docker", digest: newDigest})

	dir := write(t, map[string]string{
		"Dockerfile": "# clover: provider=docker repository=x/y track=stable\n" +
			"FROM x/y:latest@" + oldDigest + "\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	r := files[0].Results[0]
	require.NoError(t, r.Err)
	require.Equal(t, "FROM x/y:stable@"+newDigest, r.NewLine,
		"an explicit ref drives the tag, not the one on the line")
}

func TestRunTrackGithubRefreshesCommit(t *testing.T) {
	const good = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const old = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	provider.Register(fakeProvider{
		name:         "github",
		tagCommit:    map[string]string{"main": good},
		branches:     []provider.Branch{{Name: "main"}},
		commitBranch: map[string]string{good: "main"},
	})

	dir := write(t, map[string]string{
		".github/workflows/ci.yml": "# clover: provider=github repository=x/y track=* verify-branch=main\n" +
			"  - uses: x/y@" + old + " # main\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	r := files[0].Results[0]
	require.NoError(t, r.Err)
	require.True(t, r.Changed)
	require.Equal(t, "  - uses: x/y@"+good+" # main", r.NewLine,
		"the branch name stays, only the commit refreshes")
	require.NoError(t, r.Verify, "the resolved commit is on the tracked branch")
}

func TestRunTrackCooldownHoldsBackFreshDigest(t *testing.T) {
	now := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	newDigest := "sha256:" + strings.Repeat("b", 64)
	provider.Register(fakeProvider{
		name:   "docker",
		digest: newDigest,
		// the floating tag's current target was published two days ago
		candidates: []model.Candidate{
			{Version: "latest", PublishedAt: now.AddDate(0, 0, -2)},
		},
	})

	oldDigest := "sha256:" + strings.Repeat("a", 64)
	dir := write(t, map[string]string{
		"Dockerfile": "# clover: provider=docker repository=x/y track=* cooldown=7d\n" +
			"FROM x/y:latest@" + oldDigest + "\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir}, pipeline.WithNow(now))
	require.NoError(t, err)
	require.ErrorIs(t, files[0].Results[0].Err, pipeline.ErrNoCandidate,
		"a target younger than the cooldown is held back")
}

func TestRunTrackCooldownAdoptsOldEnoughDigest(t *testing.T) {
	now := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	newDigest := "sha256:" + strings.Repeat("b", 64)
	provider.Register(fakeProvider{
		name:   "docker",
		digest: newDigest,
		candidates: []model.Candidate{
			{Version: "latest", PublishedAt: now.AddDate(0, 0, -30)},
		},
	})

	oldDigest := "sha256:" + strings.Repeat("a", 64)
	dir := write(t, map[string]string{
		"Dockerfile": "# clover: provider=docker repository=x/y track=* cooldown=7d\n" +
			"FROM x/y:latest@" + oldDigest + "\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir}, pipeline.WithNow(now))
	require.NoError(t, err)
	r := files[0].Results[0]
	require.NoError(t, r.Err)
	require.Equal(t, "FROM x/y:latest@"+newDigest, r.NewLine,
		"a target older than the cooldown is adopted")
}

func TestRunTrackRejectsSelectionKeys(t *testing.T) {
	provider.Register(fakeProvider{name: "docker"})

	oldDigest := "sha256:" + strings.Repeat("a", 64)
	dir := write(t, map[string]string{
		"Dockerfile": "# clover: provider=docker repository=x/y track=* constraint=minor\n" +
			"FROM x/y:latest@" + oldDigest + "\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.EqualError(t, files[0].Results[0].Err,
		"track= cannot be used with constraint=")
}

func TestRunTrackNeedsExplicitProvider(t *testing.T) {
	oldDigest := "sha256:" + strings.Repeat("a", 64)
	dir := write(t, map[string]string{
		"Dockerfile": "# clover: track=*\nFROM x/y:latest@" + oldDigest + "\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.EqualError(t, files[0].Results[0].Err,
		"track= needs an explicit provider=")
}
