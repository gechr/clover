package httpcache_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gechr/clover/internal/httpcache"
	"github.com/stretchr/testify/require"
)

// fakeTransport is a base RoundTripper that counts calls and returns a canned
// response, so the tests exercise caching without any real network.
type fakeTransport struct {
	calls   atomic.Int64
	status  int
	body    string
	header  http.Header
	delay   time.Duration
	err     error
	handler func(*http.Request) (*http.Response, error)
}

func (f *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.handler != nil {
		return f.handler(req)
	}
	if f.err != nil {
		return nil, f.err
	}
	header := http.Header{}
	if f.header != nil {
		header = f.header.Clone()
	}
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Request:    req,
	}, nil
}

func get(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	resp, err := client.Get(url)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body)
}

func TestCachesRepeatedGET(t *testing.T) {
	t.Parallel()

	fake := &fakeTransport{body: "v1.2.3"}
	client := httpcache.New(httpcache.WithTransport(fake))

	require.Equal(t, "v1.2.3", get(t, client, "https://example.test/a"))
	require.Equal(t, "v1.2.3", get(t, client, "https://example.test/a"))
	require.Equal(t, int64(1), fake.calls.Load(), "second GET should be served from cache")
}

func TestDistinctURLsAuthAndAcceptAreSeparate(t *testing.T) {
	t.Parallel()

	fake := &fakeTransport{body: "x"}
	client := httpcache.New(httpcache.WithTransport(fake))

	get(t, client, "https://example.test/a")
	get(t, client, "https://example.test/b")
	require.Equal(t, int64(2), fake.calls.Load(), "different URLs are cached separately")

	// Same URL, different Authorization → distinct cache entries.
	do := func(token string) {
		req, err := http.NewRequest(http.MethodGet, "https://example.test/a", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", token)
		resp, err := client.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	do("Bearer one")
	do("Bearer two")
	require.Equal(t, int64(4), fake.calls.Load(), "auth is part of the cache key")

	accept := func(value string) {
		req, err := http.NewRequest(http.MethodGet, "https://example.test/a", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", value)
		resp, err := client.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	accept("application/json")
	accept("application/vnd.oci.image.index.v1+json")
	require.Equal(t, int64(6), fake.calls.Load(), "accept is part of the cache key")
}

func TestNonGETNotCached(t *testing.T) {
	t.Parallel()

	fake := &fakeTransport{body: "x"}
	client := httpcache.New(httpcache.WithTransport(fake))

	post := func() {
		resp, err := client.Post("https://example.test/a", "text/plain", strings.NewReader("body"))
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	post()
	post()
	require.Equal(t, int64(2), fake.calls.Load(), "non-GET requests are never cached")
}

func TestCacheablePOSTsAreCachedByBody(t *testing.T) {
	t.Parallel()

	fake := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(string(body))),
			Request:    req,
		}, nil
	}}
	client := httpcache.New(httpcache.WithTransport(fake))

	post := func(body string) string {
		req, err := http.NewRequestWithContext(
			httpcache.WithCacheableRequest(context.Background(), time.Minute),
			http.MethodPost,
			"https://example.test/graphql",
			strings.NewReader(body),
		)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return string(data)
	}

	require.Equal(t, "query one", post("query one"))
	require.Equal(t, "query one", post("query one"))
	require.Equal(t, "query two", post("query two"))
	require.Equal(t, int64(2), fake.calls.Load(), "the request body is part of the cache key")
}

func TestCacheablePOSTFallbackFreshnessWorksAcrossRuns(t *testing.T) {
	t.Parallel()

	store := httpcache.NewMemStore()
	fake := &fakeTransport{body: "cached"}
	client := httpcache.New(httpcache.WithStore(store), httpcache.WithTransport(fake))

	req, err := http.NewRequestWithContext(
		httpcache.WithCacheableRequest(context.Background(), time.Minute),
		http.MethodPost,
		"https://example.test/graphql",
		strings.NewReader(`{"query":"QUERY"}`),
	)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, int64(1), fake.calls.Load())

	time.Sleep(10 * time.Millisecond)
	nextFake := &fakeTransport{body: "unused"}
	nextClient := httpcache.New(httpcache.WithStore(store), httpcache.WithTransport(nextFake))
	req, err = http.NewRequestWithContext(
		httpcache.WithCacheableRequest(context.Background(), time.Minute),
		http.MethodPost,
		"https://example.test/graphql",
		strings.NewReader(`{"query":"QUERY"}`),
	)
	require.NoError(t, err)
	resp, err = nextClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, int64(0), nextFake.calls.Load(), "fresh cacheable POST hits across runs")
}

