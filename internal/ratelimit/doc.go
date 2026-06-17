// Package ratelimit provides an http.RoundTripper that respects an API's rate
// limit headers. It reads X-RateLimit-Remaining/Reset and Retry-After from
// responses and, once the remaining quota reaches zero, short-circuits further
// requests until the reset time rather than hammering a limit that is already
// exhausted. A rate-limited request yields a typed *Error carrying the reset
// time, so callers can fail loud with a clear message.
//
// It composes beneath the httpcache transport (cache outermost), so cache hits
// never consume rate-limit budget.
package ratelimit
