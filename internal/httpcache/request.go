package httpcache

import (
	"context"
	"time"
)

type requestPolicy struct {
	cacheable         bool
	fallbackFreshness time.Duration
}

type requestPolicyKey struct{}

// WithCacheableRequest marks an idempotent non-GET request as eligible for the
// HTTP cache. When the origin does not provide validators or freshness headers,
// fallbackFreshness supplies a short freshness window for cross-run reuse.
func WithCacheableRequest(ctx context.Context, fallbackFreshness time.Duration) context.Context {
	return context.WithValue(ctx, requestPolicyKey{}, requestPolicy{
		cacheable:         true,
		fallbackFreshness: fallbackFreshness,
	})
}

func policy(ctx context.Context) requestPolicy {
	p, _ := ctx.Value(requestPolicyKey{}).(requestPolicy)
	return p
}
