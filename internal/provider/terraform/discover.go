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
	Modules   string `json:"modules.v1"`
	Providers string `json:"providers.v1"`
}

// versionsResponse is the subset of the versions endpoint's response the
// provider reads. Each version is a bare semver string; the platforms, protocol
// lists, and module dependency graphs are irrelevant to version selection.
//
// The two services shape the same answer differently: providers.v1 lists
// versions at the top level, while modules.v1 nests them under one entry per
// requested source. Both are decoded into the same struct and flattened by
// [versionsResponse.versions], since exactly one of the two fields is ever
// populated.
type versionsResponse struct {
	Versions []versionEntry `json:"versions"`
	Modules  []struct {
		Versions []versionEntry `json:"versions"`
	} `json:"modules"`
}

// versionEntry is one published version in either service's listing.
type versionEntry struct {
	Version string `json:"version"`
}

// versions flattens the response to the published version strings, whichever
// service shaped it.
func (r versionsResponse) versions() []versionEntry {
	entries := r.Versions
	for _, m := range r.Modules {
		entries = append(entries, m.Versions...)
	}
	return entries
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

	entries := versions.versions()
	candidates := make([]model.Candidate, 0, len(entries))
	for _, v := range entries {
		if v.Version == "" {
			continue
		}
		candidates = append(candidates, model.NewCandidate(v.Version))
	}
	return candidates, nil
}

// versionsURL resolves the versions endpoint for res: the service base from the
// host's discovery document, then the source address and versions under it. A
// module resolves through modules.v1 and a provider through providers.v1, two
// separate services a registry advertises independently - a host may offer one
// and not the other. The base is usually a path on the same host but the
// protocol allows an absolute URL, so it is resolved as a reference.
func (p *Provider) versionsURL(ctx context.Context, res resource) (string, error) {
	var doc discovery
	if err := p.fetch(ctx, "https://"+res.host+wellKnownPath, &doc); err != nil {
		return "", err
	}

	service, base := "providers.v1", doc.Providers
	segments := []string{res.namespace, res.name}
	if res.module() {
		service, base = "modules.v1", doc.Modules
		segments = append(segments, res.target)
	}
	if base == "" {
		return "", fmt.Errorf(
			"%s: host %q does not offer the %s service",
			p.registry.name,
			res.host,
			service,
		)
	}

	ref, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("%s: parse %s base %q: %w", p.registry.name, service, base, err)
	}
	root := &url.URL{Scheme: "https", Host: res.host}
	return root.ResolveReference(ref).JoinPath(append(segments, "versions")...).String(), nil
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
