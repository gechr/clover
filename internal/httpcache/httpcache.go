package httpcache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

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
		store:         NewMemStore(),
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
		},
	}
}

// Transport is the caching http.RoundTripper. Non-GET requests pass straight
// through; GET requests are served from the store when present, and otherwise
// fetched once - concurrent identical fetches share a single round trip, and a
// fetch error is replayed to further requests for the backoff window, so a dead
// host costs one connection timeout per window instead of one per caller.
type Transport struct {
	base          http.RoundTripper
	store         Store
	group         singleflight.Group
	failMu        sync.Mutex
	failures      map[string]failure
	errorBackoff  time.Duration
	maxEntryBytes int64
}

// failure is a remembered fetch error, replayed until the backoff window ends.
type failure struct {
	err error
	at  time.Time
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return t.base.RoundTrip(req)
	}
	if noStore(req.Header) {
		return t.base.RoundTrip(req)
	}

	key := fingerprint(req)
	if entry, ok := t.store.Get(key); ok {
		return entry.response(req), nil
	}
	if err := t.recentFailure(key); err != nil {
		return nil, err
	}

	result, err, _ := t.group.Do(key, func() (any, error) {
		if entry, ok := t.store.Get(key); ok {
			return entry, nil
		}
		resp, err := t.base.RoundTrip(req)
		if err != nil {
			t.recordFailure(key, err)
			return nil, err
		}
		if tooLarge(resp, t.maxEntryBytes) {
			_ = resp.Body.Close()
			return nil, errTooLarge
		}
		defer func() { _ = resp.Body.Close() }()

		entry, err := newEntry(resp, t.maxEntryBytes)
		if err != nil {
			return nil, err
		}
		if cacheable(resp) {
			t.store.Set(key, entry)
		}
		return entry, nil
	})
	if err != nil {
		if errors.Is(err, errTooLarge) {
			return t.base.RoundTrip(req)
		}
		return nil, err
	}

	entry, ok := result.(*Entry)
	if !ok {
		return t.base.RoundTrip(req) // unreachable; defensive
	}
	return entry.response(req), nil
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
		Status:        strconv.Itoa(e.Status) + " " + http.StatusText(e.Status),
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

// cacheable reports whether a response may be stored: a 200 that the origin did
// not mark no-store.
func cacheable(resp *http.Response) bool {
	if resp.StatusCode != http.StatusOK {
		return false
	}
	return !noStore(resp.Header)
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
func fingerprint(req *http.Request) string {
	raw := strings.Join([]string{
		req.Method,
		req.URL.String(),
		req.Header.Get("Authorization"),
		req.Header.Get("Accept"),
	}, "\n")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
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
