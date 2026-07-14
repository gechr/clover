package version_test

import (
	"strings"
	"testing"
	"time"

	"github.com/gechr/clover/internal/version"
	xslices "github.com/gechr/x/slices"
	"github.com/stretchr/testify/require"
)

// cand is a minimal candidate the tests select over, mapped to version.Attrs by
// attrsOf.
type cand struct {
	tag        string
	published  time.Time
	assets     []string
	prerelease bool
}

func attrsOf(c cand) version.Attrs {
	v, _ := version.Parse(c.tag)
	return version.Attrs{
		Tag:         c.tag,
		Semver:      v,
		Prerelease:  c.prerelease,
		PublishedAt: c.published,
		Assets:      c.assets,
	}
}

func candidates(tags ...string) []cand {
	return xslices.Map(tags, func(tag string) cand { return cand{tag: tag} })
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

// TestSelectExactPin confirms WithExact overrides the whole chain: the pinned
// version is chosen past the exclude, prerelease, downgrade, and behind rules
// that would each have rejected it.
func TestSelectExactPin(t *testing.T) {
	t.Parallel()

	cur := mustParse(t, "1.5.0")
	cands := candidates("v1.0.0", "1.5.0", "2.0.0-rc.1")

	got, ok := version.Select(cur, cands, attrsOf,
		version.WithExact("1.0.0"),
		version.WithExclude(contains("1.0")),
		version.WithBehind(1))
	require.True(t, ok)
	require.Equal(t, "v1.0.0", got.tag, "parsed equality ignores the v prefix and every other rule")

	got, ok = version.Select(cur, cands, attrsOf, version.WithExact("2.0.0-rc.1"))
	require.True(t, ok)
	require.Equal(t, "2.0.0-rc.1", got.tag, "a pinned prerelease needs no prerelease opt-in")

	got, ok = version.Select(nil, candidates("v1.21.0"), attrsOf, version.WithExact("1.21"))
	require.True(t, ok)
	require.Equal(t, "v1.21.0", got.tag, "precision differences do not defeat the pin")
}

func TestSelectExactPinMissing(t *testing.T) {
	t.Parallel()

	_, reason, ok := version.SelectReason(
		nil,
		candidates("1.0.0", "2.0.0"),
		attrsOf,
		version.WithExact("9.9.9"),
	)
	require.False(t, ok)
	require.Equal(t, version.ReasonExact, reason)
	require.Equal(t, "the requested version is not in the upstream listing", reason.Detail())
}

func TestSelectReasonReportsBinding(t *testing.T) {
	t.Parallel()

	cur, _ := version.Parse("2.0.0")
	// Both candidates are older than current, so downgrade is the binding reason.
	_, reason, ok := version.SelectReason(cur, candidates("1.0.0", "1.1.0"), attrsOf)
	require.False(t, ok)
	require.Equal(t, version.ReasonDowngrade, reason)
	require.Equal(t, "every version is older than the current one", reason.Detail())

	// A successful select reports ReasonEligible.
	_, reason, ok = version.SelectReason(nil, candidates("1.0.0", "2.0.0"), attrsOf)
	require.True(t, ok)
	require.Equal(t, version.ReasonEligible, reason)
}

func TestSelectReasonReportsBindingNotMostNumerous(t *testing.T) {
	t.Parallel()

	// Two tags fail the include filter and only one reaches the prerelease gate.
	// The binding reason is the furthest a candidate got - prerelease - not the
	// more numerous include/exclude rejection.
	keep := version.WithInclude(func(tag string) bool { return strings.HasSuffix(tag, "-ent") })
	_, reason, ok := version.SelectReason(
		nil,
		candidates("1.0.0", "1.1.0", "2.0.0-ent"),
		attrsOf,
		keep,
	)
	require.False(t, ok)
	require.Equal(t, version.ReasonPrerelease, reason)
	require.Equal(t, "only prerelease versions are available", reason.Detail())
}

func TestSelectSchemeGuard(t *testing.T) {
	t.Parallel()

	// A dotted-semver line never jumps to a bare calendar tag, even though the
	// calendar number sorts highest; it stays on the newest dotted version.
	cur := mustParse(t, "3.18.0")
	got, ok := version.Select(cur, candidates("3.24.1", "20260127"), attrsOf)
	require.True(t, ok)
	require.Equal(t, "3.24.1", got.tag, "calendar tag rejected against a dotted line")

	// The guard is symmetric: a calendar line never jumps to dotted semver.
	cur = mustParse(t, "20240101")
	got, ok = version.Select(cur, candidates("20260127", "3.24.1"), attrsOf)
	require.True(t, ok)
	require.Equal(t, "20260127", got.tag, "dotted tag rejected against a calendar line")

	// When only a different-scheme candidate exists, the failure names scheme.
	cur = mustParse(t, "3.18.0")
	_, reason, ok := version.SelectReason(cur, candidates("20260127"), attrsOf)
	require.False(t, ok)
	require.Equal(t, version.ReasonScheme, reason)

	// 2- vs 3-part precision is not a scheme change: a patch bump is allowed.
	cur = mustParse(t, "7.2")
	got, ok = version.Select(cur, candidates("7.4.9"), attrsOf)
	require.True(t, ok)
	require.Equal(t, "7.4.9", got.tag, "precision difference is not a scheme mismatch")

	// WithBareMajor declares a single-number line a major pin (a mise
	// node = "24"), so dotted candidates stay eligible and the newest wins.
	cur = mustParse(t, "24")
	got, ok = version.Select(
		cur,
		candidates("24.4.1", "25.1.0"),
		attrsOf,
		version.WithBareMajor(true),
	)
	require.True(t, ok)
	require.Equal(t, "25.1.0", got.tag, "a bare-major pin selects dotted versions")

	// Without the option the same line keeps the calendar-tag guard.
	_, reason, ok = version.SelectReason(cur, candidates("24.4.1", "25.1.0"), attrsOf)
	require.False(t, ok)
	require.Equal(t, version.ReasonScheme, reason)
}

func TestSelectQualifierExempt(t *testing.T) {
	t.Parallel()

	// An -ent line, scoped to -ent by an include, selects the newest -ent even
	// though semver reads -ent as a prerelease: WithQualifier spares the track.
	cur := mustParse(t, "1.15.0-ent")
	keep := version.WithInclude(func(tag string) bool { return version.Qualifier(tag) == "ent" })
	got, ok := version.Select(
		cur,
		candidates("1.15.0-ent", "2.0.3-ent"),
		attrsOf,
		keep,
		version.WithQualifier("ent"),
	)
	require.True(t, ok)
	require.Equal(t, "2.0.3-ent", got.tag)

	// Without the exemption the same field is rejected as prerelease.
	_, reason, ok := version.SelectReason(cur, candidates("1.15.0-ent", "2.0.3-ent"), attrsOf, keep)
	require.False(t, ok)
	require.Equal(t, version.ReasonPrerelease, reason)

	// The exemption is scoped to the matched suffix: a true prerelease on a
	// different track is still excluded.
	_, reason, ok = version.SelectReason(
		cur,
		candidates("2.1.0-rc1"),
		attrsOf,
		version.WithQualifier("ent"),
	)
	require.False(t, ok)
	require.Equal(t, version.ReasonPrerelease, reason)
}

func TestSelectPrereleaseFlag(t *testing.T) {
	t.Parallel()

	// A clean-tagged candidate the upstream flags prerelease is excluded, even
	// though its tag carries no prerelease segment.
	cands := []cand{{tag: "1.2.0"}, {tag: "1.3.0", prerelease: true}}
	got, ok := version.Select(nil, cands, attrsOf)
	require.True(t, ok)
	require.Equal(t, "1.2.0", got.tag, "flagged prerelease excluded despite a clean tag")

	// The flag is additive: a structural -rc prerelease is still excluded with no
	// flag set, so an unflagged rc release does not slip through.
	cands = []cand{{tag: "1.2.0"}, {tag: "1.3.0-rc1"}}
	got, ok = version.Select(nil, cands, attrsOf)
	require.True(t, ok)
	require.Equal(t, "1.2.0", got.tag, "unflagged -rc still excluded by semver")

	// --prerelease opts into both.
	got, ok = version.Select(
		nil,
		[]cand{{tag: "1.3.0", prerelease: true}},
		attrsOf,
		version.WithPrerelease(true),
	)
	require.True(t, ok)
	require.Equal(t, "1.3.0", got.tag)
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

func TestSelectTieBreakPrefersMoreSpecific(t *testing.T) {
	t.Parallel()

	// v7 and v7.0.0 are semver-equal; the tie-break prefers the more specific tag
	// (more numeric components), deterministically regardless of input order. The
	// precise, immutable v7.0.0 outranks a floating v7 that points at the same
	// commit, so the pin is documented with the exact release.
	got1, ok := version.Select(nil, candidates("v7", "v7.0.0"), attrsOf)
	require.True(t, ok)
	require.Equal(t, "v7.0.0", got1.tag)

	got2, ok := version.Select(nil, candidates("v7.0.0", "v7"), attrsOf)
	require.True(t, ok)
	require.Equal(t, "v7.0.0", got2.tag)
}

func TestSelectTieBreakEqualPrecisionPrefersShorter(t *testing.T) {
	t.Parallel()

	// 1.2.3+build and 1.2.3 are semver-equal at equal precision (build metadata
	// does not affect precedence); the tie-break falls back to the shorter (less
	// decorated) tag, so a plain tag is never out-ranked by an equal-precision
	// decorated tag sharing its numeric core (e.g. a stripped docker variant).
	got, ok := version.Select(nil, candidates("1.2.3+build", "1.2.3"), attrsOf)
	require.True(t, ok)
	require.Equal(t, "1.2.3", got.tag)
}
