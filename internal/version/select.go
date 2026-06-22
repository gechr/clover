package version

import (
	"cmp"
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
	// Assets are the candidate's published asset filenames, matched by the asset
	// predicate. Empty for a provider that publishes none (tags, docker, helm).
	Assets []string
}

// Predicate reports whether a raw tag matches. include/exclude are supplied as
// predicates rather than patterns so this package stays free of the pattern
// engine that sits above it.
type Predicate func(tag string) bool

// Reason explains why the chain rejected a candidate. The zero value,
// [ReasonEligible], means the candidate survived every filter.
type Reason int

const (
	ReasonEligible   Reason = iota // survived every filter
	ReasonUnparsable               // tag is not a parsable version
	ReasonFiltered                 // dropped by include/exclude
	ReasonScheme                   // a different version scheme than the line (e.g. calendar tag vs dotted semver)
	ReasonNoAsset                  // no published asset matched the asset filter
	ReasonPrerelease               // a prerelease, and they are not allowed
	ReasonCooldown                 // younger than the cooldown
	ReasonConstraint               // outside the constraint
	ReasonDowngrade                // older than current, downgrades disallowed
	reasonCount                    // count of reasons, for tallying the binding one
)

// String renders the reason as a short, stable label for logging.
func (r Reason) String() string {
	switch r { //nolint:exhaustive // reasonCount is a tallying sentinel, not a reason; default covers it
	case ReasonEligible:
		return "eligible"
	case ReasonUnparsable:
		return "unparsable"
	case ReasonFiltered:
		return "filtered"
	case ReasonScheme:
		return "scheme"
	case ReasonNoAsset:
		return "no-asset"
	case ReasonPrerelease:
		return "prerelease"
	case ReasonCooldown:
		return "cooldown"
	case ReasonConstraint:
		return "constraint"
	case ReasonDowngrade:
		return "downgrade"
	default:
		return "unknown"
	}
}

// Detail is a human phrase explaining a rejection, used to enrich the
// no-candidate error so a failed run says why. It is empty for reasons that need
// no elaboration.
func (r Reason) Detail() string {
	switch r { //nolint:exhaustive // ReasonEligible and the reasonCount sentinel need no detail; default covers them
	case ReasonUnparsable:
		return "no parsable version was found"
	case ReasonFiltered:
		return "no version matched the include/exclude filters"
	case ReasonScheme:
		return "no version shares the version scheme on the line"
	case ReasonNoAsset:
		return "no version published a matching asset"
	case ReasonPrerelease:
		return "only prerelease versions are available"
	case ReasonCooldown:
		return "every version is still within its cooldown"
	case ReasonConstraint:
		return "no version satisfies the constraint"
	case ReasonDowngrade:
		return "every version is older than the current one"
	default:
		return ""
	}
}

// Option configures [Select].
type Option func(*query)

