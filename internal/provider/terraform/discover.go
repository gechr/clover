package terraform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// wellKnownPath is the service discovery document every Terraform-protocol
// registry serves, mapping service ids to their base URLs.
const wellKnownPath = "/.well-known/terraform.json"

// discovery is the subset of the service discovery document the provider
// reads: the providers.v1 base, either a path on the same host or an absolute
// URL.
type discovery struct {
	Providers string `json:"providers.v1"`
}

// versionsResponse is the subset of the versions endpoint's response the
// provider reads. Each version is a bare semver string; the platforms and
// protocol lists are irrelevant to version selection.
type versionsResponse struct {
	Versions []struct {
		Version string `json:"version"`
	} `json:"versions"`
}

// Discover lists candidate versions for a provider source. The versions
// endpoint returns the whole history in one response, so there is no
// pagination and nothing is ever left unread - --deep has no work to do here.
// The response carries no publication dates, so cooldown is inert.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("%s: invalid resource %T", p.registry.name, r)
	}

	endpoint, err := p.versionsURL(ctx, res)
	if err != nil {
		return nil, err
	}
	var versions versionsResponse
	if err := p.fetch(ctx, endpoint, &versions); err != nil {
		return nil, err
	}

	candidates := make([]model.Candidate, 0, len(versions.Versions))
	for _, v := range versions.Versions {
		if v.Version == "" {
			continue
		}
		candidates = append(candidates, model.NewCandidate(v.Version))
	}
	return candidates, nil
}

// versionsURL resolves the versions endpoint for res: the providers.v1 base
// from the host's service discovery document, then namespace/name/versions
// under it. The base is usually a path on the same host but the protocol
// allows an absolute URL, so it is resolved as a reference.
func (p *Provider) versionsURL(ctx context.Context, res resource) (string, error) {
	var doc discovery
	if err := p.fetch(ctx, "https://"+res.host+wellKnownPath, &doc); err != nil {
		return "", err
	}
	if doc.Providers == "" {
		return "", fmt.Errorf(
			"%s: host %q does not offer the providers.v1 service",
			p.registry.name,
			res.host,
		)
	}
	base, err := url.Parse(doc.Providers)
	if err != nil {
		return "", fmt.Errorf(
			"%s: parse providers.v1 base %q: %w",
			p.registry.name,
			doc.Providers,
			err,
		)
	}
	root := &url.URL{Scheme: "https", Host: res.host}
	return root.ResolveReference(base).JoinPath(res.namespace, res.name, "versions").String(), nil
}

// fetch downloads and decodes one JSON document.
func (p *Provider) fetch(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("%s: build request: %w", p.registry.name, err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: GET %s: %w", p.registry.name, endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return provider.StatusError(fmt.Sprintf("%s: GET %s", p.registry.name, endpoint), resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: decode %s: %w", p.registry.name, endpoint, err)
	}
	return nil
}
