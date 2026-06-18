package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// hubTags is the subset of the Docker Hub tags response clover reads. The Hub
// API returns tags newest-first with a last-updated timestamp, so cooldown
// works for Docker Hub images (unlike the bare OCI registry tags list). Next is
// the URL of the following page, empty on the last.
type hubTags struct {
	Next    string `json:"next"`
	Results []struct {
		Name        string    `json:"name"`
		LastUpdated time.Time `json:"last_updated"`
	} `json:"results"`
}

// discoverHub lists tags via the Docker Hub API, newest-first and with dates. A
// shallow lookup reads only the first (newest) page; a deep lookup follows the
// next-page links to exhaustion.
func (p *Provider) discoverHub(ctx context.Context, ref reference) ([]model.Candidate, error) {
	url := fmt.Sprintf(
		"https://%s/v2/repositories/%s/tags?page_size=%d&ordering=last_updated",
		hubAPIHost, ref.repository, pageSize,
	)
	token := p.hubToken(ctx)

	var candidates []model.Candidate
	for url != "" {
		page, err := p.hubPage(ctx, url, token)
		if err != nil {
			return nil, err
		}
		for _, t := range page.Results {
			candidates = append(candidates, candidate(t.Name, t.LastUpdated))
		}
		if !provider.Deep(ctx) {
			break
		}
		url = page.Next
	}
	return candidates, nil
}

// hubPage fetches and decodes one page of Docker Hub tags.
func (p *Provider) hubPage(ctx context.Context, url, token string) (hubTags, error) {
	resp, err := p.do(ctx, url, token)
	if err != nil {
		return hubTags{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return hubTags{}, fmt.Errorf("docker: list hub tags: %s", resp.Status)
	}

	var page hubTags
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return hubTags{}, fmt.Errorf("docker: decode hub tags: %w", err)
	}
	return page, nil
}

// hubToken returns a bearer token for the Docker Hub API: the env token, else a
// JWT minted by logging in with the piggybacked docker credentials, else "" for
// anonymous access. The JWT is resolved once and shared across the run.
func (p *Provider) hubToken(ctx context.Context) string {
	p.hubOnce.Do(func() {
		cfg := p.resolveAuth(hubAuthHost)
		switch {
		case cfg == nil:
			return
		case cfg.RegistryToken != "":
			p.hubJWT = cfg.RegistryToken
		case cfg.Username != "" && cfg.Password != "":
			p.hubJWT, _ = p.hubLogin(ctx, cfg.Username, cfg.Password) // anonymous on failure
		}
	})
	return p.hubJWT
}

// hubLogin exchanges a username and password for a Docker Hub API JWT.
func (p *Provider) hubLogin(ctx context.Context, username, password string) (string, error) {
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		return "", fmt.Errorf("docker: encode hub login: %w", err)
	}
	url := fmt.Sprintf("https://%s/v2/users/login", hubAPIHost)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("docker: build hub login: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("docker: hub login: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("docker: hub login: %s", resp.Status)
	}

	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("docker: decode hub login: %w", err)
	}
	return out.Token, nil
}
