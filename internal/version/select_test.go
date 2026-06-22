package version_test

import (
	"strings"
	"testing"
	"time"

	"github.com/gechr/clover/internal/version"
	"github.com/stretchr/testify/require"
)

// cand is a minimal candidate the tests select over, mapped to version.Attrs by
// attrsOf.
type cand struct {
	tag       string
	published time.Time
	assets    []string
}

func attrsOf(c cand) version.Attrs {
	v, _ := version.Parse(c.tag)
	return version.Attrs{Tag: c.tag, Semver: v, PublishedAt: c.published, Assets: c.assets}
}

func candidates(tags ...string) []cand {
	cands := make([]cand, len(tags))
	for i, tag := range tags {
		cands[i] = cand{tag: tag}
	}
	return cands
}

// contains is an include/exclude predicate matching tags containing sub.
func contains(sub string) version.Predicate {
	return func(tag string) bool { return strings.Contains(tag, sub) }
}

func TestSelectPicksNewest(t *testing.T) {
	t.Parallel()

	got, ok := version.Select(nil, candidates("1.2.0", "1.4.0", "1.3.0"), attrsOf)
	require.True(t, ok)
	require.Equal(t, "1.4.0", got.tag)
}

func TestSelectEmpty(t *testing.T) {
	t.Parallel()

	_, ok := version.Select(nil, candidates(), attrsOf)
	require.False(t, ok)
}

func TestSelectSkipsUnparseable(t *testing.T) {
	t.Parallel()

	got, ok := version.Select(nil, candidates("latest", "1.2.0", "nightly"), attrsOf)
	require.True(t, ok)
	require.Equal(t, "1.2.0", got.tag)
}

func TestSelectPrerelease(t *testing.T) {
	t.Parallel()

	cands := candidates("1.2.0", "1.3.0-rc.1")

	got, ok := version.Select(nil, cands, attrsOf)
	require.True(t, ok)
	require.Equal(t, "1.2.0", got.tag, "prereleases excluded by default")

	got, ok = version.Select(nil, cands, attrsOf, version.WithPrerelease(true))
	require.True(t, ok)
	require.Equal(t, "1.3.0-rc.1", got.tag)
}

func TestSelectIncludeExclude(t *testing.T) {
	t.Parallel()

	// include/exclude match on the raw tag, independent of the parsed semver.
	// (Variant suffixes like -alpine are stripped to a core semver by the
	// provider's candidate parser via SplitVariant; here the tags are clean.)
	cands := candidates("1.2.0", "1.3.0", "1.4.0")

	got, ok := version.Select(nil, cands, attrsOf, version.WithInclude(contains("1.3")))
	require.True(t, ok)
	require.Equal(t, "1.3.0", got.tag, "include keeps only matching tags")

	got, ok = version.Select(nil, cands, attrsOf, version.WithExclude(contains("1.4")))
	require.True(t, ok)
	require.Equal(t, "1.3.0", got.tag, "exclude drops matching tags")
}

func TestSelectAsset(t *testing.T) {
	t.Parallel()

	// Only releases publishing a linux asset qualify; the newest of those wins,
	// even though 1.3.0 (darwin-only) is newer than 1.2.0.
	cands := []cand{
		{tag: "1.2.0", assets: []string{"app_1.2.0_linux_amd64.tar.gz"}},
		{tag: "1.4.0", assets: []string{"app_1.4.0_linux_amd64.tar.gz"}},
		{tag: "1.3.0", assets: []string{"app_1.3.0_darwin_arm64.tar.gz"}},
	}
	got, ok := version.Select(nil, cands, attrsOf, version.WithAsset(contains("linux_amd64")))
	require.True(t, ok)
	require.Equal(t, "1.4.0", got.tag)
}

func TestSelectAssetNoneMatch(t *testing.T) {
	t.Parallel()

	cands := []cand{{tag: "1.2.0", assets: []string{"app_darwin.tar.gz"}}}
	_, ok := version.Select(nil, cands, attrsOf, version.WithAsset(contains("linux")))
	require.False(t, ok)
}

func TestSelectAssetAndsWithExclude(t *testing.T) {
	t.Parallel()

	// asset and exclude both narrow: 1.4.0 has the asset but is excluded by tag,
	// so the older 1.2.0 wins.
	cands := []cand{
		{tag: "1.2.0", assets: []string{"app_linux.tar.gz"}},
		{tag: "1.4.0", assets: []string{"app_linux.tar.gz"}},
	}
	got, ok := version.Select(nil, cands, attrsOf,
		version.WithAsset(contains("linux")), version.WithExclude(contains("1.4")))
	require.True(t, ok)
	require.Equal(t, "1.2.0", got.tag)
}

