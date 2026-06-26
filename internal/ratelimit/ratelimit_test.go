package ratelimit_test

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gechr/clover/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

// githubHeaders is the X-RateLimit convention (epoch reset) used to exercise the
// generic policy; it mirrors what the github provider will configure.
var githubHeaders = ratelimit.Headers{
	Remaining:  "X-RateLimit-Remaining",
	Reset:      "X-RateLimit-Reset",
	ResetKind:  ratelimit.ResetEpoch,
	RetryAfter: "Retry-After",
}

// fakeBase returns a queued response per call and counts calls.
type fakeBase struct {
	calls     atomic.Int64
	responses []*http.Response
}

func (f *fakeBase) RoundTrip(*http.Request) (*http.Response, error) {
	n := f.calls.Add(1)
	idx := int(n - 1)
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}
	return f.responses[idx], nil
}

func response(status int, header http.Header) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

func rateHeader(remaining int, reset time.Time) http.Header {
	// Canonical keys, as Go stores real response headers and as Get looks them up.
	return http.Header{
		"X-Ratelimit-Remaining": {strconv.Itoa(remaining)},
		"X-Ratelimit-Reset":     {strconv.FormatInt(reset.Unix(), 10)},
	}
}

func newReq(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "https://example.test/a", nil)
	require.NoError(t, err)
	return req
}

func TestPassesThroughWithQuota(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_000_000, 0)
	base := &fakeBase{responses: []*http.Response{
		response(http.StatusOK, rateHeader(42, now.Add(time.Hour))),
	}}
	rt := ratelimit.New(base, githubHeaders, ratelimit.WithClock(func() time.Time { return now }))

	resp, err := rt.RoundTrip(newReq(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
}

func TestExhaustedResponseYieldsError(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_000_000, 0)
	reset := now.Add(time.Hour)
	base := &fakeBase{responses: []*http.Response{
		response(http.StatusForbidden, rateHeader(0, reset)),
	}}
	rt := ratelimit.New(base, githubHeaders, ratelimit.WithClock(func() time.Time { return now }))

	_, err := rt.RoundTrip(newReq(t))

	var rlErr *ratelimit.Error
	require.ErrorAs(t, err, &rlErr)
	require.Equal(t, reset, rlErr.Reset)
}

func TestShortCircuitsAfterExhaustion(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_000_000, 0)
	reset := now.Add(time.Hour)
	base := &fakeBase{responses: []*http.Response{
		response(http.StatusForbidden, rateHeader(0, reset)),
	}}
	rt := ratelimit.New(base, githubHeaders, ratelimit.WithClock(func() time.Time { return now }))

	_, err := rt.RoundTrip(newReq(t))
	require.Error(t, err)

	// Second request must not reach the base: the quota is known to be spent.
	_, err = rt.RoundTrip(newReq(t))
	require.Error(t, err)
	require.Equal(
		t,
		int64(1),
		base.calls.Load(),
		"exhausted quota short-circuits without a network call",
	)
}

func TestAllowsAgainAfterReset(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_000_000, 0)
	past := now.Add(-time.Hour)
	base := &fakeBase{responses: []*http.Response{
		response(http.StatusForbidden, rateHeader(0, past)),
		response(http.StatusOK, rateHeader(10, now.Add(time.Hour))),
	}}
	rt := ratelimit.New(base, githubHeaders, ratelimit.WithClock(func() time.Time { return now }))

	_, err := rt.RoundTrip(newReq(t))
	require.Error(t, err)

	// Reset is in the past, so the next request proceeds.
	resp, err := rt.RoundTrip(newReq(t))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, int64(2), base.calls.Load())
}

func TestRetryAfterSeconds(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_000_000, 0)
	base := &fakeBase{responses: []*http.Response{
		response(http.StatusTooManyRequests, http.Header{"Retry-After": {"60"}}),
	}}
	rt := ratelimit.New(base, githubHeaders, ratelimit.WithClock(func() time.Time { return now }))

	_, err := rt.RoundTrip(newReq(t))

	var rlErr *ratelimit.Error
	require.ErrorAs(t, err, &rlErr)
	require.Equal(t, now.Add(60*time.Second), rlErr.Reset)
}
