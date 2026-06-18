package docker

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// registryTags is the subset of the OCI registry v2 tags response clover reads.
// The list carries no timestamps, so cooldown does not apply to these tags.
type registryTags struct {
	Tags []string `json:"tags"`
}

// discoverRegistry lists tags from an OCI registry v2 endpoint, answering the
// bearer-token challenge a registry returns on the first, unauthenticated
// request. The token request piggybacks on the user's docker login for the host.
// A shallow lookup reads only the first page; a deep lookup follows the Link
// header's next page to exhaustion (registry tags are lexically ordered, so a
// deep lookup is what guarantees the newest version is seen).
func (p *Provider) discoverRegistry(ctx context.Context, ref reference) ([]model.Candidate, error) {
	url := fmt.Sprintf("https://%s/v2/%s/tags/list?n=%d", ref.registry, ref.repository, pageSize)

	var (
		candidates []model.Candidate
		token      string
	)
	for url != "" {
		next, list, err := p.registryPage(ctx, url, ref, &token)
		if err != nil {
			return nil, err
		}
		for _, t := range list.Tags {
			candidates = append(candidates, candidate(t, time.Time{}))
		}
		if !provider.Deep(ctx) {
			// Registry tags are lexically ordered, so a truncated first page may
			// not hold the newest version; flag it so the edge can suggest --deep.
			if next != "" {
				provider.NoteTruncated(ctx, ref.String())
			}
			break
		}
		url = next
	}
	return candidates, nil
}

// registryPage fetches one page of tags, performing the bearer-token challenge
// when the registry demands it (caching the token in *token for later pages),
// and returns the next page's URL from the Link header (empty when last).
func (p *Provider) registryPage(
	ctx context.Context,
	url string,
	ref reference,
	token *string,
) (string, registryTags, error) {
	resp, err := p.do(ctx, url, *token)
	if err != nil {
		return "", registryTags{}, err
	}
	if resp.StatusCode == http.StatusUnauthorized && *token == "" {
		challenge := resp.Header.Get("WWW-Authenticate")
		_ = resp.Body.Close()
		if *token, err = p.fetchToken(ctx, challenge, ref); err != nil {
			return "", registryTags{}, err
		}
		if resp, err = p.do(ctx, url, *token); err != nil {
			return "", registryTags{}, err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", registryTags{}, fmt.Errorf(
			"docker: list tags for %s: %s",
			ref.repository,
			resp.Status,
		)
	}

	var list registryTags
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return "", registryTags{}, fmt.Errorf("docker: decode tags: %w", err)
	}
	return nextLink(resp.Header.Get("Link"), ref.registry), list, nil
}

// nextLink extracts the rel="next" URL from a registry's Link header, resolving
// a registry-relative path against the registry host. It returns "" when there
// is no next page.
func nextLink(header, registry string) string {
	for part := range strings.SplitSeq(header, ",") {
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		open := strings.IndexByte(part, '<')
		end := strings.IndexByte(part, '>')
		if open < 0 || end <= open {
			continue
		}
		url := part[open+1 : end]
		if strings.HasPrefix(url, "http") {
			return url
		}
		return "https://" + registry + url
	}
	return ""
}

// fetchToken satisfies a registry's Bearer challenge: it requests a token from
// the challenge's realm with the advertised service and scope, authenticating
// with the piggybacked docker credentials when present. A challenge already
// carrying a registry token short-circuits the exchange.
func (p *Provider) fetchToken(
	ctx context.Context,
	challenge string,
	ref reference,
) (string, error) {
	realm, params := parseChallenge(challenge)
	if realm == "" {
		return "", nil // no challenge to satisfy; retry unauthenticated
	}
	if params["scope"] == "" {
		params["scope"] = "repository:" + ref.repository + ":pull"
	}

	cfg := p.resolveAuth(ref.registry)
	if cfg != nil && cfg.RegistryToken != "" {
		return cfg.RegistryToken, nil
	}

	u, err := url.Parse(realm)
	if err != nil {
		return "", fmt.Errorf("docker: parse token realm: %w", err)
	}
	query := u.Query()
	if service := params["service"]; service != "" {
		query.Set("service", service)
	}
	query.Set("scope", params["scope"])
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("docker: build token request: %w", err)
	}
	if cfg != nil && cfg.Username != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("docker: fetch token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("docker: token endpoint %s: %s", u.Host, resp.Status)
	}

	var token struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", fmt.Errorf("docker: decode token: %w", err)
	}
	return cmp.Or(token.Token, token.AccessToken), nil
}

// parseChallenge parses a Bearer WWW-Authenticate header into its realm and the
// rest of its parameters, e.g.
// `Bearer realm="https://auth.docker.io/token",service="registry.docker.io"`.
// A non-Bearer or malformed header yields an empty realm.
func parseChallenge(header string) (string, map[string]string) {
	params := map[string]string{}
	scheme, rest, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", params
	}
	for _, part := range splitParams(rest) {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		params[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	return params["realm"], params
}

// splitParams splits a challenge's comma-separated parameters, respecting the
// double quotes around values (a scope may itself contain a comma).
func splitParams(s string) []string {
	var (
		parts   []string
		current strings.Builder
		quoted  bool
	)
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			current.WriteRune(r)
		case r == ',' && !quoted:
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}
