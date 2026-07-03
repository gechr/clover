package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	xhttp "github.com/gechr/x/http"
)

// tagList is the subset of the registry v2 tags response clover reads. The list
// carries no timestamps, so cooldown does not apply to these tags.
type tagList struct {
	Tags []string `json:"tags"`
}

// Tags lists a repository's tags from the registry v2 tags endpoint, answering
// the bearer-token challenge a registry returns on the first, unauthenticated
// request. A shallow lookup (deep=false) reads only the first page and reports
// truncated=true when a further page exists; a deep lookup follows the Link
// header to exhaustion (registry tags are lexically ordered, so a deep lookup is
// what guarantees the newest version is seen).
func (c *Client) Tags(ctx context.Context, repo Repo, deep bool) ([]string, bool, error) {
	next := fmt.Sprintf("https://%s/v2/%s/tags/list?n=%d", repo.Host, repo.Repository, pageSize)

	var (
		tags  []string
		token string
	)
	for next != "" {
		page, list, err := c.tagPage(ctx, next, repo, &token)
		if err != nil {
			return nil, false, err
		}
		tags = append(tags, list.Tags...)
		if !deep {
			return tags, page != "", nil
		}
		next = page
	}
	return tags, false, nil
}

// tagPage fetches one page of tags, performing the bearer-token challenge when
// the registry demands it (caching the token in *token for later pages), and
// returns the next page's URL from the Link header (empty when last).
func (c *Client) tagPage(
	ctx context.Context,
	url string,
	repo Repo,
	token *string,
) (string, tagList, error) {
	resp, err := c.Get(ctx, url, *token)
	if err != nil {
		return "", tagList{}, err
	}
	if resp.StatusCode == http.StatusUnauthorized && *token == "" {
		challenge := resp.Header.Get("WWW-Authenticate")
		_ = resp.Body.Close()
		if *token, err = c.fetchToken(ctx, challenge, repo); err != nil {
			return "", tagList{}, err
		}
		if resp, err = c.Get(ctx, url, *token); err != nil {
			return "", tagList{}, err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", tagList{}, c.StatusErr("list tags for "+repo.Repository, resp)
	}

	var list tagList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return "", tagList{}, fmt.Errorf("%s: decode tags: %w", c.label, err)
	}
	return nextLink(resp.Header, url), list, nil
}

// nextLink resolves the rel="next" pagination target from the response headers
// against the request URL, returning "" unless it is same-scheme, same-host - so
// a registry cannot redirect the deep walk to an arbitrary or internal endpoint.
func nextLink(header http.Header, requestURL string) string {
	target := xhttp.NextLink(header)
	if target == "" {
		return ""
	}
	base, err := url.Parse(requestURL)
	if err != nil {
		return ""
	}
	next, err := base.Parse(target)
	if err != nil || next.Scheme != base.Scheme || next.Host != base.Host {
		return ""
	}
	return next.String()
}
