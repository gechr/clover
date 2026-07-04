package httpcache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	xhttp "github.com/gechr/x/http"
	xslices "github.com/gechr/x/slices"
	xstrings "github.com/gechr/x/strings"
	"golang.org/x/sync/singleflight"
)

var errTooLarge = errors.New("response body exceeds cache entry limit")

const (
	defaultTimeout = 30 * time.Second
	dialTimeout    = 5 * time.Second
	errorBackoff   = 5 * time.Second
	keepAlive      = 30 * time.Second
	maxEntryBytes  = 16 << 20 // 16 MiB
)

type config struct {
	base          http.RoundTripper
	store         Store
	timeout       time.Duration
	errorBackoff  time.Duration
	maxEntryBytes int64
}

// New returns an *http.Client whose transport caches and coalesces GET requests.
// Providers use the returned client like any other and remain unaware of the
// cache.
func New(opts ...Option) *http.Client {
	cfg := config{
		base:          newBaseTransport(),
		store:         defaultStore(),
		timeout:       defaultTimeout,
		errorBackoff:  errorBackoff,
		maxEntryBytes: maxEntryBytes,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &http.Client{
		Timeout: cfg.timeout,
		Transport: &Transport{
			base:          cfg.base,
			store:         cfg.store,
			errorBackoff:  cfg.errorBackoff,
			failures:      make(map[string]failure),
			maxEntryBytes: cfg.maxEntryBytes,
			startedAt:     time.Now(),
		},
	}
}

// defaultStore is the store New uses when no [WithStore] is given: the run's
// in-memory store, layered over the shared disk delegate. The disk tier is
// inert until [EnableSharedDisk] runs, so this is a plain in-memory cache for
// commands that never enable it.
func defaultStore() Store {
	return NewLayeredStore(NewMemStore(), sharedDiskStore{})
}

// Transport is the caching http.RoundTripper. Non-GET requests pass straight
// through; GET requests are served from the store while fresh, revalidated with
// a conditional request when stale but carrying a validator, and otherwise
// fetched - concurrent identical fetches share a single round trip, and a fetch
// error is replayed to further requests for the backoff window, so a dead host
// costs one connection timeout per window instead of one per caller.
type Transport struct {
	base          http.RoundTripper
	store         Store
	group         singleflight.Group
	failMu        sync.Mutex
	failures      map[string]failure
	errorBackoff  time.Duration
	maxEntryBytes int64
	startedAt     time.Time
}

// failure is a remembered fetch error, replayed until the backoff window ends.
type failure struct {
	err error
	at  time.Time
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	policy := policy(req.Context())
	if req.Method != http.MethodGet && !policy.cacheable {
		counters.requests.Add(1)
		return t.base.RoundTrip(req)
	}
	if noStore(req.Header) {
		counters.requests.Add(1)
		return t.base.RoundTrip(req)
	}

	key, keyed := fingerprint(req)
	if !keyed {
		counters.requests.Add(1)
		return t.base.RoundTrip(req)
	}
	if entry, found := t.store.Get(key); found && t.fresh(entry) {
		closeRequestBody(req)
		counters.hits.Add(1)
		return entry.response(req), nil
	}
	if err := t.recentFailure(key); err != nil {
		closeRequestBody(req)
		counters.replayed.Add(1)
		return nil, err
	}

	result, err, shared := t.group.Do(key, func() (any, error) {
		entry, found := t.store.Get(key)
		if found && t.fresh(entry) {
			return entry, nil
		}
		var stale *Entry
		if found && entry.revalidatable() {
			stale = entry
		}
		counters.requests.Add(1)
		resp, err := t.base.RoundTrip(conditionalRequest(req, stale))
		if err != nil {
			t.recordFailure(key, err)
			return nil, err
		}
		if stale != nil && resp.StatusCode == http.StatusNotModified {
			_ = resp.Body.Close()
			counters.revalidated.Add(1)
			refreshed := stale.refreshed(time.Now())
			t.store.Set(key, refreshed)
			return refreshed, nil
		}
		if tooLarge(resp, t.maxEntryBytes) {
			_ = resp.Body.Close()
			return nil, errTooLarge
		}
		defer func() { _ = resp.Body.Close() }()

		entry, err = newEntry(resp, t.maxEntryBytes)
		if err != nil {
			return nil, err
		}
		entry = entry.withFallbackFreshness(policy.fallbackFreshness)
		if cacheable(resp) {
			t.store.Set(key, entry)
		}
		return entry, nil
	})
	if shared {
		counters.coalesced.Add(1)
	}
	if err != nil {
		if errors.Is(err, errTooLarge) {
			counters.requests.Add(1)
			return t.base.RoundTrip(req)
		}
		return nil, err
	}

	entry, ok := result.(*Entry)
	if !ok {
		return t.base.RoundTrip(req) // unreachable; defensive
	}
	if shared {
		closeRequestBody(req)
	}
	return entry.response(req), nil
}

// fresh reports whether entry may be served without contacting the origin. An
// entry stored during this transport's lifetime is always fresh - a run is
// short, and this is the in-run contract providers rely on. Older entries
// (loaded from a cross-run store) are fresh only within their origin-granted
// lifetime.
func (t *Transport) fresh(entry *Entry) bool {
	now := time.Now()
	if !entry.StoredAt.Before(t.startedAt) {
		return true
	}
	return entry.fresh(now)
}

// conditionalRequest returns req with the stale entry's validators attached, so
// the origin can answer 304 Not Modified. A nil stale returns req unchanged.
func conditionalRequest(req *http.Request, stale *Entry) *http.Request {
	if stale == nil {
		return req
	}
	req = req.Clone(req.Context())
	if etag := stale.Header.Get("ETag"); etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if modified := stale.Header.Get("Last-Modified"); modified != "" {
		req.Header.Set("If-Modified-Since", modified)
	}
	return req
}

// recentFailure returns the error remembered for key when its backoff window is
// still open, and forgets expired failures.
func (t *Transport) recentFailure(key string) error {
	if t.errorBackoff <= 0 {
		return nil
	}
	t.failMu.Lock()
	defer t.failMu.Unlock()
	f, ok := t.failures[key]
	if !ok {
		return nil
	}
	if time.Since(f.at) >= t.errorBackoff {
		delete(t.failures, key)
		return nil
	}
	return f.err
}

// recordFailure records a fetch error for key so requests within the backoff
// window fail fast instead of redialing.
func (t *Transport) recordFailure(key string, err error) {
	if t.errorBackoff <= 0 {
		return
	}
	t.failMu.Lock()
	defer t.failMu.Unlock()
	t.failures[key] = failure{err: err, at: time.Now()}
}

// response reconstructs a fresh, independently-readable response from a cached
// entry, so every caller gets its own body and headers.
func (e *Entry) response(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode:    e.Status,
		Status:        xhttp.Status(e.Status),
		Header:        e.Header.Clone(),
		Body:          io.NopCloser(bytes.NewReader(e.Body)),
		ContentLength: int64(len(e.Body)),
		Request:       req,
	}
}

