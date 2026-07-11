package httpcache_test

import (
	"net/http"
	"testing"

	"github.com/gechr/clover/internal/httpcache"
	"github.com/stretchr/testify/require"
)

// TestOfflineServesCacheAndFailsMisses flips the process into offline mode and
// confirms a cached response answers without a round trip while an uncached
// URL fails with ErrOffline. Not parallel: the offline switch is process-wide.
func TestOfflineServesCacheAndFailsMisses(t *testing.T) {
	fake := &fakeTransport{body: "v1.2.3"}
	client := httpcache.New(httpcache.WithTransport(fake))

	// Prime the cache online, then go offline.
	require.Equal(t, "v1.2.3", get(t, client, "https://example.test/cached"))
	require.Equal(t, int64(1), fake.calls.Load())

	httpcache.SetOffline(true)
	t.Cleanup(func() { httpcache.SetOffline(false) })

	require.Equal(t, "v1.2.3", get(t, client, "https://example.test/cached"))
	require.Equal(t, int64(1), fake.calls.Load(), "offline hit must not touch the network")

	_, err := client.Get("https://example.test/missing")
	require.ErrorIs(t, err, httpcache.ErrOffline)
	require.Equal(t, int64(1), fake.calls.Load(), "offline miss must not touch the network")
}

// TestOfflineCoversNonGET confirms offline mode also gates requests the cache
// path would otherwise pass straight to the network (a POST without a caching
// policy), so no request shape can slip online.
func TestOfflineCoversNonGET(t *testing.T) {
	fake := &fakeTransport{body: "ok"}
	client := httpcache.New(httpcache.WithTransport(fake))

	httpcache.SetOffline(true)
	t.Cleanup(func() { httpcache.SetOffline(false) })

	resp, err := client.Post("https://example.test/graphql", "application/json", http.NoBody)
	if resp != nil {
		t.Cleanup(func() { _ = resp.Body.Close() })
	}
	require.ErrorIs(t, err, httpcache.ErrOffline)
	require.Zero(t, fake.calls.Load())
}