func TestRetryableStatusNotCached(t *testing.T) {
	t.Parallel()

	for _, status := range []int{
		http.StatusRequestTimeout,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
	} {
		fake := &fakeTransport{status: status, body: "boom"}
		client := httpcache.New(httpcache.WithTransport(fake))

		get(t, client, "https://example.test/a")
		get(t, client, "https://example.test/a")
		require.Equal(t, int64(2), fake.calls.Load(),
			"a transient failure status is fetched every time")
	}
}

func TestNegativeStatusCachedForRun(t *testing.T) {
	t.Parallel()

	fake := &fakeTransport{status: http.StatusNotFound, body: "missing"}
	client := httpcache.New(httpcache.WithTransport(fake))

	do := func() int {
		resp, err := client.Get("https://example.test/a")
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		return resp.StatusCode
	}
	require.Equal(t, http.StatusNotFound, do())
	require.Equal(t, http.StatusNotFound, do())
	require.Equal(t, int64(1), fake.calls.Load(),
		"a repeated probe replays the cached negative answer")
}

func TestNegativeNoStoreNotCached(t *testing.T) {
	t.Parallel()

	fake := &fakeTransport{
		status: http.StatusNotFound,
		body:   "missing",
		header: http.Header{"Cache-Control": {"no-store"}},
	}
	client := httpcache.New(httpcache.WithTransport(fake))

	get(t, client, "https://example.test/a")
	get(t, client, "https://example.test/a")
	require.Equal(t, int64(2), fake.calls.Load(), "no-store wins over negative caching")
}

func TestNoStoreNotCached(t *testing.T) {
	t.Parallel()

	fake := &fakeTransport{body: "x", header: http.Header{"Cache-Control": {"private, no-store"}}}
	client := httpcache.New(httpcache.WithTransport(fake))

	get(t, client, "https://example.test/a")
	get(t, client, "https://example.test/a")
	require.Equal(t, int64(2), fake.calls.Load(), "no-store responses are not cached")
}

