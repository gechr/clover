package forge_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/forge"
	"github.com/stretchr/testify/require"
)

// errUnreachable marks a transport path a test asserts is never taken.
var errUnreachable = errors.New("transport unexpectedly reached")

func TestRESTClientHTTPClient(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{}
	client := forge.NewRESTClient(httpClient, "application/json")
	require.Same(t, httpClient, client.HTTPClient())
}

func TestAuthorization(t *testing.T) {
	t.Parallel()

	require.Equal(t, "Bearer x", forge.Authorization("Bearer", "x"))
	require.Equal(t, "token abc", forge.Authorization("token", "abc"))
	require.Empty(t, forge.Authorization("Bearer", ""))
}

func TestBearer(t *testing.T) {
	t.Parallel()

	require.Equal(t, "Bearer x", forge.Bearer("x"))
	require.Empty(t, forge.Bearer(""))
}

func TestDoWithContextNoContent(t *testing.T) {
	t.Parallel()

	client := forge.NewRESTClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Status:     "204 No Content",
				Header:     http.Header{},
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}),
	}, "application/json")

	var out map[string]bool
	header, err := client.DoWithContext(t.Context(), "https://api.example.test/x", "", &out)
	require.NoError(t, err)
	require.Nil(t, out, "a 204 carries no body to decode")
	require.NotNil(t, header)
}

func TestDoWithContextNilResponse(t *testing.T) {
	t.Parallel()

	client := forge.NewRESTClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(`{"ignored":true}`)),
				Request:    req,
			}, nil
		}),
	}, "application/json")

	header, err := client.DoWithContext(t.Context(), "https://api.example.test/x", "", nil)
	require.NoError(t, err)
	require.NotNil(t, header, "a nil response target returns headers without decoding")
}

func TestDoWithContextErrorStatus(t *testing.T) {
	t.Parallel()

	client := forge.NewRESTClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Status:     "500 Internal Server Error",
				Body:       io.NopCloser(strings.NewReader("boom")),
				Request:    req,
			}, nil
		}),
	}, "application/json")

	var out map[string]bool
	_, err := client.DoWithContext(t.Context(), "https://api.example.test/x", "", &out)
	require.EqualError(t, err, "boom (500 Internal Server Error)")
}

func TestDoWithContextBadURL(t *testing.T) {
	t.Parallel()

	client := forge.NewRESTClient(&http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			t.Fatal("transport must not be reached for a malformed URL")
			return nil, errUnreachable
		}),
	}, "application/json")

	var out map[string]bool
	_, err := client.DoWithContext(t.Context(), "://bad", "", &out)
	require.Error(t, err, "a request-construction error precedes the transport")
}
