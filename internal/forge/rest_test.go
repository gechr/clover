package forge_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/forge"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRESTClientSkipsRejectedAuth(t *testing.T) {
	t.Parallel()

	var got []string
	client := forge.NewRESTClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			got = append(got, req.Host+" "+req.Header.Get("Authorization"))
			if req.Header.Get("Authorization") != "" {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Status:     "401 Unauthorized",
					Body:       http.NoBody,
					Request:    req,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Request:    req,
			}, nil
		}),
	}, "application/json")

	var out map[string]bool
	_, err := client.DoWithContext(
		t.Context(), "https://api.example.test/page/1", "Bearer stale", &out,
	)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"ok": true}, out)

	out = nil
	_, err = client.DoWithContext(
		t.Context(), "https://api.example.test/page/2", "Bearer stale", &out,
	)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"ok": true}, out)
	require.Equal(t, []string{
		"api.example.test Bearer stale",
		"api.example.test ",
		"api.example.test ",
	}, got)
}

func TestRESTClientRejectedAuthIsOriginScoped(t *testing.T) {
	t.Parallel()

	var got []string
	client := forge.NewRESTClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			got = append(got, req.Host+" "+req.Header.Get("Authorization"))
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Status:     "401 Unauthorized",
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}),
	}, "application/json")

	var out map[string]bool
	_, err := client.DoWithContext(
		t.Context(), "https://one.example.test/page", "Bearer stale", &out,
	)
	require.EqualError(t, err, " (401 Unauthorized)")

	_, err = client.DoWithContext(
		t.Context(), "https://two.example.test/page", "Bearer stale", &out,
	)
	require.EqualError(t, err, " (401 Unauthorized)")
	require.Equal(t, []string{
		"one.example.test Bearer stale",
		"one.example.test ",
		"two.example.test Bearer stale",
		"two.example.test ",
	}, got)
}
