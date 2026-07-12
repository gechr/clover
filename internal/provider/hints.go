package provider

import "context"

// Hints collects every context hint that shapes what Discover returns. The
// pipeline keys its run-scoped discovery memoization on it, so any new With*
// hint must gain a field here - two lookups may share one result only when the
// provider would see identical hints.
type Hints struct {
	Deep         bool
	Qualifier    string
	TagPrefix    string
	VersionFloor string
}

// HintsFrom reads every discovery hint carried by ctx.
func HintsFrom(ctx context.Context) Hints {
	return Hints{
		Deep:         Deep(ctx),
		Qualifier:    Qualifier(ctx),
		TagPrefix:    TagPrefix(ctx),
		VersionFloor: VersionFloor(ctx),
	}
}
