package provider

import "context"

// qualifierKey is the unexported context key under which the qualifier hint is
// carried.
type qualifierKey struct{}

// WithQualifier returns a context carrying the located tag's qualifier (e.g.
// "alpine3.22" for a line on 1.27-alpine3.22). The pipeline sets it only when
// selection is pinned to that exact qualifier, so a provider may narrow
// discovery to tags containing it - a strict superset of what selection can
// accept - and skip pages that could never be selected.
func WithQualifier(ctx context.Context, qualifier string) context.Context {
	return context.WithValue(ctx, qualifierKey{}, qualifier)
}

// Qualifier returns the qualifier hint for this lookup, "" when selection is
// not pinned to one and discovery must stay unfiltered.
func Qualifier(ctx context.Context) string {
	qualifier, _ := ctx.Value(qualifierKey{}).(string)
	return qualifier
}
