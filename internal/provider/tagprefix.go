package provider

import "context"

// tagPrefixKey is the unexported context key under which the tag-prefix hint is
// carried.
type tagPrefixKey struct{}

// WithTagPrefix returns a context carrying the marker's tag-prefix (e.g. "api/"
// for a monorepo component). Selection cannot accept a tag without the prefix,
// so a provider may narrow discovery to tags containing it - a strict superset
// of what selection can accept - and skip tags that could never be selected.
func WithTagPrefix(ctx context.Context, prefix string) context.Context {
	return context.WithValue(ctx, tagPrefixKey{}, prefix)
}

// TagPrefix returns the tag-prefix hint for this lookup, "" when the marker has
// none and discovery must stay unfiltered.
func TagPrefix(ctx context.Context) string {
	prefix, _ := ctx.Value(tagPrefixKey{}).(string)
	return prefix
}
