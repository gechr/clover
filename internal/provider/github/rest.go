package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// restClient issues GitHub REST requests over a shared transport. It holds no
// host or credential: each call carries its own absolute URL and its own token,
// since both the host and the credential are per-marker values. go-gh's own REST
// client refuses to build without a resolvable token, which would break
// anonymous access (and force tests to find an ambient credential), so clover
// issues REST requests directly instead.
type restClient struct {
	httpClient *http.Client
}

// apiURL builds the absolute REST API URL for a path on a host, mirroring go-gh's
// host mapping: github.com is served at api.github.com, a GitHub Enterprise
// Server host under https://<host>/api/v3.
func apiURL(host, path string) string {
	if host == defaultHost {
		return "https://api.github.com/" + path
	}
	return "https://" + host + "/api/v3/" + path
}

// DoWithContext issues a GET against the absolute url, attaching token as a
// Bearer credential when non-empty, fails on a non-2xx status, decodes the JSON
// body into response when non-nil, and returns the response headers (for
// pagination).
func (c *restClient) DoWithContext(
	ctx context.Context,
	url, token string,
	response any,
) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
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