// newEntry buffers a bounded response body so it can be cached and replayed. The
// caller owns closing the original response body.
func newEntry(resp *http.Response, limit int64) (*Entry, error) {
	if limit <= 0 {
		return nil, errTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errTooLarge
	}
	return &Entry{
		Status:   resp.StatusCode,
		Header:   resp.Header.Clone(),
		Body:     body,
		StoredAt: time.Now(),
	}, nil
}

func tooLarge(resp *http.Response, limit int64) bool {
	return limit <= 0 || resp.ContentLength > limit
}

// cacheable reports whether a response may be stored: a 200, or a negative
// answer, that the origin did not mark no-store.
func cacheable(resp *http.Response) bool {
	if noStore(resp.Header) {
		return false
	}
	return resp.StatusCode == http.StatusOK || negative(resp.StatusCode)
}

// negative reports whether a status is a client error whose outcome is stable
// for the rest of the run - a missing release stays missing - so replaying it
// saves a request per repeated probe. A retryable status (408, 429, 5xx) is a
// transient failure a later attempt could clear.
func negative(status int) bool {
	if xhttp.IsRetryableStatus(status) {
		return false
	}
	return status >= http.StatusBadRequest && status < http.StatusInternalServerError
}

// noStore reports whether headers carry a Cache-Control no-store directive.
func noStore(header http.Header) bool {
	for _, value := range header.Values("Cache-Control") {
		directives := xstrings.SplitCSV(value)
		for i, directive := range directives {
			directives[i] = cacheDirectiveName(directive)
		}
		if xslices.ContainsFold(directives, "no-store") {
			return true
		}
	}
	return false
}

func cacheDirectiveName(directive string) string {
	name, _, _ := strings.Cut(directive, "=")
	return strings.TrimSpace(name)
}

// fingerprint derives the cache key from the parts of a request that change the
// response. Authorization is included because authenticated responses differ;
// hashing it keeps tokens out of any future on-disk keys.
func fingerprint(req *http.Request) (string, bool) {
	parts := []string{
		req.Method,
		req.URL.String(),
		req.Header.Get("Authorization"),
		req.Header.Get("Accept"),
	}
	body, ok := requestBody(req)
	if !ok {
		return "", false
	}
	if body != nil {
		parts = append(parts, req.Header.Get("Content-Type"))
	}
	hasher := sha256.New()
	_, _ = io.WriteString(hasher, strings.Join(parts, "\n"))
	if body != nil {
		_, _ = hasher.Write([]byte("\n"))
		_, _ = hasher.Write(body)
	}
	return hex.EncodeToString(hasher.Sum(nil)), true
}

func requestBody(req *http.Request) ([]byte, bool) {
	if req.Method == http.MethodGet || req.Body == nil {
		return nil, true
	}
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, false
		}
		defer func() { _ = body.Close() }()
		data, err := io.ReadAll(body)
		return data, err == nil
	}
	data, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, false
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(data))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	return data, true
}

func closeRequestBody(req *http.Request) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
}

// newBaseTransport clones the default transport but caps connection setup, so an
// unreachable host fails fast instead of hanging on the OS default.
func newBaseTransport() http.RoundTripper {
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: keepAlive}
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		t := base.Clone()
		t.DialContext = dialer.DialContext
		t.TLSHandshakeTimeout = dialTimeout
		return t
	}
	return &http.Transport{
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: dialTimeout,
	}
}
