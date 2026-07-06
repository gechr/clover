package httpcache

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	xstrings "github.com/gechr/x/strings"
)

// fresh reports whether the entry is still within its origin-granted freshness
// lifetime, so it may be served without contacting the origin.
func (e *Entry) fresh(now time.Time) bool {
	if !e.FreshUntil.IsZero() {
		return now.Before(e.FreshUntil)
	}
	ttl, ok := lifetime(e.Header)
	return ok && now.Sub(e.StoredAt) < ttl
}

// refreshed returns a copy of the entry with its store time reset and the 304
// response's header fields folded in (RFC 9111 §3.2) - the origin may grant new
// freshness or a new validator alongside a 304. Content-Length is excepted, as
// the stored body is unchanged. The Body is shared - entries are immutable.
func (e *Entry) refreshed(now time.Time, update http.Header) *Entry {
	clone := *e
	clone.StoredAt = now
	clone.Header = e.Header.Clone()
	for name, values := range update {
		if http.CanonicalHeaderKey(name) == "Content-Length" {
			continue
		}
		clone.Header[http.CanonicalHeaderKey(name)] = values
	}
	return &clone
}

// revalidatable reports whether the entry carries a validator (ETag or
// Last-Modified) usable for a conditional request.
func (e *Entry) revalidatable() bool {
	return e.Header.Get("ETag") != "" || e.Header.Get("Last-Modified") != ""
}

func (e *Entry) withFallbackFreshness(ttl time.Duration) *Entry {
	if ttl <= 0 || e.revalidatable() {
		return e
	}
	if _, ok := lifetime(e.Header); ok {
		return e
	}
	clone := *e
	clone.FreshUntil = clone.StoredAt.Add(ttl)
	return &clone
}

// lifetime returns the positive freshness lifetime the origin granted:
// Cache-Control max-age when present, otherwise Expires minus Date. A response
// granting no (or zero) freshness reports ok as false.
func lifetime(header http.Header) (time.Duration, bool) {
	if d, ok := maxAge(header); ok {
		return d, true
	}
	return expiresLifetime(header)
}

// maxAge returns a positive Cache-Control max-age directive as a duration.
func maxAge(header http.Header) (time.Duration, bool) {
	for _, value := range header.Values("Cache-Control") {
		for _, directive := range xstrings.SplitCSV(value) {
			name, arg, ok := strings.Cut(directive, "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(name), "max-age") {
				continue
			}
			secs, err := strconv.Atoi(strings.TrimSpace(arg))
			if err != nil || secs <= 0 {
				continue
			}
			return time.Duration(secs) * time.Second, true
		}
	}
	return 0, false
}

// expiresLifetime returns a positive Expires minus Date lifetime. Both headers
// must parse - without a Date there is no origin clock to measure against.
func expiresLifetime(header http.Header) (time.Duration, bool) {
	expires, err := http.ParseTime(header.Get("Expires"))
	if err != nil {
		return 0, false
	}
	date, err := http.ParseTime(header.Get("Date"))
	if err != nil {
		return 0, false
	}
	if d := expires.Sub(date); d > 0 {
		return d, true
	}
	return 0, false
}