func TestSelectReasonReportsDominant(t *testing.T) {
	t.Parallel()

	cur, _ := version.Parse("2.0.0")
	// Both candidates are older than current, so downgrade is the dominant reason.
	_, reason, ok := version.SelectReason(cur, candidates("1.0.0", "1.1.0"), attrsOf)
	require.False(t, ok)
	require.Equal(t, version.ReasonDowngrade, reason)
	require.Equal(t, "every version is older than the current one", reason.Detail())

	// A successful select reports ReasonEligible.
	_, reason, ok = version.SelectReason(nil, candidates("1.0.0", "2.0.0"), attrsOf)
	require.True(t, ok)
	require.Equal(t, version.ReasonEligible, reason)
}

func TestSelectConstraint(t *testing.T) {
	t.Parallel()

	cands := candidates("1.2.0", "1.3.0", "2.0.0")
	current := mustParse(t, "1.2.0")

	c, err := version.NewConstraint("minor", current)
	require.NoError(t, err)

	got, ok := version.Select(current, cands, attrsOf, version.WithConstraint(c))
	require.True(t, ok)
	require.Equal(t, "1.3.0", got.tag, "major bump excluded by minor ceiling")
}

func TestSelectBehind(t *testing.T) {
	t.Parallel()

	cands := candidates("1.2.0", "1.3.0", "1.4.0")

	got, ok := version.Select(nil, cands, attrsOf, version.WithBehind(1))
	require.True(t, ok)
	require.Equal(t, "1.3.0", got.tag)

	_, ok = version.Select(nil, cands, attrsOf, version.WithBehind(5))
	require.False(t, ok, "behind past the end selects nothing")
}

func TestSelectBehindCountsOnlyEligible(t *testing.T) {
	t.Parallel()

	// The prerelease is filtered before behind indexes, so it must not occupy a
	// slot: behind=1 steps from 2.0.0 straight to 1.2.0, skipping the rc.
	cands := candidates("1.2.0", "1.3.0-rc.1", "2.0.0")

	got, ok := version.Select(nil, cands, attrsOf, version.WithBehind(1))
	require.True(t, ok)
	require.Equal(t, "1.2.0", got.tag)
}

func TestSelectBehindRespectsDowngrade(t *testing.T) {
	t.Parallel()

	cands := candidates("1.2.0", "1.3.0", "1.4.0")
	current, err := version.Parse("1.4.0") // already on the newest
	require.NoError(t, err)

	// behind would step below current, which is a downgrade: refused by default so
	// a manual bump is never silently reverted.
	_, ok := version.Select(current, cands, attrsOf, version.WithBehind(1))
	require.False(t, ok, "behind must not downgrade a current pin")

	// Opting into downgrades lets behind step back.
	got, ok := version.Select(current, cands, attrsOf,
		version.WithBehind(1), version.WithDowngrade(true))
	require.True(t, ok)
	require.Equal(t, "1.3.0", got.tag)
}

func TestSelectNaturalPrerelease(t *testing.T) {
	t.Parallel()

	// beta10 is newer than beta9: a lexical sort would pick beta9.
	got, ok := version.Select(nil, candidates("1.0.0-beta9", "1.0.0-beta10"), attrsOf,
		version.WithPrerelease(true))
	require.True(t, ok)
	require.Equal(t, "1.0.0-beta10", got.tag)
}

func TestSelectDowngrade(t *testing.T) {
	t.Parallel()

	cands := candidates("1.2.0", "1.1.0")
	current := mustParse(t, "1.2.0")

	_, ok := version.Select(current, cands, attrsOf)
	require.True(t, ok)

	// Only an older candidate, downgrades disallowed → nothing.
	_, ok = version.Select(current, candidates("1.1.0"), attrsOf)
	require.False(t, ok)

	got, ok := version.Select(
		current,
		candidates("1.1.0"),
		attrsOf,
		version.WithDowngrade(true),
	)
	require.True(t, ok)
	require.Equal(t, "1.1.0", got.tag)
}

func TestSelectCooldown(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 17, 0, 0, 0, 0, time.UTC)
	cands := []cand{
		{tag: "1.2.0", published: now.Add(-10 * 24 * time.Hour)},
		{tag: "1.3.0", published: now.Add(-1 * time.Hour)},
	}

	got, ok := version.Select(nil, cands, attrsOf,
		version.WithCooldown(3*24*time.Hour), version.WithNow(now))
	require.True(t, ok)
	require.Equal(t, "1.2.0", got.tag, "fresh 1.3.0 held back by cooldown")
}

func TestSelectTieBreakPrefersShorter(t *testing.T) {
	t.Parallel()

	// 1.2 and 1.2.0 are semver-equal; the tie-break prefers the shorter (less
	// decorated) tag, deterministically regardless of input order. This keeps a
	// plain tag from being out-ranked by a longer, more-decorated equal version.
	got1, ok := version.Select(nil, candidates("1.2", "1.2.0"), attrsOf)
	require.True(t, ok)
	require.Equal(t, "1.2", got1.tag)

	got2, ok := version.Select(nil, candidates("1.2.0", "1.2"), attrsOf)
	require.True(t, ok)
	require.Equal(t, "1.2", got2.tag)
}
