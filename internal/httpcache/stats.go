package httpcache

import "sync/atomic"

// Stats is a snapshot of transport activity: requests that reached the network,
// cache hits served without one, conditional revalidations answered 304,
// singleflight-coalesced callers, and failures replayed by the error backoff.
type Stats struct {
	Requests    int64
	Hits        int64
	Revalidated int64
	Coalesced   int64
	Replayed    int64
}

// counters accumulates process-wide: every Transport increments the same set,
// so the run summary reports one total without threading a handle through
// providers - mirroring the shared disk store.
var counters struct {
	requests    atomic.Int64
	hits        atomic.Int64
	revalidated atomic.Int64
	coalesced   atomic.Int64
	replayed    atomic.Int64
}

// Snapshot returns the process-wide transport totals so far.
func Snapshot() Stats {
	return Stats{
		Requests:    counters.requests.Load(),
		Hits:        counters.hits.Load(),
		Revalidated: counters.revalidated.Load(),
		Coalesced:   counters.coalesced.Load(),
		Replayed:    counters.replayed.Load(),
	}
}
