package provider

import "context"

// deepKey is the unexported context key under which deep-lookup is carried.
type deepKey struct{}

// WithDeep returns a context that asks providers to perform a deep lookup -
// following pagination to exhaustion rather than reading only the first page.
// It is a run-scoped flag the CLI sets from --deep and providers read in
// Discover, so the lookup depth need not widen the Provider interface.
func WithDeep(ctx context.Context, deep bool) context.Context {
	return context.WithValue(ctx, deepKey{}, deep)
}

// Deep reports whether a deep lookup was requested for this run. The default is
// shallow (first page only), which suffices for recency-ordered sources and
// keeps a run fast and within rate limits.
func Deep(ctx context.Context) bool {
	deep, _ := ctx.Value(deepKey{}).(bool)
	return deep
}
