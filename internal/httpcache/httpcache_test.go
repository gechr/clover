package httpcache_test

import (
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
	calls  atomic.Int64
	status int
	body   string
	header http.Header
	delay  time.Duration
}

func (f *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	if f.delay > 0 {
		time.Sleep(f.delay)
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

func TestDistinctURLsAndAuthAreSeparate(t *testing.T) {
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

func TestNonOKNotCached(t *testing.T) {
	t.Parallel()

	fake := &fakeTransport{status: http.StatusInternalServerError, body: "boom"}
	client := httpcache.New(httpcache.WithTransport(fake))

	get(t, client, "https://example.test/a")
	get(t, client, "https://example.test/a")
	require.Equal(t, int64(2), fake.calls.Load(), "only 200 responses are cached")
}

func TestNoStoreNotCached(t *testing.T) {
	t.Parallel()

	fake := &fakeTransport{body: "x", header: http.Header{"Cache-Control": {"no-store"}}}
	client := httpcache.New(httpcache.WithTransport(fake))

	get(t, client, "https://example.test/a")
	get(t, client, "https://example.test/a")
	require.Equal(t, int64(2), fake.calls.Load(), "no-store responses are not cached")
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
