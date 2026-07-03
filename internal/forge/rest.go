package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// RESTClient issues a forge's REST GETs over a shared transport. It holds no
// host or credential: each call carries its own absolute URL and its own
// Authorization value, since both the host and the credential are per-marker
// values.
type RESTClient struct {
	httpClient *http.Client
	accept     string
}

// NewRESTClient returns a client issuing GETs through httpClient, sending
// accept as the Accept header on every request.
func NewRESTClient(httpClient *http.Client, accept string) RESTClient {
	return RESTClient{httpClient: httpClient, accept: accept}
}

// HTTPClient returns the underlying client, for requests that go beyond the
// GET-and-decode surface (e.g. an OAuth token refresh POST) but must share the
// same transport.
func (c RESTClient) HTTPClient() *http.Client {
	return c.httpClient
}

// Authorization joins an auth scheme and token into an Authorization header
// value, or "" when the token is empty.
func Authorization(scheme, token string) string {
	if token == "" {
		return ""
	}
	return scheme + " " + token
}

// Bearer returns the Authorization value attaching token as a Bearer
// credential, or "" when the token is empty.
func Bearer(token string) string {
	return Authorization("Bearer", token)
}

// DoWithContext issues a GET against the absolute url, attaching authorization
// (a full header value, e.g. "Bearer <token>") when non-empty, fails on a
// non-2xx status, decodes the JSON body into response when non-nil, and returns
// the response headers (for pagination). A stored credential that has expired
// or been revoked would 401 every request, including public reads that work
// anonymously; rather than fail those outright, a 401 from a credentialed
// request is retried once without the credential, so a stale token degrades to
// anonymous access. Only nil-body GETs are issued here, so the retry is always
// safe.
func (c RESTClient) DoWithContext(
	ctx context.Context,
	url, authorization string,
	response any,
) (http.Header, error) {
	resp, err := c.do(ctx, url, authorization)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized && authorization != "" {
		resp.Body.Close()
		resp, err = c.do(ctx, url, "")
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		msg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s (%s)", strings.TrimSpace(string(msg)), resp.Status)
	}
	if response == nil || resp.StatusCode == http.StatusNoContent {
		return resp.Header, nil
	}
	return resp.Header, json.NewDecoder(resp.Body).Decode(response)
}

// do issues a single GET, attaching authorization when non-empty.
func (c RESTClient) do(ctx context.Context, url, authorization string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", c.accept)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	return c.httpClient.Do(req)
}
