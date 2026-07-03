package oci

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
)

const tokenExpirySkew = 30 * time.Second

type tokenKey struct {
	Realm      string
	Service    string
	Scope      string
	AuthHost   string
	Credential string
}

type repoTokenKey struct {
	Host       string
	AuthHost   string
	Repository string
}

type cachedToken struct {
	Value  string
	Expiry time.Time
}

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
		c.storeRepoToken(repo, tokenKey{
			Realm:      realm,
			Service:    params["service"],
			Scope:      params["scope"],
			AuthHost:   repo.authHost(),
			Credential: credentialFingerprint(cfg),
		}, cachedToken{Value: cfg.RegistryToken})
		return cfg.RegistryToken, nil
	}

	key := tokenKey{
		Realm:      realm,
		Service:    params["service"],
		Scope:      params["scope"],
		AuthHost:   repo.authHost(),
		Credential: credentialFingerprint(cfg),
	}
	if token, ok := c.cachedToken(key); ok {
		c.rememberRepoToken(repo, key)
		return token, nil
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
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", fmt.Errorf("%s: decode token: %w", c.label, err)
	}
	value := cmp.Or(token.Token, token.AccessToken)
	c.storeRepoToken(repo, key, cachedToken{Value: value, Expiry: tokenExpiry(token.ExpiresIn)})
	return value, nil
}

func tokenExpiry(expiresIn int64) time.Time {
	if expiresIn <= 0 {
		return time.Time{}
	}
	return time.Now().Add(time.Duration(expiresIn) * time.Second)
}

func (c *Client) cachedRepoToken(repo Repo) string {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.repoTokens == nil || c.tokens == nil {
		return ""
	}
	key, ok := c.repoTokens[repo.cacheKey()]
	if !ok {
		return ""
	}
	token, ok := c.tokens[key]
	if !ok || token.expired() {
		delete(c.repoTokens, repo.cacheKey())
		delete(c.tokens, key)
		return ""
	}
	return token.Value
}

func (c *Client) cachedToken(key tokenKey) (string, bool) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.tokens == nil {
		return "", false
	}
	token, ok := c.tokens[key]
	if !ok || token.expired() {
		delete(c.tokens, key)
		return "", false
	}
	return token.Value, true
}

func (c *Client) forgetRepoToken(repo Repo) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.repoTokens == nil {
		return
	}
	key, ok := c.repoTokens[repo.cacheKey()]
	if !ok {
		return
	}
	delete(c.repoTokens, repo.cacheKey())
	delete(c.tokens, key)
}

func (c *Client) rememberRepoToken(repo Repo, key tokenKey) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.repoTokens == nil {
		c.repoTokens = make(map[repoTokenKey]tokenKey)
	}
	c.repoTokens[repo.cacheKey()] = key
}

func (c *Client) storeRepoToken(repo Repo, key tokenKey, token cachedToken) {
	if token.Value == "" {
		return
	}
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.tokens == nil {
		c.tokens = make(map[tokenKey]cachedToken)
	}
	if c.repoTokens == nil {
		c.repoTokens = make(map[repoTokenKey]tokenKey)
	}
	c.tokens[key] = token
	c.repoTokens[repo.cacheKey()] = key
}

func (t cachedToken) expired() bool {
	return !t.Expiry.IsZero() && time.Now().After(t.Expiry.Add(-tokenExpirySkew))
}

func (r Repo) cacheKey() repoTokenKey {
	return repoTokenKey{Host: r.Host, AuthHost: r.authHost(), Repository: r.Repository}
}

func credentialFingerprint(cfg *authn.AuthConfig) string {
	if cfg == nil {
		return ""
	}
	raw := strings.Join([]string{
		cfg.Username,
		cfg.Password,
		cfg.Auth,
		cfg.IdentityToken,
		cfg.RegistryToken,
	}, "\n")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
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
