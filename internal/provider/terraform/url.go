package terraform

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// URL builds the web page for a resolved candidate on the registry's public
// registry. A private registry (an explicit host) has no known web UI, so the
// link goes empty. Registry versions are bare semver, so Version and Ref
// coincide; Ref is still preferred for the synthesized current-version
// candidate.
func (p *Provider) URL(r provider.Resource, c model.Candidate) string {
	res, ok := r.(resource)
	ref := cmp.Or(c.Ref, c.Version)
	if !ok || ref == "" || res.host != p.registry.host {
		return ""
	}
	return fmt.Sprintf(p.registry.web, res.namespace, res.name, ref)
}

// Identify returns the namespace/name provider and, on a public registry, its
// web page. A private registry (an explicit host) has no known web UI, so the
// URL goes empty. The landing page is the version page's format with the
// trailing version segment dropped.
func (p *Provider) Identify(r provider.Resource) (string, string) {
	res, ok := r.(resource)
	if !ok {
		return "", ""
	}
	id := res.namespace + "/" + res.name
	if res.host != p.registry.host {
		return id, ""
	}
	home := p.registry.web
	if i := strings.LastIndex(home, "/"); i >= 0 {
		home = home[:i]
	}
	return id, fmt.Sprintf(home, res.namespace, res.name)
}