type query struct {
	constraint *Constraint
	includes   []Predicate
	excludes   []Predicate
	assets     []Predicate
	cooldown   time.Duration
	now        time.Time
	behind     int
	prerelease bool
	downgrade  bool
	observe    func(tag string, reason Reason)

	// currentArity is the count of dotted numeric components in the version on
	// the line (3 for 3.18.0, 1 for a bare calendar tag like 20260127), or 0
	// when there is no current version. It anchors the scheme guard, computed
	// once in [SelectReason] rather than per candidate.
	currentArity int

	// qualifier is the trailing suffix on the line (the "ent" in 1.15.0-ent), or
	// "". A candidate sharing it is exempt from the prerelease gate, so a line
	// pinned to a vendor track keeps that track without the suffix counting as a
	// prerelease.
	qualifier string
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

// WithAsset keeps only candidates publishing an asset whose filename matches at
// least one predicate. With no asset predicates every candidate is eligible.
func WithAsset(preds ...Predicate) Option {
	return func(q *query) { q.assets = append(q.assets, preds...) }
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

// WithDowngrade permits selecting a version older than current.
func WithDowngrade(allow bool) Option { return func(q *query) { q.downgrade = allow } }

// WithQualifier exempts candidates carrying the same trailing suffix as the line
// (e.g. "ent" for a 1.15.0-ent pin) from the prerelease gate, so a vendor track
// that semver reads as a prerelease stays selectable without opening every
// prerelease. Empty disables the exemption.
func WithQualifier(q string) Option { return func(query *query) { query.qualifier = q } }

// WithObserver reports each candidate the chain rejects, together with the
// [Reason]. It fires only for skipped candidates, letting a caller surface why a
// version was passed over (e.g. at debug level) without this package taking on a
// logging dependency.
func WithObserver(fn func(tag string, reason Reason)) Option {
	return func(q *query) { q.observe = fn }
}

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
	item, _, ok := SelectReason(current, cands, attrs, opts...)
	return item, ok
}

// SelectReason is [Select], additionally reporting why no candidate was chosen.
// When ok is false it returns the binding reason: the latest-stage filter any
// candidate reached before being rejected (see [bindingReason]), so a caller
// explains the failure by the constraint that actually stood in the way rather
// than the one the most tags happened to trip. When ok is true the reason is
// [ReasonEligible].
func SelectReason[T any](
	current *Version,
	cands []T,
	attrs func(T) Attrs,
	opts ...Option,
) (T, Reason, bool) {
	q := query{}
	for _, opt := range opts {
		opt(&q)
	}
	if current != nil {
		q.currentArity = numericArity(current.Original())
	}

	kept := make([]scored[T], 0, len(cands))
	var rejected [reasonCount]int
	for _, c := range cands {
		a := attrs(c)
		if r := q.eligible(current, a); r != ReasonEligible {
			rejected[r]++
			if q.observe != nil {
				q.observe(a.Tag, r)
			}
			continue
		}
		kept = append(kept, scored[T]{item: c, semver: a.Semver, tag: a.Tag})
	}

	if q.behind >= len(kept) {
		var zero T
		return zero, bindingReason(rejected), false
	}

	slices.SortStableFunc(kept, func(x, y scored[T]) int {
		if c := Compare(y.semver, x.semver); c != 0 {
			return c
		}
		// Equal versions: prefer the shorter (less decorated) tag, so a plain
		// tag is never out-ranked by a variant of the same version; fall back to
		// a lexical compare for a deterministic order.
		if c := cmp.Compare(len(x.tag), len(y.tag)); c != 0 {
			return c
		}
		return strings.Compare(y.tag, x.tag)
	})
	return kept[q.behind].item, ReasonEligible, true
}

// bindingReason returns the binding rejection reason from a tally: the
// latest-stage filter any candidate reached before being rejected. Because
// [query.eligible] checks filters in a fixed order and the [Reason] constants
// are declared in that same order, the highest reason with a hit is the furthest
// a candidate progressed - the constraint that actually stood between the field
// and a selection. Reporting that, rather than the most numerous reason, keeps a
// flood of tags failing an early filter (say include/exclude) from masking the
// handful that passed it only to fail a later one (say prerelease). It is
// [ReasonEligible] when nothing was rejected (no candidates at all).
func bindingReason(rejected [reasonCount]int) Reason {
	for r := reasonCount - 1; r > ReasonEligible; r-- {
		if rejected[r] > 0 {
			return r
		}
	}
	return ReasonEligible
}

// eligible reports the [Reason] a candidate was rejected, or [ReasonEligible]
// when it survives every filter. The order of the checks fixes the reported
// reason when more than one would apply.
func (q *query) eligible(current *Version, a Attrs) Reason {
	switch {
	case a.Semver == nil:
		return ReasonUnparsable
	case !q.included(a.Tag) || q.excluded(a.Tag):
		return ReasonFiltered
	case q.schemeMismatch(a.Tag):
		return ReasonScheme
	case len(q.assets) > 0 && !q.hasAsset(a.Assets):
		return ReasonNoAsset
	case !q.prerelease && isPrerelease(a.Semver) && !q.qualifierExempt(a.Tag):
		return ReasonPrerelease
	case q.tooFresh(a.PublishedAt):
		return ReasonCooldown
	case !q.constraint.Allowed(a.Semver):
		return ReasonConstraint
	case q.isDowngrade(current, a.Semver):
		return ReasonDowngrade
	default:
		return ReasonEligible
	}
}

// qualifierExempt reports whether a candidate shares the line's trailing
// suffix, so its prerelease segment is the wanted vendor track (1.15.0-ent
// selecting 2.0.3-ent) rather than a true prerelease to exclude. The scoping to
// that suffix is the include's job; this only spares the matched track from the
// prerelease gate.
func (q *query) qualifierExempt(tag string) bool {
	return q.qualifier != "" && Qualifier(tag) == q.qualifier
}

// schemeMismatch reports whether a candidate uses a different version scheme
// than the line: a bare single-number tag (a calendar stamp like 20260127) may
// not replace a dotted multi-part version (3.18.0), nor the reverse. It is the
// scheme-level distinction only - 2- vs 3-part precision is deliberately not
// locked - so it catches the calendar-tag-beats-semver footgun without
// rejecting an ordinary patch or major bump. Inert when the line has no current
// version (currentArity 0) or the candidate has no numeric core.
func (q *query) schemeMismatch(tag string) bool {
	if q.currentArity == 0 {
		return false
	}
	if n := numericArity(tag); n != 0 {
		return (n == 1) != (q.currentArity == 1)
	}
	return false
}

// numericArity counts the leading dotted numeric components of a version
// string: 3.18.0 -> 3, 7.2 -> 2, 20260127 -> 1, 1.15.0-ent -> 3. A leading v is
// ignored and a prerelease or build suffix is dropped, so the count reflects the
// version core's shape. It is 0 when the string has no numeric core.
func numericArity(tag string) int {
	core := strings.TrimPrefix(tag, "v")
	core, _, _ = strings.Cut(core, "-")
	core, _, _ = strings.Cut(core, "+")
	n := 0
	for part := range strings.SplitSeq(core, ".") {
		if part == "" ||
			strings.IndexFunc(part, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			break
		}
		n++
	}
	return n
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

// hasAsset reports whether any asset filename matches any asset predicate, so a
// candidate qualifies when it publishes at least one matching asset.
func (q *query) hasAsset(names []string) bool {
	for _, p := range q.assets {
		if slices.ContainsFunc(names, p) {
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
	if q.downgrade || current == nil {
		return false
	}
	return Compare(cand, current) < 0
}

// isPrerelease reports whether v carries a prerelease segment.
func isPrerelease(v *Version) bool { return v.Prerelease() != "" }