func TestRequestNoStoreBypassesCache(t *testing.T) {
	t.Parallel()

	fake := &fakeTransport{body: "x"}
	client := httpcache.New(httpcache.WithTransport(fake))

	do := func() {
		req, err := http.NewRequest(http.MethodGet, "https://example.test/a", nil)
		require.NoError(t, err)
		req.Header.Set("Cache-Control", "no-store")
		resp, err := client.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	do()
	do()
	require.Equal(t, int64(2), fake.calls.Load(), "request no-store bypasses cache")
}

func TestNoStoreIsExactDirective(t *testing.T) {
	t.Parallel()

	fake := &fakeTransport{body: "x", header: http.Header{"Cache-Control": {"x-no-store"}}}
	client := httpcache.New(httpcache.WithTransport(fake))

	get(t, client, "https://example.test/a")
	get(t, client, "https://example.test/a")
	require.Equal(t, int64(1), fake.calls.Load(), "only an exact no-store directive bypasses cache")
}

func TestOversizeBodyNotCached(t *testing.T) {
	t.Parallel()

	fake := &fakeTransport{body: "larger than the limit"}
	client := httpcache.New(
		httpcache.WithMaxEntryBytes(5),
		httpcache.WithTransport(fake),
	)

	require.Equal(t, "larger than the limit", get(t, client, "https://example.test/a"))
	require.Equal(t, "larger than the limit", get(t, client, "https://example.test/a"))
	require.Equal(t, int64(4), fake.calls.Load(), "oversize responses pass through without caching")
}

func TestErrorBackoffReplaysFailure(t *testing.T) {
	t.Parallel()

	fake := &fakeTransport{err: errors.New("dial tcp: connection refused")}
	client := httpcache.New(httpcache.WithTransport(fake))

	_, err := client.Get("https://example.test/a")
	require.EqualError(t, err, `Get "https://example.test/a": dial tcp: connection refused`)
	_, err = client.Get("https://example.test/a")
	require.EqualError(t, err, `Get "https://example.test/a": dial tcp: connection refused`)
	require.Equal(t, int64(1), fake.calls.Load(), "second GET replays the remembered error")

	// A different key is unaffected by the remembered failure.
	_, err = client.Get("https://example.test/b")
	require.EqualError(t, err, `Get "https://example.test/b": dial tcp: connection refused`)
	require.Equal(t, int64(2), fake.calls.Load(), "backoff is per key")
}

func TestErrorBackoffExpires(t *testing.T) {
	t.Parallel()

	fake := &fakeTransport{err: errors.New("boom")}
	client := httpcache.New(
		httpcache.WithErrorBackoff(time.Millisecond),
		httpcache.WithTransport(fake),
	)

	_, err := client.Get("https://example.test/a")
	require.EqualError(t, err, `Get "https://example.test/a": boom`)
	time.Sleep(5 * time.Millisecond)
	_, err = client.Get("https://example.test/a")
	require.EqualError(t, err, `Get "https://example.test/a": boom`)
	require.Equal(t, int64(2), fake.calls.Load(), "an expired backoff retries the fetch")
}

func TestErrorBackoffDisabled(t *testing.T) {
	t.Parallel()

	fake := &fakeTransport{err: errors.New("boom")}
	client := httpcache.New(
		httpcache.WithErrorBackoff(0),
		httpcache.WithTransport(fake),
	)

	_, err := client.Get("https://example.test/a")
	require.EqualError(t, err, `Get "https://example.test/a": boom`)
	_, err = client.Get("https://example.test/a")
	require.EqualError(t, err, `Get "https://example.test/a": boom`)
	require.Equal(t, int64(2), fake.calls.Load(), "a disabled backoff retries every fetch")
}

// crossRunStore populates a store through one client and returns it, so a
// second client sees the entries as predating its own run - the same shape as
// entries loaded from a cross-run (disk) store.
func crossRunStore(t *testing.T, header http.Header, body string) *httpcache.MemStore {
	t.Helper()
	store := httpcache.NewMemStore()
	fake := &fakeTransport{body: body, header: header}
	client := httpcache.New(httpcache.WithStore(store), httpcache.WithTransport(fake))
	get(t, client, "https://example.test/a")
	// Guarantee the stored entry strictly predates the next client's start.
	time.Sleep(10 * time.Millisecond)
	return store
}

func TestCrossRunFreshEntryBypassesTransport(t *testing.T) {
	t.Parallel()

	header := http.Header{
		"Cache-Control": {"public, max-age=300"},
		"Etag":          {`W/"v1"`},
	}
	store := crossRunStore(t, header, "cached")
	fake := &fakeTransport{body: "unused"}
	client := httpcache.New(httpcache.WithStore(store), httpcache.WithTransport(fake))

	require.Equal(t, "cached", get(t, client, "https://example.test/a"))
	// The rate-limit invariant: a fresh hit never reaches the base transport,
	// so a rate-limit-aware transport composed there spends no quota.
	require.Equal(t, int64(0), fake.calls.Load(), "fresh cross-run entries make no round trip")
}

func TestStaleEntryRevalidatedNotModified(t *testing.T) {
	t.Parallel()

	store := crossRunStore(t, http.Header{"Etag": {`W/"v1"`}}, "cached")
	var conditional []*http.Request
	fake := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		conditional = append(conditional, req)
		return &http.Response{
			StatusCode: http.StatusNotModified,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	}}
	client := httpcache.New(httpcache.WithStore(store), httpcache.WithTransport(fake))

	require.Equal(t, "cached", get(t, client, "https://example.test/a"))
	require.Equal(
		t,
		int64(1),
		fake.calls.Load(),
		"a stale entry revalidates through the base transport",
	)
	require.Len(t, conditional, 1)
	require.Equal(t, `W/"v1"`, conditional[0].Header.Get("If-None-Match"))

	// The 304 reset the entry's store time, so further requests are in-run hits.
	require.Equal(t, "cached", get(t, client, "https://example.test/a"))
	require.Equal(
		t,
		int64(1),
		fake.calls.Load(),
		"a revalidated entry is fresh for the rest of the run",
	)
}

