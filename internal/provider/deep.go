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

// Truncation identifies a resource whose shallow lookup was incomplete, paired
// with the upstream web page a --deep hint can link to.
type Truncation struct {
	Resource string // short label shown in the hint
	URL      string // upstream web page the label links to
}

// truncationSinkKey is the unexported context key for the truncation sink.
type truncationSinkKey struct{}

// WithTruncationSink returns a context carrying a callback a provider invokes
// (via NoteTruncated) when a shallow lookup stopped with more results still
// available - so the edge can suggest --deep without the provider depending on a
// logger. The sink may be called from multiple goroutines, so it must be safe
// for concurrent use. A nil sink disables the notification.
func WithTruncationSink(ctx context.Context, sink func(Truncation)) context.Context {
	return context.WithValue(ctx, truncationSinkKey{}, sink)
}

// NoteTruncated reports that resource's shallow lookup was incomplete, invoking
// the sink set by WithTruncationSink if any with the resource's label and its
// upstream web page. It is a no-op otherwise, so a provider can call it
// unconditionally.
func NoteTruncated(ctx context.Context, resource, url string) {
	if sink, ok := ctx.Value(truncationSinkKey{}).(func(Truncation)); ok && sink != nil {
		sink(Truncation{Resource: resource, URL: url})
	}
}
