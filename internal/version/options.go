package version

import "time"

// Option configures [Select].
type Option func(*query)

type query struct {
	assets     []Predicate
	bareMajor  bool
	behind     int
	constraint *Constraint
	cooldown   time.Duration
	downgrade  bool
	excludes   []Predicate
	includes   []Predicate
	now        time.Time
	observe    func(tag string, reason Reason)
	prerelease bool

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

// WithAsset keeps only candidates publishing an asset whose filename matches at
// least one predicate. With no asset predicates every candidate is eligible.
func WithAsset(preds ...Predicate) Option {
	return func(q *query) { q.assets = append(q.assets, preds...) }
}

// WithBareMajor treats a single-number version on the line as a major-precision
// pin rather than a calendar tag, so dotted candidates stay eligible against it
// (node = "24" selecting 25.1.0). It applies where the file format gives a bare
// number that meaning (a mise tool pin) and leaves the calendar-tag guard in
// force everywhere else.
func WithBareMajor(allow bool) Option { return func(q *query) { q.bareMajor = allow } }

// WithBehind selects the nth candidate behind the newest after all filtering (0
// = newest).
func WithBehind(n int) Option { return func(q *query) { q.behind = n } }

// WithConstraint limits selection to versions the constraint allows.
func WithConstraint(c *Constraint) Option { return func(q *query) { q.constraint = c } }

// WithCooldown requires a candidate to be at least d old, measured against the
// time set by [WithNow]. Without an injected now, or for a candidate with no
// publish time, cooldown does not apply.
func WithCooldown(d time.Duration) Option { return func(q *query) { q.cooldown = d } }

// WithDowngrade permits selecting a version older than current.
func WithDowngrade(allow bool) Option { return func(q *query) { q.downgrade = allow } }

// WithExclude drops candidates matching any predicate.
func WithExclude(preds ...Predicate) Option {
	return func(q *query) { q.excludes = append(q.excludes, preds...) }
}

// WithInclude keeps only candidates matching at least one predicate. With no
// include predicates every candidate is eligible.
func WithInclude(preds ...Predicate) Option {
	return func(q *query) { q.includes = append(q.includes, preds...) }
}

// WithNow injects the reference time for cooldown, keeping selection free of a
// hidden clock and so deterministic.
func WithNow(t time.Time) Option { return func(q *query) { q.now = t } }

// WithObserver reports each candidate the chain rejects, together with the
// [Reason]. It fires only for skipped candidates, letting a caller surface why a
// version was passed over (e.g. at debug level) without this package taking on a
// logging dependency.
func WithObserver(fn func(tag string, reason Reason)) Option {
	return func(q *query) { q.observe = fn }
}

// WithPrerelease allows prerelease versions, which are otherwise excluded.
func WithPrerelease(allow bool) Option { return func(q *query) { q.prerelease = allow } }

// WithQualifier exempts candidates carrying the same trailing suffix as the line
// (e.g. "ent" for a 1.15.0-ent pin) from the prerelease gate, so a vendor track
// that semver reads as a prerelease stays selectable without opening every
// prerelease. Empty disables the exemption.
func WithQualifier(q string) Option { return func(query *query) { query.qualifier = q } }
