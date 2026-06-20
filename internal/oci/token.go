package oci

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// fetchToken satisfies a registry's Bearer challenge: it requests a token from
// the challenge's realm with the advertised service and scope, authenticating
// with the piggybacked login credentials when present. A challenge already
// carrying a registry token short-circuits the exchange.
func (c *Client) fetchToken(ctx context.Context, challenge string, repo Repo) (string, error) {
	realm, params := parseChallenge(challenge)
	if realm == "" {
		return "", nil // no challenge to satisfy; retry unauthenticated
	}
	if params["scope"] == "" {
		params["scope"] = "repository:" + repo.Repository + ":pull"
	}

	cfg := c.ResolveAuth(repo.authHost())
	if cfg != nil && cfg.RegistryToken != "" {
		return cfg.RegistryToken, nil
	}

	u, err := url.Parse(realm)
	if err != nil {
		return "", fmt.Errorf("%s: parse token realm: %w", c.label, err)
	}
	query := u.Query()
	if service := params["service"]; service != "" {
		query.Set("service", service)
	}
	query.Set("scope", params["scope"])
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("%s: build token request: %w", c.label, err)
	}
	if cfg != nil && cfg.Username != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: fetch token: %w", c.label, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: token endpoint %s: %s", c.label, u.Host, resp.Status)
	}

	var token struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", fmt.Errorf("%s: decode token: %w", c.label, err)
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
