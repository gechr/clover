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
)

// registryTags is the subset of the OCI registry v2 tags response clover reads.
// The list carries no timestamps, so cooldown does not apply to these tags.
type registryTags struct {
	Tags []string `json:"tags"`
}

// discoverRegistry lists tags from an OCI registry v2 endpoint, answering the
// bearer-token challenge a registry returns on the first, unauthenticated
// request. The token request piggybacks on the user's docker login for the host.
func (p *Provider) discoverRegistry(ctx context.Context, ref reference) ([]model.Candidate, error) {
	endpoint := fmt.Sprintf(
		"https://%s/v2/%s/tags/list?n=%d",
		ref.registry,
		ref.repository,
		pageSize,
	)

	resp, err := p.do(ctx, endpoint, "")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		_ = resp.Body.Close()
		token, terr := p.fetchToken(ctx, challenge, ref)
		if terr != nil {
			return nil, terr
		}
		resp, err = p.do(ctx, endpoint, token)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker: list tags for %s: %s", ref.repository, resp.Status)
	}

	var list registryTags
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("docker: decode tags: %w", err)
	}

	candidates := make([]model.Candidate, 0, len(list.Tags))
	for _, t := range list.Tags {
		candidates = append(candidates, candidate(t, time.Time{}))
	}
	return candidates, nil
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
