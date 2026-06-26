package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// apiBaseURL is the GitHub REST API root. go-gh derives this from the host, but
// its client refuses to build without a resolvable token, which would break
// anonymous access (and force tests to find an ambient credential). clover
// issues REST requests directly instead, attaching a token only when it has one.
const apiBaseURL = "https://api.github.com/"

// restClient issues GitHub REST requests over a shared transport, authenticating
// only when a token is present so anonymous (rate-limited) access still works.
type restClient struct {
	httpClient *http.Client
	token      string
}

// DoWithContext mirrors go-gh's RESTClient.DoWithContext so call sites are
// unchanged: it issues method against path (relative to the API root), fails on
// a non-2xx status, and decodes the JSON body into response when non-nil.
func (c *restClient) DoWithContext(
	ctx context.Context,
	method, path string,
	body io.Reader,
	response any,
) error {
	req, err := http.NewRequestWithContext(ctx, method, apiBaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s (%s)", strings.TrimSpace(string(msg)), resp.Status)
	}
	if response == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(response)
}
