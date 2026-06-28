package gitea

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// restClient issues Gitea REST requests over a shared transport. It holds no
// credential: each call carries its own absolute URL and its own token/scheme,
// since both the host and the credential are per-marker values.
type restClient struct {
	httpClient *http.Client
}

// apiURL builds the absolute API URL for a path on a host, e.g.
// https://codeberg.org/api/v1/repos/owner/name/tags.
func apiURL(host, path string) string {
	return "https://" + host + "/api/v1/" + path
}

// DoWithContext issues a GET against the absolute url, authenticating with the
// given token and scheme, fails on a non-2xx status, decodes the JSON body into
// response when non-nil, and returns the response headers (for pagination). A
// stored credential that has expired or been revoked would 401 every request,
// including public reads that work anonymously; rather than fail those outright, a
// 401 from a credentialed request is retried once without the credential, so a
// stale token degrades to anonymous access.
func (c *restClient) DoWithContext(
	ctx context.Context,
	url, token, scheme string,
	response any,
) (http.Header, error) {
	resp, err := c.do(ctx, url, token, scheme)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized && token != "" {
		resp.Body.Close()
		resp, err = c.do(ctx, url, "", "")
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		msg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s (%s)", strings.TrimSpace(string(msg)), resp.Status)
	}
	if response == nil {
		return resp.Header, nil
	}
	return resp.Header, json.NewDecoder(resp.Body).Decode(response)
}

// do issues a single GET, attaching the token under its scheme when non-empty.
// Gitea reads a personal access token as `token <tok>` and an OAuth access token
// as `Bearer <tok>`.
func (c *restClient) do(ctx context.Context, url, token, scheme string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", scheme+" "+token)
	}
	return c.httpClient.Do(req)
}