func TestStaleEntryRevalidatedLastModified(t *testing.T) {
	t.Parallel()

	const modified = "Wed, 24 Jun 2026 23:36:26 GMT"
	store := crossRunStore(t, http.Header{"Last-Modified": {modified}}, "cached")
	var conditional []*http.Request
	fake := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		conditional = append(conditional, req)
		return &http.Response{
			StatusCode: http.StatusNotModified,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	}}
	client := httpcache.New(httpcache.WithStore(store), httpcache.WithTransport(fake))

	require.Equal(t, "cached", get(t, client, "https://example.test/a"))
	require.Len(t, conditional, 1)
	require.Equal(t, modified, conditional[0].Header.Get("If-Modified-Since"))
}

func TestStaleEntryReplacedOnOK(t *testing.T) {
	t.Parallel()

	store := crossRunStore(t, http.Header{"Etag": {`W/"v1"`}}, "old")
	fake := &fakeTransport{body: "new", header: http.Header{"Etag": {`W/"v2"`}}}
	client := httpcache.New(httpcache.WithStore(store), httpcache.WithTransport(fake))

	require.Equal(t, "new", get(t, client, "https://example.test/a"))
	require.Equal(t, "new", get(t, client, "https://example.test/a"))
	require.Equal(t, int64(1), fake.calls.Load(), "a 200 replaces the stale entry")
}

func TestStaleEntryWithoutValidatorRefetched(t *testing.T) {
	t.Parallel()

	store := crossRunStore(t, http.Header{}, "old")
	var requests []*http.Request
	fake := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("new")),
			Request:    req,
		}, nil
	}}
	client := httpcache.New(httpcache.WithStore(store), httpcache.WithTransport(fake))

	require.Equal(t, "new", get(t, client, "https://example.test/a"))
	require.Len(t, requests, 1)
	require.Empty(t, requests[0].Header.Get("If-None-Match"))
	require.Empty(t, requests[0].Header.Get("If-Modified-Since"))
}

func TestStaleEntryRevalidationErrorPropagates(t *testing.T) {
	t.Parallel()

	store := crossRunStore(t, http.Header{"Etag": {`W/"v1"`}}, "cached")
	fake := &fakeTransport{err: errors.New("boom")}
	client := httpcache.New(httpcache.WithStore(store), httpcache.WithTransport(fake))

	_, err := client.Get("https://example.test/a")
	require.EqualError(t, err, `Get "https://example.test/a": boom`)
}

func TestSnapshotCountsActivity(t *testing.T) { //nolint:paralleltest // reads process-wide counters
	before := httpcache.Snapshot()

	fake := &fakeTransport{body: "x"}
	client := httpcache.New(httpcache.WithTransport(fake))
	get(t, client, "https://stats.test/a")
	get(t, client, "https://stats.test/a")

	after := httpcache.Snapshot()
	require.Equal(t, int64(1), after.Requests-before.Requests, "one lookup reached the network")
	require.Equal(t, int64(1), after.Hits-before.Hits, "one was served from cache")
	require.Equal(t, before.Revalidated, after.Revalidated)
	require.Equal(t, before.Replayed, after.Replayed)
}

func TestCoalescesConcurrentGETs(t *testing.T) {
	t.Parallel()

	fake := &fakeTransport{body: "v1", delay: 50 * time.Millisecond}
	client := httpcache.New(httpcache.WithTransport(fake))

	const callers = 20
	bodies := make([]string, callers)
	errs := make([]error, callers)

	var wg sync.WaitGroup
	wg.Add(callers)
	for i := range callers {
		go func() {
			defer wg.Done()
			resp, err := client.Get("https://example.test/a")
			if err != nil {
				errs[i] = err
				return
			}
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(resp.Body)
			errs[i], bodies[i] = err, string(body)
		}()
	}
	wg.Wait()

	// Assertions run on the test goroutine, after the workers finish.
	for i := range callers {
		require.NoError(t, errs[i])
		require.Equal(t, "v1", bodies[i])
	}
	require.Equal(
		t,
		int64(1),
		fake.calls.Load(),
		"concurrent identical GETs collapse to one round trip",
	)
}
