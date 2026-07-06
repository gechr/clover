// Package httpcache provides a caching http.RoundTripper that sits beneath the
// providers: they use an ordinary *http.Client and never manage caching
// themselves. Cacheable GET responses are memoised for the run and concurrent
// identical requests are coalesced, so a hundred markers pointing at the same
// upstream cause a single round trip. Cache hits make no network call at all,
// which is also the most effective way to stay within an API's rate limit.
//
// The cache is keyed by method, URL, and Authorization, and stores full
// responses including their validators (ETag, Last-Modified), leaving room for a
// disk-backed Store and conditional revalidation behind the same seam.
//
// The RFC 9111 semantics are implemented selectively, scoped to a short-lived
// CLI talking to known APIs: no-store is honored, but the no-cache and
// must-revalidate directives and the Age header are deliberately ignored, and
// Vary is subsumed by keying on the headers that matter here (Authorization,
// Accept).
package httpcache
