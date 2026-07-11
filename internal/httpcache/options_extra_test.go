package httpcache_test

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gechr/clover/internal/httpcache"
	"github.com/stretchr/testify/require"
)

// blockingTransport blocks until the request context is cancelled, so a client
// timeout is the only thing that can end the round trip.
type blockingTransport struct{}

func (blockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestWithTimeout(t *testing.T) {
	t.Parallel()

	client := httpcache.New(
		httpcache.WithTimeout(10*time.Millisecond),
		httpcache.WithTransport(blockingTransport{}),
	)

	_, err := client.Get("https://example.test/slow")
	require.Error(t, err)

	var netErr net.Error
	require.ErrorAs(t, err, &netErr)
	require.True(t, netErr.Timeout(), "the client timeout produces a timeout error")
}
