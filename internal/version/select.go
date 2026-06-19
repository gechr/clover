package version

import (
	"slices"
	"strings"
	"time"
)

// Attrs is the slice of a candidate the selection chain reads. Callers supply
// an extractor mapping their own candidate type to Attrs, so this package needs
// no knowledge of (and no import cycle with) the richer candidate model.
type Attrs struct {
	// Tag is the raw version string, matched by the include/exclude predicates.
	Tag string
	// Semver is the parsed version. A nil Semver is unorderable and never
	// selected.
	Semver *Version
	// PublishedAt is the release time, consulted by cooldown. Zero disables
	// cooldown for the candidate.
	PublishedAt time.Time
}

// Predicate reports whether a raw tag matches. include/exclude are supplied as
// predicates rather than patterns so this package stays free of the pattern
// engine that sits above it.
type Predicate func(tag string) bool

// Option configures [Select].
type Option func(*query)

type query struct {
	constraint     *Constraint
	includes       []Predicate
	excludes       []Predicate
	cooldown       time.Duration
	now            time.Time
	behind         int
	prerelease     bool
	allowDowngrade bool
}

// WithConstraint limits selection to versions the constraint allows.
func WithConstraint(c *Constraint) Option { return func(q *query) { q.constraint = c } }

// WithInclude keeps only candidates matching at least one predicate. With no
// include predicates every candidate is eligible.
func WithInclude(preds ...Predicate) Option {
	return func(q *query) { q.includes = append(q.includes, preds...) }
}

// WithExclude drops candidates matching any predicate.
func WithExclude(preds ...Predicate) Option {
	return func(q *query) { q.excludes = append(q.excludes, preds...) }
}

// WithPrerelease allows prerelease versions, which are otherwise excluded.
func WithPrerelease(allow bool) Option { return func(q *query) { q.prerelease = allow } }

// WithCooldown requires a candidate to be at least d old, measured against the
// time set by [WithNow]. Without an injected now, or for a candidate with no
// publish time, cooldown does not apply.
func WithCooldown(d time.Duration) Option { return func(q *query) { q.cooldown = d } }

// WithNow injects the reference time for cooldown, keeping selection free of a
// hidden clock and so deterministic.
func WithNow(t time.Time) Option { return func(q *query) { q.now = t } }

// WithBehind selects the nth candidate behind the newest after all filtering (0
// = newest).
func WithBehind(n int) Option { return func(q *query) { q.behind = n } }

// WithAllowDowngrade permits selecting a version older than current.
func WithAllowDowngrade(allow bool) Option { return func(q *query) { q.allowDowngrade = allow } }

// scored pairs a candidate with the fields the chain sorts and tie-breaks on,
// so the extractor runs once per candidate rather than once per comparison.
type scored[T any] struct {
	item   T
	semver *Version
	tag    string
}

// Select runs the selection chain over cands and returns the chosen candidate.
// The chain filters by include/exclude, prerelease, cooldown, constraint, and
// downgrade, then sorts the survivors newest-first and picks the one behind
// places back. ok is false when nothing survives or behind overshoots.
//
// current is the version in the file: it bounds downgrades and, via a keyword
// constraint, anchors the allowed range. The sort tie-breaks equal versions by
// raw tag so the result is deterministic regardless of discovery order.
func Select[T any](current *Version, cands []T, attrs func(T) Attrs, opts ...Option) (T, bool) {
	q := query{}
	for _, opt := range opts {
		opt(&q)
	}

	kept := make([]scored[T], 0, len(cands))
	for _, c := range cands {
		a := attrs(c)
		if q.eligible(current, a) {
			kept = append(kept, scored[T]{item: c, semver: a.Semver, tag: a.Tag})
		}
	}

	if q.behind >= len(kept) {
		var zero T
		return zero, false
	}

	slices.SortStableFunc(kept, func(x, y scored[T]) int {
		if c := Compare(y.semver, x.semver); c != 0 {
			return c
		}
		return strings.Compare(y.tag, x.tag)
	})
	return kept[q.behind].item, true
}

// eligible reports whether a candidate survives every filter in the chain.
func (q *query) eligible(current *Version, a Attrs) bool {
	switch {
	case a.Semver == nil:
		return false
	case !q.included(a.Tag) || q.excluded(a.Tag):
		return false
	case !q.prerelease && isPrerelease(a.Semver):
		return false
	case q.tooFresh(a.PublishedAt):
		return false
	case !q.constraint.Allowed(a.Semver):
		return false
	case q.isDowngrade(current, a.Semver):
		return false
	default:
		return true
	}
}

// included reports whether tag matches the include set (OR), or true when no
// include predicates were given.
func (q *query) included(tag string) bool {
	if len(q.includes) == 0 {
		return true
	}
	for _, p := range q.includes {
		if p(tag) {
			return true
		}
	}
	return false
}

// excluded reports whether tag matches any exclude predicate.
func (q *query) excluded(tag string) bool {
	for _, p := range q.excludes {
		if p(tag) {
			return true
		}
	}
	return false
}

// tooFresh reports whether a candidate published at t is younger than the
// cooldown. It is inert without a cooldown, an injected now, or a publish time.
func (q *query) tooFresh(t time.Time) bool {
	return TooFresh(q.now, t, q.cooldown)
}

// TooFresh reports whether something published at published is younger than
// cooldown, measured against now. It is inert (false) without a cooldown, a
// reference now, or a publish time, so the track path can reuse the same
// freshness rule selection applies, passing zero values freely.
func TooFresh(now, published time.Time, cooldown time.Duration) bool {
	if cooldown <= 0 || now.IsZero() || published.IsZero() {
		return false
	}
	return now.Sub(published) < cooldown
}

// isDowngrade reports whether cand is older than current, unless downgrades are
// allowed or there is no current to compare against.
func (q *query) isDowngrade(current, cand *Version) bool {
	if q.allowDowngrade || current == nil {
		return false
	}
	return Compare(cand, current) < 0
}

// isPrerelease reports whether v carries a prerelease segment.
func isPrerelease(v *Version) bool { return v.Prerelease() != "" }
