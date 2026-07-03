package httpcache

import (
	"net/http"
	"time"
)

// Option configures the client returned by [New].
type Option func(*config)

// WithMaxEntryBytes sets the largest response body the cache will buffer. A
// non-positive value disables response-body caching.
func WithMaxEntryBytes(n int64) Option { return func(c *config) { c.maxEntryBytes = n } }

// WithStore sets the cache backend (default: an in-memory [MemStore]). This is
// the seam for a disk-backed, cross-run store.
func WithStore(s Store) Option { return func(c *config) { c.store = s } }

// WithTimeout sets the client's total per-request timeout.
func WithTimeout(d time.Duration) Option { return func(c *config) { c.timeout = d } }

// WithTransport sets the underlying transport the cache wraps on a miss. Compose
// a rate-limit-aware transport here so cache hits never consume rate limit.
func WithTransport(rt http.RoundTripper) Option { return func(c *config) { c.base = rt } }
