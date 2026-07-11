package httpcache

import (
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
)

// ErrOffline is the failure for a request offline mode cannot serve: nothing
// in the cache matches it. Callers see it wrapped with the request URL.
var ErrOffline = errors.New("offline and not cached")

// offline is the process-wide offline switch. Like the shared disk store it is
// set once by the command layer and read by every transport [New] built.
var offline atomic.Bool

// SetOffline switches every client built by [New] into (or out of) offline
// mode: requests are answered from the cache alone, stale entries included,
// and a miss fails with [ErrOffline] instead of touching the network.
func SetOffline(on bool) { offline.Store(on) }

// Offline reports whether offline mode is on.
func Offline() bool { return offline.Load() }

// offlineRoundTrip serves req from the store alone: any entry answers, however
// stale, and a miss is [ErrOffline]. Freshness and revalidation both need the
// origin, so neither applies offline.
func (t *Transport) offlineRoundTrip(req *http.Request) (*http.Response, error) {
	closeRequestBody(req)
	if key, keyed := fingerprint(req); keyed {
		if entry, found := t.store.Get(key); found {
			counters.hits.Add(1)
			return entry.response(req), nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrOffline, req.URL.Redacted())
}
