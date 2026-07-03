package ratelimit

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ResetKind says how a reset header value is interpreted.
type ResetKind int

const (
	ResetEpoch ResetKind = iota // Unix seconds (e.g. GitHub X-RateLimit-Reset)
	ResetDelta                  // seconds from now (e.g. the IETF draft RateLimit-Reset)
)

// Headers names the rate-limit headers for a provider. The parsing logic is the
// same everywhere; only the names - and whether the reset is an absolute epoch
// or a delta - differ, so a provider configures those and nothing more. An empty
// RetryAfter disables Retry-After handling.
type Headers struct {
	Remaining  string
	Reset      string
	ResetKind  ResetKind
	RetryAfter string
}

// Error reports that a request was refused because the rate limit is exhausted.
// Reset is when the limit is expected to replenish (zero if unknown).
type Error struct {
	Reset time.Time
}

func (e *Error) Error() string {
	if e.Reset.IsZero() {
		return "rate limit exceeded"
	}
	return "rate limit exceeded; resets at " + e.Reset.UTC().Format(time.RFC3339)
}

// Option configures a [Transport].
type Option func(*Transport)

// WithClock overrides the time source, for tests.
func WithClock(now func() time.Time) Option {
	return func(t *Transport) { t.now = now }
}

// Transport is a rate-limit-aware http.RoundTripper. It reads the configured
// [Headers] from each response and, once the quota is spent, refuses further
// requests until the reset time rather than hammering the limit.
type Transport struct {
	base    http.RoundTripper
	headers Headers
	now     func() time.Time

	mu        sync.Mutex
	reset     time.Time
	remaining int
	known     bool
}

// New wraps base with rate-limit awareness using the given header names. A nil
// base uses http.DefaultTransport.
func New(base http.RoundTripper, headers Headers, opts ...Option) *Transport {
	if base == nil {
		base = http.DefaultTransport
	}
	t := &Transport{base: base, headers: headers, now: time.Now}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// RoundTrip implements http.RoundTripper. It refuses a request up front when the
// quota is known to be spent, otherwise forwards it, records the limit state the
// response reports, and converts a rate-limited response into an [Error].
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if reset, blocked := t.blocked(); blocked {
		return nil, &Error{Reset: reset}
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	t.observe(resp)
	if reset, limited := t.limited(resp); limited {
		_ = resp.Body.Close()
		return nil, &Error{Reset: reset}
	}
	return resp, nil
}

// blocked reports whether the quota is spent and not yet reset.
func (t *Transport) blocked() (time.Time, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.known && t.remaining <= 0 && t.now().Before(t.reset) {
		return t.reset, true
	}
	return time.Time{}, false
}

// observe records the remaining/reset a response reports, if present.
func (t *Transport) observe(resp *http.Response) {
	remaining, okRemaining := atoi(resp.Header.Get(t.headers.Remaining))
	reset, okReset := t.parseReset(resp.Header.Get(t.headers.Reset))
	if !okRemaining || !okReset {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.remaining, t.reset, t.known = remaining, reset, true
}

// limited reports whether resp is a rate-limit rejection and when it resets. A
// 403 or 429 counts as limited when it carries Retry-After or reports zero
// remaining.
func (t *Transport) limited(resp *http.Response) (time.Time, bool) {
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
		return time.Time{}, false
	}
	if t.headers.RetryAfter != "" {
		if reset, ok := t.retryAfter(resp.Header.Get(t.headers.RetryAfter)); ok {
			return reset, true
		}
	}
	if remaining, ok := atoi(resp.Header.Get(t.headers.Remaining)); ok && remaining <= 0 {
		reset, _ := t.parseReset(resp.Header.Get(t.headers.Reset))
		return reset, true
	}
	return time.Time{}, false
}

// parseReset interprets a reset header value per the configured [ResetKind].
func (t *Transport) parseReset(value string) (time.Time, bool) {
	n, ok := atoi(value)
	if !ok {
		return time.Time{}, false
	}
	switch t.headers.ResetKind {
	case ResetDelta:
		return t.now().Add(time.Duration(n) * time.Second), true
	case ResetEpoch:
		return time.Unix(int64(n), 0), true
	}
	return time.Time{}, false
}

// retryAfter interprets a Retry-After value: either a number of seconds or an
// HTTP date.
func (t *Transport) retryAfter(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	if seconds, ok := atoi(value); ok {
		return t.now().Add(time.Duration(seconds) * time.Second), true
	}
	if when, err := http.ParseTime(value); err == nil {
		return when, true
	}
	return time.Time{}, false
}

// atoi parses a base-10 integer header, reporting whether it was present and
// valid.
func atoi(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	value, _, _ = strings.Cut(value, ";")
	value = strings.TrimSpace(value)
	n, err := strconv.Atoi(value)
	return n, err == nil
}
