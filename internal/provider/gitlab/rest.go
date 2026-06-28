package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// restClient issues GitLab REST requests over a shared transport. It holds no
// host or credential: each call carries its own absolute URL and its own token,
// since both the host and the credential are per-marker values. clover issues
// requests directly, attaching a token only when it has one, so anonymous
// (rate-limited) reads of a public project still work.
type restClient struct {
	httpClient *http.Client
}

// apiURL builds the absolute REST API URL for a path on a host, e.g.
// https://gitlab.com/api/v4/projects/.../repository/tags. A self-managed host
// serves the same /api/v4 surface.
func apiURL(host, path string) string {
	return "https://" + host + "/api/v4/" + path
}

// DoWithContext issues a GET against the absolute url, attaching token as a
// Bearer credential when non-empty, fails on a non-2xx status, decodes the JSON
// body into response when non-nil, and returns the response headers (for
// pagination). A stored credential that has expired or been revoked would 401
// every request, including public reads that work anonymously; rather than fail
// those outright, a 401 from a credentialed request is retried once without the
// credential, so an expired login degrades to anonymous access. Only nil-body
// (GET) requests are issued here, so the retry is always safe.
func (c *restClient) DoWithContext(
	ctx context.Context,
	url, token string,
	response any,
) (http.Header, error) {
	resp, err := c.do(ctx, url, token)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized && token != "" {
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

// do issues a single GET, attaching token as a Bearer credential when non-empty.
// Bearer is the one header GitLab accepts for both credential kinds: an OAuth
// token minted by the device flow and a personal access token. The PRIVATE-TOKEN
// header works only for PATs, so a stored OAuth token sent that way 401s.
func (c *restClient) do(ctx context.Context, url, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return c.httpClient.Do(req)
}
